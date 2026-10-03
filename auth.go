package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	sessionCookie      = "session"
	sessionLifetime    = 30 * 24 * time.Hour
	loginLinkLifetime  = 15 * time.Minute
	verifyLinkLifetime = 24 * time.Hour
	minPasswordLen     = 8
	maxPasswordLen     = 72 // bcrypt ignores bytes past 72
)

// randRead is the cryptographic random source for tokens. Tests replace it to
// force a failure. A failure must not become a token.
var randRead = rand.Read

// newToken returns 32 random bytes, base64url-encoded, for session IDs and emailed links.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := randRead(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// dummyHash is compared against when an email has no password, so a failed login
// takes about as long whether or not the account exists.
var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("not a real password"), bcrypt.DefaultCost)
	return h
})

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// validEmail accepts one plain address. net/mail would also accept a display name
// ("Bob <bob@example.com>"); requiring the parsed address to equal the input rejects that.
func validEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

func validatePassword(pw string) string {
	switch {
	case len(pw) < minPasswordLen:
		return fmt.Sprintf("Password must be at least %d characters.", minPasswordLen)
	case len(pw) > maxPasswordLen:
		return fmt.Sprintf("Password must be at most %d characters.", maxPasswordLen)
	}
	return ""
}

// validTimeZone accepts IANA names. "Local" is rejected because it means the server's zone.
func validTimeZone(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// safeNext keeps post-login redirects on this site. Only a local path is allowed.
// url.Parse rejects malformed values and control characters; the extra checks
// reject scheme-relative URLs and backslash forms a browser can treat as a host.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	for i := 0; i < len(next); i++ {
		if next[i] < 0x20 || next[i] == 0x7f || next[i] == '\\' {
			return "/"
		}
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}
	return next
}

// Request context: the logged-in user and their session ID.

type ctxKey int

const (
	userKey ctxKey = iota
	sessionKey
)

func currentUser(r *http.Request) *User {
	u, _ := r.Context().Value(userKey).(*User)
	return u
}

func currentSessionID(r *http.Request) string {
	id, _ := r.Context().Value(sessionKey).(string)
	return id
}

func (a *app) setSessionCookie(w http.ResponseWriter, raw string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    raw,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   a.cfg.Env != "dev",
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *app) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cfg.Env != "dev",
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *app) startSession(w http.ResponseWriter, userID uint) error {
	raw, err := newToken()
	if err != nil {
		return err
	}
	expires := a.now().Add(sessionLifetime)
	s := Session{ID: hashToken(raw), UserID: userID, ExpiresAt: expires}
	if err := a.db.Create(&s).Error; err != nil {
		return err
	}
	a.setSessionCookie(w, raw, expires)
	return nil
}

// loadUser puts the logged-in user, if any, into the request context. Sessions renew
// once they're past half their lifetime, so active users stay logged in.
// /static and /healthz skip the lookup. The health check answers from its own query,
// including when a session cookie is present and the database is down.
func (a *app) loadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		var s Session
		err = a.db.Take(&s, "id = ?", hashToken(c.Value)).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			a.serverError(w, r, err)
			return
		}

		now := a.now()
		if !now.Before(s.ExpiresAt) {
			a.db.Delete(&s)
			a.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		var u User
		if err := a.db.Take(&u, s.UserID).Error; err != nil {
			a.serverError(w, r, err)
			return
		}

		if s.ExpiresAt.Sub(now) < sessionLifetime/2 {
			s.ExpiresAt = now.Add(sessionLifetime)
			if err := a.db.Model(&s).Update("expires_at", s.ExpiresAt).Error; err != nil {
				a.serverError(w, r, err)
				return
			}
			a.setSessionCookie(w, c.Value, s.ExpiresAt)
		}

		ctx := context.WithValue(r.Context(), userKey, &u)
		ctx = context.WithValue(ctx, sessionKey, s.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *app) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r) == nil {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// Signup

type signupForm struct {
	Email string
	Next  string
	Error string
}

func (a *app) handleSignupForm(w http.ResponseWriter, r *http.Request) {
	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, "signup.html", signupForm{Next: r.URL.Query().Get("next")})
}

func (a *app) handleSignup(w http.ResponseWriter, r *http.Request) {
	form := signupForm{
		Email: normalizeEmail(r.PostFormValue("email")),
		Next:  r.PostFormValue("next"),
	}
	fail := func(status int, msg string) {
		form.Error = msg
		a.render(w, r, status, "signup.html", form)
	}

	if !a.signupLimiter.allow(a.clientIP(r)) {
		fail(http.StatusTooManyRequests, "Too many signups from this network. Try again later.")
		return
	}
	if a.turnstileFailed(r) {
		fail(http.StatusForbidden, turnstileRejectedMessage)
		return
	}
	if !validEmail(form.Email) {
		fail(http.StatusUnprocessableEntity, "Enter a valid email address.")
		return
	}

	tz := r.PostFormValue("timezone")
	if !validTimeZone(tz) {
		tz = "UTC"
	}

	var count int64
	if err := a.db.Model(&User{}).Where("email = ?", form.Email).Count(&count).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	if count > 0 {
		fail(http.StatusUnprocessableEntity, "An account with that email already exists. Log in instead.")
		return
	}

	// No password yet. A password typed before the mailbox is confirmed would
	// belong to whoever filled in the form, who may not own the address.
	u := User{Email: form.Email, TimeZone: tz}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		return createPersonalHousehold(tx, u.ID)
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.startSession(w, u.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	// Verification is best-effort. The account and session stand either way.
	if err := a.sendVerificationEmail(&u); err != nil && !errors.Is(err, errMailThrottled) {
		a.logger.Error("server error", "method", r.Method, "path", logPath(r.URL.Path), "err", err)
	}
	http.Redirect(w, r, safeNext(form.Next), http.StatusSeeOther)
}

// Password login

type loginForm struct {
	Email        string
	Next         string
	Error        string
	ShowPassword bool
}

func (a *app) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, "login.html", loginForm{Next: r.URL.Query().Get("next")})
}

func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	form := loginForm{
		Email:        normalizeEmail(r.PostFormValue("email")),
		Next:         r.PostFormValue("next"),
		ShowPassword: true,
	}
	fail := func(status int, msg string) {
		form.Error = msg
		a.render(w, r, status, "login.html", form)
	}

	if !a.loginLimiter.allow(a.clientIP(r)) {
		fail(http.StatusTooManyRequests, "Too many login attempts. Wait a minute and try again.")
		return
	}
	if a.turnstileFailed(r) {
		fail(http.StatusForbidden, turnstileRejectedMessage)
		return
	}

	var u User
	err := a.db.Take(&u, "email = ?", form.Email).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		a.serverError(w, r, err)
		return
	}

	hash := dummyHash()
	if err == nil && u.HasPassword() {
		hash = []byte(u.PasswordHash)
	}
	passwordOK := bcrypt.CompareHashAndPassword(hash, []byte(r.PostFormValue("password"))) == nil
	// An unverified address has no password credential, even if a hash was stored
	// before this rule. Proving the mailbox is what establishes the account.
	if err != nil || !u.HasPassword() || u.EmailVerifiedAt == nil || !passwordOK {
		fail(http.StatusUnauthorized, "Email or password is incorrect. No password yet? Use an email link instead.")
		return
	}

	if err := a.startSession(w, u.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, safeNext(form.Next), http.StatusSeeOther)
}

// Magic links

type linkSentPage struct {
	Email string
	Dev   bool
}

func (a *app) handleLoginLinkRequest(w http.ResponseWriter, r *http.Request) {
	form := loginForm{
		Email: normalizeEmail(r.PostFormValue("email")),
		Next:  r.PostFormValue("next"),
	}

	if !a.linkLimiter.allow(a.clientIP(r)) {
		form.Error = "Too many email links requested. Try again in a few minutes."
		a.render(w, r, http.StatusTooManyRequests, "login.html", form)
		return
	}
	if a.turnstileFailed(r) {
		form.Error = turnstileRejectedMessage
		a.render(w, r, http.StatusForbidden, "login.html", form)
		return
	}
	if !validEmail(form.Email) {
		form.Error = "Enter a valid email address."
		a.render(w, r, http.StatusUnprocessableEntity, "login.html", form)
		return
	}

	// The response is the same whether or not the account exists, so this form
	// can't be used to find out who has an account.
	var u User
	err := a.db.Take(&u, "email = ?", form.Email).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
	case err != nil:
		a.serverError(w, r, err)
		return
	default:
		if err := a.sendLoginLink(u, safeNext(form.Next)); err != nil && !errors.Is(err, errMailThrottled) {
			a.logger.Error("server error", "method", r.Method, "path", logPath(r.URL.Path), "err", err)
			back := "/login"
			if next := safeNext(form.Next); next != "/" {
				back = "/login?next=" + url.QueryEscape(next)
			}
			a.messageTo(w, r, http.StatusInternalServerError,
				"We couldn't send your login link",
				"Something went wrong while sending the email. Please try again in a moment.",
				"Back to log in", back)
			return
		}
	}

	a.render(w, r, http.StatusOK, "login_sent.html", linkSentPage{Email: form.Email, Dev: a.cfg.SMTPHost == ""})
}

func (a *app) sendLoginLink(u User, next string) error {
	if !a.mailAddrLimiter.allow(normalizeEmail(u.Email)) {
		return errMailThrottled
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	lt := LoginToken{UserID: u.ID, TokenHash: hashToken(token), ExpiresAt: a.now().Add(loginLinkLifetime)}
	if err := a.db.Create(&lt).Error; err != nil {
		return err
	}

	link := a.cfg.BaseURL + "/login/link/" + token
	if next != "/" {
		link += "?next=" + url.QueryEscape(next)
	}
	var body string
	if u.EmailVerifiedAt == nil {
		body = "Use this link to log in to Track Anything:\n\n" + link +
			"\n\nIt works once and expires in 15 minutes. This address is not confirmed yet, so opening the link also confirms it, signs out anyone who was using the account before then, and removes any password set before the address was confirmed.\n\n" +
			"If you did not ask to sign up or log in, open the link anyway. Leaving it unused can leave someone else signed in on an account for this address.\n"
	} else {
		body = "Use this link to log in to Track Anything:\n\n" + link +
			"\n\nIt works once and expires in 15 minutes. If you didn't ask for it, you can ignore this email.\n"
	}
	return a.mailer.Send(u.Email, "Your Track Anything login link", body)
}

// findLoginToken returns the unused, unexpired token for the raw value in a link.
func (a *app) findLoginToken(token string) (LoginToken, error) {
	var lt LoginToken
	err := a.db.Take(&lt, "token_hash = ? AND used_at IS NULL AND expires_at > ?", hashToken(token), a.now()).Error
	return lt, err
}

type linkConfirmPage struct {
	Valid bool
	Email string
	Next  string
}

// handleLoginLinkConfirm does not log in on GET. Email security scanners open links
// before the person does and would use up the token. The page posts the form itself
// when JavaScript runs; the button still works without it.
func (a *app) handleLoginLinkConfirm(w http.ResponseWriter, r *http.Request) {
	page := linkConfirmPage{Next: r.URL.Query().Get("next")}

	lt, err := a.findLoginToken(r.PathValue("token"))
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		a.serverError(w, r, err)
		return
	}
	if err == nil {
		var u User
		if err := a.db.Take(&u, lt.UserID).Error; err != nil {
			a.serverError(w, r, err)
			return
		}
		page.Valid = true
		page.Email = u.Email
	}

	status := http.StatusOK
	if !page.Valid {
		status = http.StatusGone
	}
	a.render(w, r, status, "login_link.html", page)
}

func (a *app) handleLoginLinkUse(w http.ResponseWriter, r *http.Request) {
	hash := hashToken(r.PathValue("token"))
	now := a.now()

	// A single conditional update marks the token used, so two clicks can't both succeed.
	res := a.db.Model(&LoginToken{}).
		Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", hash, now).
		Update("used_at", now)
	if res.Error != nil {
		a.serverError(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		a.render(w, r, http.StatusGone, "login_link.html", linkConfirmPage{})
		return
	}

	var lt LoginToken
	if err := a.db.Take(&lt, "token_hash = ?", hash).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	// The link was delivered to the mailbox. The first time that happens, end
	// every session and any password that existed before the address was proven.
	if _, err := a.claimUnverifiedEmail(lt.UserID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.startSession(w, lt.UserID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, safeNext(r.PostFormValue("next")), http.StatusSeeOther)
}

// Email verification

// errMailThrottled means this address was sent too many auth emails recently.
// Callers show the same page they would after a successful send.
var errMailThrottled = errors.New("auth email throttled")

// claimUnverifiedEmail runs when someone proves they own the mailbox.
// If the address was not yet verified, it clears any password and deletes every
// session, then marks the address verified. An already-verified account is left
// as it is. claimed reports whether this call was that first confirmation.
func (a *app) claimUnverifiedEmail(userID uint) (bool, error) {
	claimed := false
	err := a.db.Transaction(func(tx *gorm.DB) error {
		var u User
		if err := tx.Take(&u, userID).Error; err != nil {
			return err
		}
		if u.EmailVerifiedAt != nil {
			return nil
		}
		now := a.now()
		err := tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
			"email_verified_at": now,
			"password_hash":     "",
		}).Error
		if err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&Session{}).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}

func (a *app) sendVerificationEmail(u *User) error {
	if !a.mailAddrLimiter.allow(normalizeEmail(u.Email)) {
		return errMailThrottled
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	vt := VerificationToken{UserID: u.ID, TokenHash: hashToken(token), ExpiresAt: a.now().Add(verifyLinkLifetime)}
	if err := a.db.Create(&vt).Error; err != nil {
		return err
	}
	link := a.cfg.BaseURL + "/verify/" + token
	body := "Confirm your email for Track Anything:\n\n" + link +
		"\n\nThe link works once and expires in 24 hours. Opening it confirms this address, signs out anyone who was using the account before then, and removes any password set before the address was confirmed.\n\n" +
		"If you did not sign up, open the link anyway. Leaving it unused can leave someone else signed in on an account for this address.\n"
	return a.mailer.Send(u.Email, "Confirm your Track Anything email", body)
}

func (a *app) findVerificationToken(token string) (VerificationToken, error) {
	var vt VerificationToken
	err := a.db.Take(&vt, "token_hash = ? AND used_at IS NULL AND expires_at > ?", hashToken(token), a.now()).Error
	return vt, err
}

type verifyPage struct {
	Valid bool
	Email string
}

// handleVerifyConfirm does not verify on GET. Mailbox scanners open links, and a
// GET that confirmed the address would be used up before the person clicked.
func (a *app) handleVerifyConfirm(w http.ResponseWriter, r *http.Request) {
	page := verifyPage{}
	vt, err := a.findVerificationToken(r.PathValue("token"))
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		a.serverError(w, r, err)
		return
	}
	if err == nil {
		var u User
		if err := a.db.Take(&u, vt.UserID).Error; err != nil {
			a.serverError(w, r, err)
			return
		}
		page.Valid = true
		page.Email = u.Email
	}
	status := http.StatusOK
	if !page.Valid {
		status = http.StatusGone
	}
	a.render(w, r, status, "verify.html", page)
}

func (a *app) handleVerifyUse(w http.ResponseWriter, r *http.Request) {
	hash := hashToken(r.PathValue("token"))
	now := a.now()
	res := a.db.Model(&VerificationToken{}).
		Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", hash, now).
		Update("used_at", now)
	if res.Error != nil {
		a.serverError(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		a.render(w, r, http.StatusGone, "verify.html", verifyPage{})
		return
	}

	var vt VerificationToken
	if err := a.db.Take(&vt, "token_hash = ?", hash).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	// Keep the signup browser signed in with a new session. A different browser
	// proving the mailbox does not inherit the old session.
	keep := currentUser(r) != nil && currentUser(r).ID == vt.UserID
	claimed, err := a.claimUnverifiedEmail(vt.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if claimed && keep {
		if err := a.startSession(w, vt.UserID); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/verify/done", http.StatusSeeOther)
}

func (a *app) handleVerifyDone(w http.ResponseWriter, r *http.Request) {
	a.messageTo(w, r, http.StatusOK, "Email verified",
		"This address is confirmed. Any session or password created before this confirmation has been removed.",
		"Back to Track Anything", "/")
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if id := currentSessionID(r); id != "" {
		if err := a.db.Delete(&Session{}, "id = ?", id).Error; err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	a.clearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
