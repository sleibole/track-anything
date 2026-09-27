package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

func trackerURL(t Tracker) string {
	return fmt.Sprintf("/trackers/%d", t.ID)
}

// logEntry saves an event and clears that day's recorded zero, because a day with
// events can't also be "none". CreatedAt is set by the caller from the app clock,
// since it drives the undo window.
func (a *app) logEntry(e *Entry, loc *time.Location) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		return tx.Where("tracker_id = ? AND day = ?", e.TrackerID, localDay(e.OccurredAt, loc)).Delete(&RecordedZero{}).Error
	})
}

// recordZero marks a day as none unless it already has events. It returns false when
// the day has events.
func (a *app) recordZero(t Tracker, day string, loc *time.Location, z RecordedZero) (bool, error) {
	start, end, err := dayBounds(day, loc)
	if err != nil {
		return false, err
	}
	var n int64
	if err := a.db.Model(&Entry{}).Where("tracker_id = ? AND occurred_at >= ? AND occurred_at < ?", t.ID, start, end).Count(&n).Error; err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	z.TrackerID, z.Day = t.ID, day
	err = a.db.Where(RecordedZero{TrackerID: t.ID, Day: day}).Attrs(z).FirstOrCreate(&RecordedZero{}).Error
	return err == nil, err
}

func (a *app) tooLateToUndo(w http.ResponseWriter, r *http.Request) {
	a.message(w, r, http.StatusForbidden, "Too late to undo",
		"Undo works for 15 minutes after logging. An owner can still correct it from the tracker page.")
}

func (a *app) handleQuickLog(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, _, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	now := a.now()
	e := Entry{TrackerID: t.ID, OccurredAt: now.UTC(), RecordedByID: &u.ID, CreatedAt: now}
	if err := a.logEntry(&e, location(u.TimeZone)); err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectBack(w, r, trackerURL(t))
}

// parseEntryTime reads an owner's time field in their zone.
func parseEntryTime(v string, loc *time.Location, now time.Time) (time.Time, string) {
	t, err := time.ParseInLocation(timeInputLayout, v, loc)
	if err != nil {
		return t, "Enter a valid date and time."
	}
	if t.After(now.Add(time.Minute)) {
		return t, "That time hasn't happened yet."
	}
	return t.UTC(), ""
}

func readNote(r *http.Request) (string, string) {
	note := strings.TrimSpace(r.PostFormValue("note"))
	if utf8.RuneCountInString(note) > maxNote {
		return note, fmt.Sprintf("Keep the note to %d characters.", maxNote)
	}
	return note, ""
}

// handleLogEntry logs with an optional note. Only an owner's time field is read; a
// member's entry always happens now, whatever the request says.
func (a *app) handleLogEntry(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, role, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	loc := location(u.TimeZone)
	now := a.now()

	note, msg := readNote(r)
	occurred := now.UTC()
	if v := r.PostFormValue("time"); msg == "" && role == roleOwner && v != "" {
		occurred, msg = parseEntryTime(v, loc, now)
	}
	if msg != "" {
		a.renderTrackerError(w, r, t, role, msg)
		return
	}

	e := Entry{TrackerID: t.ID, OccurredAt: occurred, RecordedByID: &u.ID, Note: note, CreatedAt: now}
	if err := a.logEntry(&e, loc); err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectBack(w, r, trackerURL(t))
}

// handleRecordZero marks today, or for an owner an earlier day, as none.
func (a *app) handleRecordZero(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, role, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	loc := location(u.TimeZone)
	now := a.now()
	today := localDay(now, loc)
	day := r.PostFormValue("day")
	if day == "" {
		day = today
	}
	if day != today && role != roleOwner {
		a.ownerOnly(w, r)
		return
	}
	if _, err := time.ParseInLocation(dayLayout, day, loc); err != nil || day > today {
		a.renderTrackerError(w, r, t, role, "Pick a day that has already started.")
		return
	}
	ok, err := a.recordZero(t, day, loc, RecordedZero{RecordedByID: &u.ID, CreatedAt: now})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !ok {
		a.renderTrackerError(w, r, t, role, "That day already has entries, so it can't be none.")
		return
	}
	redirectBack(w, r, trackerURL(t))
}

// entryForUser loads an entry on a tracker the user can see. ok is false when it has
// already written a 404 or 500.
func (a *app) entryForUser(w http.ResponseWriter, r *http.Request) (e Entry, t Tracker, role string, ok bool) {
	if err := a.db.Take(&e, pathID(r, "eid")).Error; err != nil {
		a.lookupError(w, r, err)
		return e, t, "", false
	}
	t, role, err := trackerForUser(a.db, currentUser(r).ID, e.TrackerID)
	if err != nil {
		a.lookupError(w, r, err)
		return e, t, "", false
	}
	return e, t, role, true
}

func (a *app) handleUndoEntry(w http.ResponseWriter, r *http.Request) {
	e, t, _, ok := a.entryForUser(w, r)
	if !ok {
		return
	}
	if !recent(e.CreatedAt, a.now()) {
		a.tooLateToUndo(w, r)
		return
	}
	if err := a.db.Delete(&e).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectBack(w, r, trackerURL(t))
}

func (a *app) handleEditEntry(w http.ResponseWriter, r *http.Request) {
	e, t, role, ok := a.entryForUser(w, r)
	if !ok {
		return
	}
	if role != roleOwner {
		a.ownerOnly(w, r)
		return
	}
	loc := location(currentUser(r).TimeZone)
	note, msg := readNote(r)
	occurred := e.OccurredAt
	if msg == "" {
		occurred, msg = parseEntryTime(r.PostFormValue("time"), loc, a.now())
	}
	if msg != "" {
		a.renderTrackerError(w, r, t, role, msg)
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&e).Updates(map[string]any{"occurred_at": occurred, "note": note}).Error; err != nil {
			return err
		}
		return tx.Where("tracker_id = ? AND day = ?", t.ID, localDay(occurred, loc)).Delete(&RecordedZero{}).Error
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectBack(w, r, trackerURL(t))
}

func (a *app) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	e, t, role, ok := a.entryForUser(w, r)
	if !ok {
		return
	}
	if role != roleOwner {
		a.ownerOnly(w, r)
		return
	}
	if err := a.db.Delete(&e).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectBack(w, r, trackerURL(t))
}

func (a *app) zeroForUser(w http.ResponseWriter, r *http.Request) (z RecordedZero, t Tracker, role string, ok bool) {
	if err := a.db.Take(&z, pathID(r, "zid")).Error; err != nil {
		a.lookupError(w, r, err)
		return z, t, "", false
	}
	t, role, err := trackerForUser(a.db, currentUser(r).ID, z.TrackerID)
	if err != nil {
		a.lookupError(w, r, err)
		return z, t, "", false
	}
	return z, t, role, true
}

func (a *app) handleUndoZero(w http.ResponseWriter, r *http.Request) {
	z, t, _, ok := a.zeroForUser(w, r)
	if !ok {
		return
	}
	if !recent(z.CreatedAt, a.now()) {
		a.tooLateToUndo(w, r)
		return
	}
	if err := a.db.Delete(&z).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectBack(w, r, trackerURL(t))
}

func (a *app) handleDeleteZero(w http.ResponseWriter, r *http.Request) {
	z, t, role, ok := a.zeroForUser(w, r)
	if !ok {
		return
	}
	if role != roleOwner {
		a.ownerOnly(w, r)
		return
	}
	if err := a.db.Delete(&z).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectBack(w, r, trackerURL(t))
}
