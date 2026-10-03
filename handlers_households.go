package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

// errNameTaken is returned from a create or rename transaction when the name
// matches another household the same user already owns.
var errNameTaken = errors.New("household name taken")

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
	Members   []memberView
	Trackers  []Tracker
	Archived  []Tracker // shown to owners only
	InviteURL string
	DraftName string // the name field, kept when a rename fails
	NameError string
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
	p, err := a.householdView(h, role, u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "household.html", p)
}

func (a *app) householdView(h Household, role string, u *User) (householdPage, error) {
	p := householdPage{Household: h, IsOwner: role == roleOwner}
	err := a.db.Table("household_members").
		Select("users.email, household_members.user_id, household_members.role").
		Joins("JOIN users ON users.id = household_members.user_id").
		Where("household_members.household_id = ?", h.ID).
		Order("household_members.created_at, users.id").
		Scan(&p.Members).Error
	if err != nil {
		return p, err
	}
	for i := range p.Members {
		p.Members[i].IsSelf = p.Members[i].UserID == u.ID
	}
	if err := a.db.Where("household_id = ? AND archived_at IS NULL", h.ID).Order("position, id").Find(&p.Trackers).Error; err != nil {
		return p, err
	}
	if p.IsOwner {
		if err := a.db.Where("household_id = ? AND archived_at IS NOT NULL", h.ID).Order("archived_at DESC").Find(&p.Archived).Error; err != nil {
			return p, err
		}
		if h.InviteToken != nil {
			p.InviteURL = a.cfg.BaseURL + "/join/" + *h.InviteToken
		}
	}
	return p, nil
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

// sameHouseholdName reports whether two household names match after trimming
// surrounding whitespace and ignoring case.
func sameHouseholdName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func householdNameProblem(name string) string {
	switch {
	case name == "":
		return "Give the household a name."
	case utf8.RuneCountInString(name) > maxTrackerName:
		return fmt.Sprintf("Keep the name to %d characters.", maxTrackerName)
	}
	return ""
}

// ownedNameTaken reports whether name matches a household this user owns, other than exceptID.
func ownedNameTaken(owned []Household, exceptID uint, name string) bool {
	for _, h := range owned {
		if h.ID == exceptID {
			continue
		}
		if sameHouseholdName(h.Name, name) {
			return true
		}
	}
	return false
}

func (a *app) renderDashboardError(w http.ResponseWriter, r *http.Request, u *User, name, msg string) {
	groups, err := a.dashboard(u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusUnprocessableEntity, "dashboard.html", dashboardPage{Groups: groups, Name: name, Error: msg})
}

func (a *app) handleCreateHousehold(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	name := strings.TrimSpace(r.PostFormValue("name"))
	if msg := householdNameProblem(name); msg != "" {
		a.renderDashboardError(w, r, u, name, msg)
		return
	}
	h := Household{Name: name}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		owned, err := ownedHouseholds(tx, u.ID)
		if err != nil {
			return err
		}
		if ownedNameTaken(owned, 0, name) {
			return errNameTaken
		}
		if err := tx.Create(&h).Error; err != nil {
			return err
		}
		return tx.Create(&HouseholdMember{HouseholdID: h.ID, UserID: u.ID, Role: roleOwner}).Error
	})
	if errors.Is(err, errNameTaken) {
		a.renderDashboardError(w, r, u, name, "You already have a household with that name.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectTo(w, r, householdURL(h.ID))
}

func (a *app) handleRenameHousehold(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	h, ok := a.ownedHousehold(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if msg := householdNameProblem(name); msg != "" {
		a.renderRenameError(w, r, h, u, name, msg)
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		owned, err := ownedHouseholds(tx, u.ID)
		if err != nil {
			return err
		}
		if ownedNameTaken(owned, h.ID, name) {
			return errNameTaken
		}
		return tx.Model(&Household{}).Where("id = ?", h.ID).Update("name", name).Error
	})
	if errors.Is(err, errNameTaken) {
		a.renderRenameError(w, r, h, u, name, "You already have a household with that name.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectTo(w, r, householdURL(h.ID))
}

func (a *app) renderRenameError(w http.ResponseWriter, r *http.Request, h Household, u *User, name, msg string) {
	p, err := a.householdView(h, roleOwner, u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.DraftName = name
	p.NameError = msg
	a.render(w, r, http.StatusUnprocessableEntity, "household.html", p)
}

func (a *app) handleLeaveHousehold(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	h, role, err := householdForUser(a.db, u.ID, pathID(r, "hid"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	if role != roleMember {
		a.message(w, r, http.StatusForbidden, "Owners stay", "An owner can't leave a household this way.")
		return
	}
	res := a.db.Where("household_id = ? AND user_id = ? AND role = ?", h.ID, u.ID, roleMember).Delete(&HouseholdMember{})
	if res.Error != nil {
		a.serverError(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		a.message(w, r, http.StatusForbidden, "Owners stay", "An owner can't leave a household this way.")
		return
	}
	redirectTo(w, r, "/")
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
