package main

import (
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

type settingsPage struct {
	TimeZoneLabel   string
	TimeZoneOptions []timeZoneOption
	HasPassword     bool
	EmailVerified   bool
	Saved           string // "timezone", "password", or "verification" after a successful save
	TimeZoneError   string
	PasswordError   string
	VerifyError     string
	VerifyNote      string
}

func (a *app) settingsPageFor(r *http.Request) settingsPage {
	u := currentUser(r)
	return settingsPage{
		TimeZoneLabel:   timeZoneLabel(u.TimeZone),
		TimeZoneOptions: timeZoneOptions(u.TimeZone, a.now()),
		HasPassword:     u.HasPassword(),
		EmailVerified:   u.EmailVerifiedAt != nil,
		Saved:           r.URL.Query().Get("saved"),
	}
}

func (a *app) handleSettings(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, http.StatusOK, "settings.html", a.settingsPageFor(r))
}

func (a *app) handleSettingsTimeZone(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	tz := r.PostFormValue("timezone")
	if !listedTimeZone(tz) && tz != u.TimeZone {
		page := a.settingsPageFor(r)
		page.TimeZoneError = "Pick a time zone from the list."
		a.render(w, r, http.StatusUnprocessableEntity, "settings.html", page)
		return
	}
	if err := a.db.Model(u).Update("time_zone", tz).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings?saved=timezone", http.StatusSeeOther)
}

// handleSettingsPassword sets or changes the password. It doesn't ask for the current
// one, because "forgot password" is: log in with an email link, then set a new one here.
// Every other session is logged out, so a changed password locks out anyone else.
func (a *app) handleSettingsPassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if a.turnstileFailed(r) {
		page := a.settingsPageFor(r)
		page.PasswordError = turnstileRejectedMessage
		a.render(w, r, http.StatusForbidden, "settings.html", page)
		return
	}
	password := r.PostFormValue("password")

	if u.EmailVerifiedAt == nil {
		page := a.settingsPageFor(r)
		page.PasswordError = "Confirm your email before setting a password."
		a.render(w, r, http.StatusUnprocessableEntity, "settings.html", page)
		return
	}

	msg := validatePassword(password)
	if msg == "" && password != r.PostFormValue("confirm") {
		msg = "The two passwords don't match."
	}
	if msg != "" {
		page := a.settingsPageFor(r)
		page.PasswordError = msg
		a.render(w, r, http.StatusUnprocessableEntity, "settings.html", page)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.db.Model(u).Update("password_hash", string(hash)).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.db.Where("user_id = ? AND id <> ?", u.ID, currentSessionID(r)).Delete(&Session{}).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings?saved=password", http.StatusSeeOther)
}

func (a *app) handleSettingsVerify(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.EmailVerifiedAt != nil {
		page := a.settingsPageFor(r)
		page.VerifyNote = "This email is already verified."
		a.render(w, r, http.StatusOK, "settings.html", page)
		return
	}
	if !a.verifyLimiter.allow(fmt.Sprintf("%d", u.ID)) {
		page := a.settingsPageFor(r)
		page.VerifyError = "Too many verification emails. Try again in a few minutes."
		a.render(w, r, http.StatusTooManyRequests, "settings.html", page)
		return
	}
	if err := a.sendVerificationEmail(u); err != nil {
		if errors.Is(err, errMailThrottled) {
			page := a.settingsPageFor(r)
			page.VerifyError = "Too many verification emails. Try again in a few minutes."
			a.render(w, r, http.StatusTooManyRequests, "settings.html", page)
			return
		}
		a.logger.Error("server error", "method", r.Method, "path", logPath(r.URL.Path), "err", err)
		page := a.settingsPageFor(r)
		page.VerifyError = "We couldn't send the verification email. Please try again in a moment."
		a.render(w, r, http.StatusInternalServerError, "settings.html", page)
		return
	}
	http.Redirect(w, r, "/settings?saved=verification", http.StatusSeeOther)
}
