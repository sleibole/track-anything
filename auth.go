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
	sessionCookie     = "session"
	sessionLifetime   = 30 * 24 * time.Hour
	loginLinkLifetime = 15 * time.Minute
	minPasswordLen    = 8
	maxPasswordLen    = 72 // bcrypt ignores bytes past 72
)

// newToken returns 32 random bytes, base64url-encoded, for session IDs and login links.
func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
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

// safeNext keeps post-login redirects on this site.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
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

func (a *app) setSessionCookie(w http.ResponseWriter, s Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.ID,
		Path:     "/",
		Expires:  s.ExpiresAt,
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
	s := Session{ID: newToken(), UserID: userID, ExpiresAt: a.now().Add(sessionLifetime)}
	if err := a.db.Create(&s).Error; err != nil {
		return err
	}
	a.setSessionCookie(w, s)
	return nil
}

// loadUser puts the logged-in user, if any, into the request context. Sessions renew
// once they're past half their lifetime, so active users stay logged in.
func (a *app) loadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}

		var s Session
		err = a.db.Take(&s, "id = ?", c.Value).Error
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
			a.setSessionCookie(w, s)
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

	if !a.signupLimiter.allow(clientIP(r)) {
		fail(http.StatusTooManyRequests, "Too many signups from this network. Try again later.")
		return
	}
	if !validEmail(form.Email) {
		fail(http.StatusUnprocessableEntity, "Enter a valid email address.")
		return
	}

	password := r.PostFormValue("password")
	var hash []byte
	if password != "" {
		if msg := validatePassword(password); msg != "" {
			fail(http.StatusUnprocessableEntity, msg)
			return
		}
		var err error
		if hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost); err != nil {
			a.serverError(w, r, err)
			return
		}
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

	u := User{Email: form.Email, PasswordHash: string(hash), TimeZone: tz}
	if err := a.db.Create(&u).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.startSession(w, u.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, safeNext(form.Next), http.StatusSeeOther)
}

// Password login

type loginForm struct {
	Email string
	Next  string
	Error string
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
		Email: normalizeEmail(r.PostFormValue("email")),
		Next:  r.PostFormValue("next"),
	}
	fail := func(status int, msg string) {
		form.Error = msg
		a.render(w, r, status, "login.html", form)
	}

	if !a.loginLimiter.allow(clientIP(r)) {
		fail(http.StatusTooManyRequests, "Too many login attempts. Wait a minute and try again.")
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
	if err != nil || !u.HasPassword() || !passwordOK {
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

	if !a.linkLimiter.allow(clientIP(r)) {
		form.Error = "Too many email links requested. Try again in a few minutes."
		a.render(w, r, http.StatusTooManyRequests, "login.html", form)
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
		if err := a.sendLoginLink(u, safeNext(form.Next)); err != nil {
			a.serverError(w, r, err)
			return
		}
	}

	a.render(w, r, http.StatusOK, "login_sent.html", linkSentPage{Email: form.Email, Dev: a.cfg.SMTPHost == ""})
}

func (a *app) sendLoginLink(u User, next string) error {
	token := newToken()
	lt := LoginToken{UserID: u.ID, TokenHash: hashToken(token), ExpiresAt: a.now().Add(loginLinkLifetime)}
	if err := a.db.Create(&lt).Error; err != nil {
		return err
	}

	link := a.cfg.BaseURL + "/login/link/" + token
	if next != "/" {
		link += "?next=" + url.QueryEscape(next)
	}
	body := "Use this link to log in to Track Anything:\n\n" + link +
		"\n\nIt works once and expires in 15 minutes. If you didn't ask for it, you can ignore this email.\n"
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

// handleLoginLinkConfirm shows a button instead of logging in on GET, because email
// security scanners open links before the person does and would use up the token.
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
	if err := a.startSession(w, lt.UserID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, safeNext(r.PostFormValue("next")), http.StatusSeeOther)
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
