package main

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"gorm.io/gorm"
)

type emailForm struct {
	Email string
	Next  string
	Error string
	Sent  bool
	Dev   bool
}

func (a *app) handleAddEmailForm(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !u.IsGuest() {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	next := r.URL.Query().Get("next")
	if next != "" {
		next = safeNext(next)
	}
	a.render(w, r, http.StatusOK, "account_email.html", emailForm{Next: next})
}

func (a *app) handleAddEmail(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !u.IsGuest() {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	form := emailForm{
		Email: normalizeEmail(r.PostFormValue("email")),
		Next:  r.PostFormValue("next"),
	}
	if form.Next != "" {
		form.Next = safeNext(form.Next)
	}
	fail := func(status int, msg string) {
		form.Error = msg
		a.render(w, r, status, "account_email.html", form)
	}
	if a.turnstileFailed(r) {
		fail(http.StatusForbidden, turnstileRejectedMessage)
		return
	}
	if !a.verifyLimiter.allow("guest:" + u.IDString()) {
		fail(http.StatusTooManyRequests, "Too many email messages. Try again in a few minutes.")
		return
	}
	if !validEmail(form.Email) {
		fail(http.StatusUnprocessableEntity, "Enter a valid email address.")
		return
	}
	if !a.mailAddrLimiter.allow(form.Email) {
		fail(http.StatusTooManyRequests, "Too many email messages. Try again in a few minutes.")
		return
	}

	var proof string
	var proofLife time.Duration
	var to, subject, body string
	err := a.withImmediate(func(tx *gorm.DB) error {
		var guest User
		if err := tx.Take(&guest, u.ID).Error; err != nil {
			return err
		}
		if !guest.IsGuest() {
			return errTransferDeny
		}
		if err := a.supersedeGuestClaims(tx, guest.ID, a.now()); err != nil {
			return err
		}
		var dest User
		err := tx.Take(&dest, "email = ?", form.Email).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := a.now()
		claim := GuestClaim{GuestUserID: guest.ID, Email: form.Email, Next: form.Next, CreatedAt: now}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			claim.ExpiresAt = now.Add(verifyLinkLifetime)
			if err := tx.Create(&claim).Error; err != nil {
				return err
			}
			token, err := newToken()
			if err != nil {
				return err
			}
			vt := VerificationToken{
				UserID: guest.ID, TokenHash: hashToken(token), Email: form.Email,
				ClaimID: &claim.ID, ExpiresAt: claim.ExpiresAt,
			}
			if err := tx.Create(&vt).Error; err != nil {
				return err
			}
			to, subject = form.Email, "Confirm your Track Anything email"
			body = "Confirm this email for the Track Anything account that asked to add it:\n\n" +
				a.cfg.BaseURL + "/verify/" + token +
				"\n\nThe link works once and expires in 24 hours. If you did not ask to add this address, ignore this email.\n"
			return nil
		}
		claim.ExpiresAt = now.Add(loginLinkLifetime)
		if err := tx.Create(&claim).Error; err != nil {
			return err
		}
		proof, err = newToken()
		if err != nil {
			return err
		}
		intent := TransferIntent{
			ClaimID: claim.ID, SourceProofHash: hashToken(proof),
			SourceUserID: guest.ID, SourceSessionID: currentSessionID(r),
			DestUserID: dest.ID, ExpiresAt: claim.ExpiresAt,
		}
		if err := tx.Create(&intent).Error; err != nil {
			return err
		}
		token, err := newToken()
		if err != nil {
			return err
		}
		lt := LoginToken{
			UserID: dest.ID, TokenHash: hashToken(token), TransferIntentID: &intent.ID,
			ExpiresAt: claim.ExpiresAt,
		}
		if err := tx.Create(&lt).Error; err != nil {
			return err
		}
		proofLife = loginLinkLifetime
		to, subject = dest.Email, "Your Track Anything login link"
		body = "Use this link to log in to Track Anything:\n\n" + a.cfg.BaseURL + "/login/link/" + token +
			"\n\nIt works once and expires in 15 minutes. Logging in does not move any trackers. Confirm the move in the browser where you started.\n\nIf you didn't ask for it, you can ignore this email.\n"
		return nil
	})
	if errors.Is(err, errTransferDeny) {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if proof != "" {
		a.setFlowCookie(w, transferCookie, proof, proofLife)
	}
	if err := a.mailer.Send(to, subject, body); err != nil {
		a.logger.Error("server error", "method", r.Method, "path", logPath(r.URL.Path), "err", err)
		fail(http.StatusInternalServerError, "We couldn't send the email. Please try again in a moment.")
		return
	}
	form.Sent = true
	form.Dev = a.cfg.SMTPHost == ""
	a.render(w, r, http.StatusOK, "account_email.html", form)
}

func (u User) IDString() string {
	return strconv.FormatUint(uint64(u.ID), 10)
}
