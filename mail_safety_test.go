package main

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
}

func TestDevMailDefaultsToConsole(t *testing.T) {
	for _, key := range []string{
		"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "SMTP_ALLOW_REAL", "MAIL_BACKEND", "MAIL_FROM",
		"TURNSTILE_SITE_KEY", "TURNSTILE_SECRET_KEY", "TRUSTED_IP_HEADER", "BASE_URL",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("ENV", "dev")

	cfg := loadConfig()
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	backend, err := cfg.resolveMail()
	if err != nil {
		t.Fatal(err)
	}
	if backend != mailConsole {
		t.Fatalf("backend %q", backend)
	}
	m, err := newMailer(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(consoleMailer); !ok {
		t.Fatalf("mailer %T", m)
	}
}

func TestDevSMTPCredentialsAloneDoNotSend(t *testing.T) {
	cfg := config{
		Env:      "dev",
		SMTPHost: "smtp.example.com",
		SMTPPort: "587",
		SMTPUser: "live-user",
		SMTPPass: "live-password",
		MailFrom: "Track Anything <hello@trackanything.io>",
	}
	err := cfg.validate()
	if !errors.Is(err, errDevSMTPOptIn) {
		t.Fatalf("validate: %v", err)
	}
	if strings.Contains(err.Error(), "live-password") || strings.Contains(err.Error(), "live-user") {
		t.Fatalf("error exposed credentials: %v", err)
	}
	if _, err := newMailer(cfg, discardLogger()); !errors.Is(err, errDevSMTPOptIn) {
		t.Fatalf("newMailer: %v", err)
	}

	// Credentials with no host stay on the console. They do not select a server.
	cfg.SMTPHost = ""
	m, err := newMailer(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(consoleMailer); !ok {
		t.Fatalf("mailer %T", m)
	}
}

func TestDevSMTPRequiresOptIn(t *testing.T) {
	cfg := config{
		Env:         "dev",
		MailBackend: mailSMTP,
		SMTPHost:    "smtp.example.com",
		SMTPPort:    "587",
		SMTPUser:    "live-user",
		SMTPPass:    "live-password",
		MailFrom:    "Track Anything <hello@trackanything.io>",
	}
	if err := cfg.validate(); !errors.Is(err, errDevSMTPOptIn) {
		t.Fatalf("without opt-in: %v", err)
	}
	if _, err := newMailer(cfg, discardLogger()); !errors.Is(err, errDevSMTPOptIn) {
		t.Fatalf("newMailer without opt-in: %v", err)
	}

	cfg.SMTPAllowReal = "1"
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	m, err := newMailer(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	sm, ok := m.(smtpMailer)
	if !ok {
		t.Fatalf("mailer %T", m)
	}
	if sm.requireTLS {
		t.Fatal("development opt-in required TLS")
	}
	if sm.addr != "smtp.example.com:587" {
		t.Fatalf("addr %s", sm.addr)
	}

	// The flag without credentials still does not build a sender.
	cfg.SMTPUser = ""
	cfg.SMTPPass = ""
	if _, err := newMailer(cfg, discardLogger()); err == nil {
		t.Fatal("opt-in without credentials built a mailer")
	}
}

func TestDevLocalMailDoesNotNeedOptIn(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "::1"} {
		cfg := config{
			Env:      "dev",
			SMTPHost: host,
			SMTPPort: "1025",
			MailFrom: "Track Anything <hello@trackanything.io>",
		}
		backend, err := cfg.resolveMail()
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if backend != mailLocal {
			t.Fatalf("%s backend %q", host, backend)
		}
	}
}

func TestDevConsoleSelectionIgnoresSMTP(t *testing.T) {
	cfg := config{
		Env:           "dev",
		MailBackend:   mailConsole,
		SMTPHost:      "smtp.example.com",
		SMTPPort:      "587",
		SMTPUser:      "live-user",
		SMTPPass:      "live-password",
		SMTPAllowReal: "1",
		MailFrom:      "Track Anything <hello@trackanything.io>",
	}
	m, err := newMailer(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(consoleMailer); !ok {
		t.Fatalf("mailer %T", m)
	}
}

func TestProdMailKeepsConfiguredBackend(t *testing.T) {
	cfg := config{
		Env:                "prod",
		BaseURL:            "https://trackanything.io",
		baseURLSet:         true,
		TurnstileSiteKey:   "site-key",
		TurnstileSecretKey: "secret-key",
		SMTPHost:           "smtp.example.com",
		SMTPPort:           "587",
		SMTPUser:           "live-user",
		SMTPPass:           "live-password",
		MailFrom:           "Track Anything <hello@trackanything.io>",
		MailBackend:        mailConsole,
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	m, err := newMailer(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	sm, ok := m.(smtpMailer)
	if !ok {
		t.Fatalf("mailer %T", m)
	}
	if !sm.requireTLS || sm.implicitTLS {
		t.Fatalf("requireTLS %v implicit %v", sm.requireTLS, sm.implicitTLS)
	}
	if strings.Contains(buf.String(), "live-password") || strings.Contains(buf.String(), "live-user") {
		t.Fatalf("construction logged credentials: %s", buf.String())
	}

	// An empty production host stays console and does not invent a server.
	// MAIL_BACKEND is a development control and does not change that.
	unset := cfg
	unset.SMTPHost = ""
	unset.MailBackend = mailSMTP
	backend, err := unset.resolveMail()
	if err != nil {
		t.Fatal(err)
	}
	if backend != mailConsole {
		t.Fatalf("backend %q", backend)
	}
}

func TestProdConsoleMailHidesMessage(t *testing.T) {
	const token = "verify-token-secret"
	body := "Open https://trackanything.io/verify/" + token + " to confirm"
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	m, err := newMailer(config{Env: "prod", MailFrom: "Track Anything <hello@trackanything.io>"}, logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(consoleMailer); !ok {
		t.Fatalf("mailer %T", m)
	}
	if err := m.Send("person@example.com", "Confirm your Track Anything email", body); err != nil {
		t.Fatal(err)
	}
	logged := buf.String()
	for _, secret := range []string{token, body, "https://", "person@example.com", "Confirm your Track Anything email"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("production log contains %q: %s", secret, logged)
		}
	}
}

func TestDevConsoleMailShowsLink(t *testing.T) {
	const token = "dev-signin-token"
	body := "http://localhost:8080/login/link/" + token
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	m, err := newMailer(config{Env: "dev"}, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Send("person@example.com", "Your Track Anything login link", body); err != nil {
		t.Fatal(err)
	}
	logged := buf.String()
	if !strings.Contains(logged, token) || !strings.Contains(logged, body) {
		t.Fatalf("development log hid the link: %s", logged)
	}
}

func TestDotEnvCannotOverrideForcedConsole(t *testing.T) {
	for _, key := range []string{"SMTP_HOST", "SMTP_USER", "SMTP_PASS", "SMTP_ALLOW_REAL", "MAIL_FROM"} {
		t.Setenv(key, "")
	}
	t.Setenv("ENV", "dev")
	t.Setenv("MAIL_BACKEND", mailConsole)

	dir := t.TempDir()
	body := "" +
		"MAIL_BACKEND=smtp\n" +
		"SMTP_HOST=smtp.example.com\n" +
		"SMTP_USER=live-user\n" +
		"SMTP_PASS=live-password\n" +
		"SMTP_ALLOW_REAL=1\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnv(filepath.Join(dir, ".env")); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig()
	m, err := newMailer(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(consoleMailer); !ok {
		t.Fatalf("mailer %T", m)
	}
	if cfg.SMTPHost != "" || cfg.SMTPPass != "" || cfg.SMTPAllowReal != "" {
		t.Fatalf("file overrode forced mail settings: %+v", cfg)
	}
}

func TestHandlersUseMemMailer(t *testing.T) {
	a := newTestApp(t)
	m, ok := a.mailer.(*memMailer)
	if !ok {
		t.Fatalf("mailer %T", a.mailer)
	}
	const token = "mem-mailer-token"
	if err := m.Send("person@example.com", "Your Track Anything login link", "http://example.test/login/link/"+token); err != nil {
		t.Fatal(err)
	}
	msgs := m.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Body, token) {
		t.Fatalf("messages %+v", msgs)
	}
}
