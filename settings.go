package main

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

type settingsPage struct {
	TimeZoneLabel   string
	TimeZoneOptions []timeZoneOption
	HasPassword     bool
	Saved           string // "timezone" or "password" after a successful save
	TimeZoneError   string
	PasswordError   string
}

func (a *app) settingsPageFor(r *http.Request) settingsPage {
	u := currentUser(r)
	return settingsPage{
		TimeZoneLabel:   timeZoneLabel(u.TimeZone),
		TimeZoneOptions: timeZoneOptions(u.TimeZone, a.now()),
		HasPassword:     u.HasPassword(),
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
	password := r.PostFormValue("password")

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
