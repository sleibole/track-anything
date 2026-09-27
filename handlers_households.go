package main

import (
	"errors"
	"fmt"
	"net/http"

	"gorm.io/gorm"
)

type memberView struct {
	Email  string
	UserID uint
	Role   string
	IsSelf bool
}

func (m memberView) IsOwner() bool { return m.Role == roleOwner }

type householdPage struct {
	Household Household
	IsOwner   bool
	CreatedBy string
	Members   []memberView
	Trackers  []Tracker
	Archived  []Tracker // shown to owners only
	InviteURL string
}

func householdURL(id uint) string {
	return fmt.Sprintf("/households/%d", id)
}

func (a *app) handleHousehold(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	h, role, err := householdForUser(a.db, u.ID, pathID(r, "hid"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	p := householdPage{Household: h, IsOwner: role == roleOwner}
	if p.CreatedBy, err = a.createdByOther(h.ID, u.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	err = a.db.Table("household_members").
		Select("users.email, household_members.user_id, household_members.role").
		Joins("JOIN users ON users.id = household_members.user_id").
		Where("household_members.household_id = ?", h.ID).
		Order("household_members.created_at, users.id").
		Scan(&p.Members).Error
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for i := range p.Members {
		p.Members[i].IsSelf = p.Members[i].UserID == u.ID
	}
	if err := a.db.Where("household_id = ? AND archived_at IS NULL", h.ID).Order("position, id").Find(&p.Trackers).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	if p.IsOwner {
		if err := a.db.Where("household_id = ? AND archived_at IS NOT NULL", h.ID).Order("archived_at DESC").Find(&p.Archived).Error; err != nil {
			a.serverError(w, r, err)
			return
		}
		if h.InviteToken != nil {
			p.InviteURL = a.cfg.BaseURL + "/join/" + *h.InviteToken
		}
	}
	a.render(w, r, http.StatusOK, "household.html", p)
}

// ownedHousehold loads a household for an owner-only action, writing the 404 or 403
// itself. ok is false when the handler should stop.
func (a *app) ownedHousehold(w http.ResponseWriter, r *http.Request) (h Household, ok bool) {
	h, role, err := householdForUser(a.db, currentUser(r).ID, pathID(r, "hid"))
	if err != nil {
		a.lookupError(w, r, err)
		return h, false
	}
	if role != roleOwner {
		a.ownerOnly(w, r)
		return h, false
	}
	return h, true
}

func (a *app) handleInviteOn(w http.ResponseWriter, r *http.Request) {
	h, ok := a.ownedHousehold(w, r)
	if !ok {
		return
	}
	token, err := newToken()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.db.Model(&h).Update("invite_token", token).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, householdURL(h.ID), http.StatusSeeOther)
}

func (a *app) handleInviteOff(w http.ResponseWriter, r *http.Request) {
	h, ok := a.ownedHousehold(w, r)
	if !ok {
		return
	}
	if err := a.db.Model(&h).Update("invite_token", nil).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, householdURL(h.ID), http.StatusSeeOther)
}

// targetMember loads the member an owner is acting on. Owners can't be removed or
// demoted in phase 2, so acting on an owner is refused.
func (a *app) targetMember(w http.ResponseWriter, r *http.Request, h Household) (m HouseholdMember, ok bool) {
	if err := a.db.Take(&m, "household_id = ? AND user_id = ?", h.ID, pathID(r, "uid")).Error; err != nil {
		a.lookupError(w, r, err)
		return m, false
	}
	if m.Role == roleOwner {
		a.message(w, r, http.StatusForbidden, "Owners stay", "An owner can't be removed from a household.")
		return m, false
	}
	return m, true
}

func (a *app) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	h, ok := a.ownedHousehold(w, r)
	if !ok {
		return
	}
	m, ok := a.targetMember(w, r, h)
	if !ok {
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("household_id = ? AND user_id = ? AND role = ?", m.HouseholdID, m.UserID, roleMember).Delete(&HouseholdMember{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 || h.InviteToken == nil {
			return nil
		}
		token, err := newToken()
		if err != nil {
			return err
		}
		return tx.Model(&Household{}).Where("id = ?", h.ID).Update("invite_token", token).Error
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, householdURL(h.ID), http.StatusSeeOther)
}

func (a *app) handlePromoteMember(w http.ResponseWriter, r *http.Request) {
	h, ok := a.ownedHousehold(w, r)
	if !ok {
		return
	}
	m, ok := a.targetMember(w, r, h)
	if !ok {
		return
	}
	err := a.db.Model(&HouseholdMember{}).
		Where("household_id = ? AND user_id = ?", m.HouseholdID, m.UserID).
		Update("role", roleOwner).Error
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, householdURL(h.ID), http.StatusSeeOther)
}

// Joining through an invite link. GET only shows the invitation; joining is a POST,
// so opening a link can't add anyone to a household by itself.

type joinPage struct {
	Household     Household
	CreatedBy     string
	Token         string
	AlreadyMember bool
}

func (a *app) invitedHousehold(w http.ResponseWriter, r *http.Request) (h Household, ok bool) {
	token := r.PathValue("token")
	err := a.db.Take(&h, "invite_token = ?", token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || token == "" {
		a.message(w, r, http.StatusNotFound, "Invite link not valid",
			"This invite link was turned off or replaced. Ask for a new one.")
		return h, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return h, false
	}
	return h, true
}

func (a *app) handleJoinPage(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	h, ok := a.invitedHousehold(w, r)
	if !ok {
		return
	}
	p := joinPage{Household: h, Token: r.PathValue("token")}
	var err error
	if p.CreatedBy, err = a.createdByOther(h.ID, u.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	_, _, err = householdForUser(a.db, u.ID, h.ID)
	switch {
	case err == nil:
		p.AlreadyMember = true
	case !errors.Is(err, gorm.ErrRecordNotFound):
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "join.html", p)
}

func (a *app) handleJoin(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	h, ok := a.invitedHousehold(w, r)
	if !ok {
		return
	}
	m := HouseholdMember{HouseholdID: h.ID, UserID: u.ID, Role: roleMember}
	if err := a.db.Where(HouseholdMember{HouseholdID: h.ID, UserID: u.ID}).Attrs(m).FirstOrCreate(&m).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
