package main

import (
	"errors"
	"net/http"
	"strconv"

	"gorm.io/gorm"
)

// errOrderConflict means the posted ids are not the list that is on the page now.
// Nothing is written. Repeating a list that does match is safe.
var errOrderConflict = errors.New("order conflict")

// orderIDs reads the posted id fields in order. Duplicates, blanks, and non-numeric
// values are rejected so a bad request cannot rewrite part of the list.
func orderIDs(r *http.Request) ([]uint, bool) {
	if err := r.ParseForm(); err != nil {
		return nil, false
	}
	raw := r.PostForm["id"]
	if len(raw) == 0 {
		return nil, false
	}
	ids := make([]uint, 0, len(raw))
	seen := make(map[uint]struct{}, len(raw))
	for _, s := range raw {
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil || n == 0 {
			return nil, false
		}
		id := uint(n)
		if _, ok := seen[id]; ok {
			return nil, false
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, true
}

func sameIDSet(a, b []uint) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[uint]struct{}, len(a))
	for _, id := range a {
		seen[id] = struct{}{}
	}
	if len(seen) != len(a) {
		return false
	}
	for _, id := range b {
		if _, ok := seen[id]; !ok {
			return false
		}
	}
	return true
}

func (a *app) finishOrder(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if errors.Is(err, errOrderConflict) {
		a.message(w, r, http.StatusConflict, "Order changed", "This list changed somewhere else. Reload and try again.")
		return
	}
	a.serverError(w, r, err)
}

func (a *app) badOrder(w http.ResponseWriter, r *http.Request) {
	a.message(w, r, http.StatusUnprocessableEntity, "Couldn't reorder", "That order wasn't valid.")
}

// handleReorderHouseholds saves the signed-in user's household order.
// The ids must be exactly the households they belong to. Another person's order is untouched.
func (a *app) handleReorderHouseholds(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	ids, ok := orderIDs(r)
	if !ok {
		a.badOrder(w, r)
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		var members []HouseholdMember
		if err := tx.Where("user_id = ?", u.ID).Find(&members).Error; err != nil {
			return err
		}
		have := make([]uint, len(members))
		for i, m := range members {
			have[i] = m.HouseholdID
		}
		if !sameIDSet(have, ids) {
			return errOrderConflict
		}
		for i, id := range ids {
			// A repeated save writes the same positions. SQLite then reports no
			// changed rows, so this does not treat that as a missing membership.
			err := tx.Model(&HouseholdMember{}).
				Where("user_id = ? AND household_id = ?", u.ID, id).
				Update("position", i+1).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
	a.finishOrder(w, r, err)
}

// handleReorderHome saves the signed-in user's own order for their home list.
// The ids must be exactly the active trackers they can see, across households.
// It does not move a tracker, and it does not change anyone else's order or a
// household's shared order.
func (a *app) handleReorderHome(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	ids, ok := orderIDs(r)
	if !ok {
		a.badOrder(w, r)
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		var have []uint
		err := tx.Model(&Tracker{}).
			Joins("JOIN household_members ON household_members.household_id = trackers.household_id").
			Where("household_members.user_id = ? AND trackers.archived_at IS NULL", u.ID).
			Pluck("trackers.id", &have).Error
		if err != nil {
			return err
		}
		if !sameIDSet(have, ids) {
			return errOrderConflict
		}
		// Rows for trackers they can no longer see go too, so a rejoin appends.
		if err := tx.Where("user_id = ?", u.ID).Delete(&TrackerOrder{}).Error; err != nil {
			return err
		}
		rows := make([]TrackerOrder, len(ids))
		for i, id := range ids {
			rows[i] = TrackerOrder{UserID: u.ID, TrackerID: id, Position: i + 1}
		}
		return tx.Create(&rows).Error
	})
	a.finishOrder(w, r, err)
}

// handleReorderTrackers saves the order of active trackers in one household.
// The owner is the only one who can change it, because the order is shared.
// The ids must be exactly that household's active trackers, so a drag cannot move
// a tracker into another household or drop one that was added in another tab.
func (a *app) handleReorderTrackers(w http.ResponseWriter, r *http.Request) {
	h, role, err := householdForUser(a.db, currentUser(r).ID, pathID(r, "hid"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	if role != roleOwner {
		a.ownerOnly(w, r)
		return
	}
	ids, ok := orderIDs(r)
	if !ok {
		a.badOrder(w, r)
		return
	}
	err = a.db.Transaction(func(tx *gorm.DB) error {
		var active []Tracker
		if err := tx.Where("household_id = ? AND archived_at IS NULL", h.ID).Find(&active).Error; err != nil {
			return err
		}
		have := make([]uint, len(active))
		for i, t := range active {
			have[i] = t.ID
		}
		if !sameIDSet(have, ids) {
			return errOrderConflict
		}
		for i, id := range ids {
			err := tx.Model(&Tracker{}).Where("id = ? AND household_id = ?", id, h.ID).
				Update("position", i+1).Error
			if err != nil {
				return err
			}
		}
		// Keep archived trackers after the visible list so a later restore appends.
		var archived []Tracker
		if err := tx.Where("household_id = ? AND archived_at IS NOT NULL", h.ID).
			Order("position, id").Find(&archived).Error; err != nil {
			return err
		}
		base := len(ids)
		for i, t := range archived {
			if err := tx.Model(&Tracker{}).Where("id = ?", t.ID).Update("position", base+i+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
	a.finishOrder(w, r, err)
}
