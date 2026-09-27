package main

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "time/tzdata"

	"gorm.io/gorm"
)

//go:embed templates static
var embedded embed.FS

func init() {
	// Go's built-in MIME table doesn't know this extension; browsers expect this type.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

type config struct {
	Addr     string
	DBPath   string
	Env      string // "dev" or "prod"
	BaseURL  string // used to build links in emails, e.g. "https://trackanything.io"
	SMTPHost string // empty = log emails to the console instead of sending
	SMTPPort string
	SMTPUser string
	SMTPPass string
	MailFrom string // e.g. "Track Anything <hello@trackanything.io>"
}

func loadConfig() config {
	cfg := config{
		Addr:     getenv("ADDR", ":8080"),
		DBPath:   getenv("DB_PATH", "data/trackanything.db"),
		Env:      getenv("ENV", "dev"),
		BaseURL:  strings.TrimSuffix(getenv("BASE_URL", "http://localhost:8080"), "/"),
		SMTPHost: os.Getenv("SMTP_HOST"),
		SMTPPort: getenv("SMTP_PORT", "587"),
		SMTPUser: os.Getenv("SMTP_USER"),
		SMTPPass: os.Getenv("SMTP_PASS"),
		MailFrom: getenv("MAIL_FROM", "Track Anything <hello@trackanything.io>"),
	}
	if port := os.Getenv("PORT"); port != "" && os.Getenv("ADDR") == "" {
		cfg.Addr = ":" + port
	}
	return cfg
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type app struct {
	cfg    config
	db     *gorm.DB
	views  *views
	logger *slog.Logger
	mailer mailer
	now    func() time.Time // replaced in tests to move the clock

	loginLimiter  *rateLimiter
	linkLimiter   *rateLimiter
	signupLimiter *rateLimiter
	shareLimiter  *rateLimiter
}

func newApp(cfg config, db *gorm.DB, v *views, logger *slog.Logger, m mailer) *app {
	a := &app{cfg: cfg, db: db, views: v, logger: logger, mailer: m, now: time.Now}
	clock := func() time.Time { return a.now() }
	a.loginLimiter = newRateLimiter(10, time.Minute, clock)
	a.linkLimiter = newRateLimiter(5, 15*time.Minute, clock)
	a.signupLimiter = newRateLimiter(10, time.Hour, clock)
	a.shareLimiter = newRateLimiter(30, time.Minute, clock)
	return a
}

func main() {
	if err := loadDotEnv(".env"); err != nil {
		slog.Error("load .env", "err", err)
		os.Exit(1)
	}
	cfg := loadConfig()
	logger := newLogger(cfg.Env)

	if err := run(cfg, logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfg config, logger *slog.Logger) error {
	db, err := openDB(cfg.DBPath)
	if err != nil {
		return err
	}

	v, err := loadViews(embedded)
	if err != nil {
		return err
	}

	m, err := newMailer(cfg, logger)
	if err != nil {
		return err
	}
	if cfg.Env == "prod" && cfg.SMTPHost == "" {
		logger.Warn("SMTP_HOST is not set; magic-link emails will only be logged")
	}

	a := newApp(cfg, db, v, logger, m)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr, "env", cfg.Env, "db", cfg.DBPath)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	return nil
}

func newLogger(env string) *slog.Logger {
	if env == "prod" {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.FileServerFS(embedded))
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	mux.HandleFunc("GET /{$}", a.handleHome)

	mux.HandleFunc("GET /signup", a.handleSignupForm)
	mux.HandleFunc("POST /signup", a.handleSignup)
	mux.HandleFunc("GET /login", a.handleLoginForm)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /login/link", a.handleLoginLinkRequest)
	mux.HandleFunc("GET /login/link/{token}", a.handleLoginLinkConfirm)
	mux.HandleFunc("POST /login/link/{token}", a.handleLoginLinkUse)
	mux.HandleFunc("POST /logout", a.handleLogout)

	mux.HandleFunc("GET /settings", a.requireUser(a.handleSettings))
	mux.HandleFunc("POST /settings/timezone", a.requireUser(a.handleSettingsTimeZone))
	mux.HandleFunc("POST /settings/password", a.requireUser(a.handleSettingsPassword))

	mux.HandleFunc("GET /households/{hid}", a.requireUser(a.handleHousehold))
	mux.HandleFunc("POST /households/{hid}/invite", a.requireUser(a.handleInviteOn))
	mux.HandleFunc("POST /households/{hid}/invite/delete", a.requireUser(a.handleInviteOff))
	mux.HandleFunc("POST /households/{hid}/members/{uid}/delete", a.requireUser(a.handleRemoveMember))
	mux.HandleFunc("POST /households/{hid}/members/{uid}/owner", a.requireUser(a.handlePromoteMember))
	mux.HandleFunc("GET /join/{token}", a.requireUser(a.handleJoinPage))
	mux.HandleFunc("POST /join/{token}", a.requireUser(a.handleJoin))

	mux.HandleFunc("GET /trackers/new", a.requireUser(a.handleNewTracker))
	mux.HandleFunc("POST /trackers", a.requireUser(a.handleCreateTracker))
	mux.HandleFunc("GET /trackers/{id}", a.requireUser(a.handleShowTracker))
	mux.HandleFunc("GET /trackers/{id}/edit", a.requireUser(a.handleEditTracker))
	mux.HandleFunc("POST /trackers/{id}", a.requireUser(a.handleUpdateTracker))
	mux.HandleFunc("POST /trackers/{id}/archive", a.requireUser(a.handleArchiveTracker))
	mux.HandleFunc("POST /trackers/{id}/restore", a.requireUser(a.handleRestoreTracker))
	mux.HandleFunc("POST /trackers/{id}/share", a.requireUser(a.handleShareOn))
	mux.HandleFunc("POST /trackers/{id}/share/delete", a.requireUser(a.handleShareOff))

	mux.HandleFunc("POST /trackers/{id}/quick", a.requireUser(a.handleQuickLog))
	mux.HandleFunc("POST /trackers/{id}/entries", a.requireUser(a.handleLogEntry))
	mux.HandleFunc("POST /trackers/{id}/zero", a.requireUser(a.handleRecordZero))
	mux.HandleFunc("POST /entries/{eid}/undo", a.requireUser(a.handleUndoEntry))
	mux.HandleFunc("POST /entries/{eid}", a.requireUser(a.handleEditEntry))
	mux.HandleFunc("POST /entries/{eid}/delete", a.requireUser(a.handleDeleteEntry))
	mux.HandleFunc("POST /zeros/{zid}/undo", a.requireUser(a.handleUndoZero))
	mux.HandleFunc("POST /zeros/{zid}/delete", a.requireUser(a.handleDeleteZero))

	mux.HandleFunc("GET /s/{token}", a.handleShare)
	mux.HandleFunc("POST /s/{token}/quick", a.handleShareQuickLog)
	mux.HandleFunc("POST /s/{token}/zero", a.handleShareZero)
	mux.HandleFunc("POST /s/{token}/entries/{eid}/undo", a.handleShareUndoEntry)
	mux.HandleFunc("POST /s/{token}/zeros/{zid}/undo", a.handleShareUndoZero)

	csrf := http.NewCrossOriginProtection()
	return a.recoverPanic(a.logRequests(csrf.Handler(a.loadUser(mux))))
}
