package main

import (
	"errors"
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
// the day has events. The count and the write share one transaction so a log that
// lands at the same time cannot leave both entries and a recorded zero.
func (a *app) recordZero(t Tracker, day string, loc *time.Location, z RecordedZero) (bool, error) {
	start, end, err := dayBounds(day, loc)
	if err != nil {
		return false, err
	}
	recorded := false
	err = a.db.Transaction(func(tx *gorm.DB) error {
		countEntries := func() (int64, error) {
			var n int64
			err := tx.Model(&Entry{}).
				Where("tracker_id = ? AND occurred_at >= ? AND occurred_at < ?", t.ID, start, end).
				Count(&n).Error
			return n, err
		}
		n, err := countEntries()
		if err != nil {
			return err
		}
		if n > 0 {
			return tx.Where("tracker_id = ? AND day = ?", t.ID, day).Delete(&RecordedZero{}).Error
		}
		z.TrackerID, z.Day = t.ID, day
		err = tx.Create(&z).Error
		if isUniqueConstraint(err) {
			n, err = countEntries()
			if err != nil {
				return err
			}
			if n > 0 {
				return tx.Where("tracker_id = ? AND day = ?", t.ID, day).Delete(&RecordedZero{}).Error
			}
			recorded = true
			return nil
		}
		if err != nil {
			return err
		}
		recorded = true
		return nil
	})
	return recorded, err
}

func isUniqueConstraint(err error) bool {
	return err != nil && (errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "UNIQUE constraint failed"))
}

func (a *app) tooLateToUndo(w http.ResponseWriter, r *http.Request) {
	a.message(w, r, http.StatusForbidden, "Too late to undo",
		"Undo works for 15 minutes after logging. An owner can still correct it from the tracker page.")
}

// quickEntry is the one-tap log. A count records now. A number or duration records
// now and the value of the latest entry by OccurredAt. The request body is not a
// value: a hidden field cannot replace what is carried forward.
func (a *app) quickEntry(t Tracker, now time.Time, by *uint, viaLink bool) (Entry, string, error) {
	e := Entry{TrackerID: t.ID, OccurredAt: now.UTC(), RecordedByID: by, ViaLink: viaLink, CreatedAt: now}
	if t.IsCount() {
		return e, "", nil
	}
	if t.IsCumulative() {
		return e, "Enter an amount to add.", nil
	}
	last, err := a.latestEntry(t.ID)
	if err != nil {
		return e, "", err
	}
	switch {
	case t.IsNumber():
		if last == nil || last.Number == nil {
			return e, "Log a value before logging it again.", nil
		}
		n := *last.Number
		e.Number = &n
	case t.IsDuration():
		if last == nil || last.DurationSec == nil || *last.DurationSec <= 0 {
			return e, "Log a duration before logging it again.", nil
		}
		sec := *last.DurationSec
		e.DurationSec = &sec
	default:
		return e, "This tracker can't be logged that way.", nil
	}
	return e, "", nil
}

func (a *app) handleQuickLog(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, role, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	now := a.now()
	e, msg, err := a.quickEntry(t, now, &u.ID, false)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if msg != "" {
		a.renderTrackerError(w, r, t, role, msg)
		return
	}
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

// readEntryValue reads the value that belongs to this tracker.
// A count takes neither a number nor a duration. The other kinds reject the wrong field
// instead of ignoring it.
func readEntryValue(r *http.Request, t Tracker) (number *float64, duration *int, msg string) {
	rawNumber := strings.TrimSpace(r.PostFormValue("number"))
	hours := strings.TrimSpace(r.PostFormValue("hours"))
	minutes := strings.TrimSpace(r.PostFormValue("minutes"))
	seconds := strings.TrimSpace(r.PostFormValue("seconds"))
	hasDuration := hours != "" || minutes != "" || seconds != ""
	switch {
	case t.IsCount():
		if rawNumber != "" || hasDuration {
			return nil, nil, "A count tracker doesn't take a value."
		}
		return nil, nil, ""
	case t.IsNumber():
		if hasDuration {
			return nil, nil, "A number tracker doesn't take a duration."
		}
		v, msg := parseNumber(rawNumber)
		if msg != "" {
			return nil, nil, msg
		}
		return &v, nil, ""
	case t.IsCumulative():
		if hasDuration {
			return nil, nil, "A cumulative tracker doesn't take a duration."
		}
		v, msg := parseAmount(rawNumber)
		if msg != "" {
			return nil, nil, msg
		}
		return &v, nil, ""
	case t.IsDuration():
		if rawNumber != "" {
			return nil, nil, "A duration tracker doesn't take a number."
		}
		sec, msg := parseDurationFields(r.PostFormValue("hours"), r.PostFormValue("minutes"), r.PostFormValue("seconds"))
		if msg != "" {
			return nil, nil, msg
		}
		return nil, &sec, ""
	default:
		return nil, nil, "This tracker can't be logged that way."
	}
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
	number, duration, valueMsg := readEntryValue(r, t)
	if msg == "" {
		msg = valueMsg
	}
	occurred := now.UTC()
	if v := r.PostFormValue("time"); msg == "" && role == roleOwner && v != "" {
		occurred, msg = parseEntryTime(v, loc, now)
	}
	if msg != "" {
		if r.PostFormValue("log_again") == "1" || r.PostFormValue("add_amount") == "1" {
			a.renderNumberLogError(w, r, t, role, msg)
			return
		}
		a.renderTrackerError(w, r, t, role, msg)
		return
	}

	var before []float64
	if t.IsCumulative() && t.Goal != nil {
		before, err = a.entryNumbers(t.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	e := Entry{
		TrackerID: t.ID, OccurredAt: occurred, RecordedByID: &u.ID, Note: note,
		Number: number, DurationSec: duration, CreatedAt: now,
	}
	if err := a.logEntry(&e, loc); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.redirectAfterEntry(w, r, t, trackerURL(t), before)
}

// redirectAfterEntry returns to the page the entry was logged from.
// A cumulative goal that the new amount finished adds a one-time notice.
func (a *app) redirectAfterEntry(w http.ResponseWriter, r *http.Request, t Tracker, fallback string, before []float64) {
	to := redirectTarget(r, fallback)
	if t.IsCumulative() && t.Goal != nil && *t.Goal > 0 {
		after, err := a.entryNumbers(t.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if n := completedDelta(before, after, *t.Goal); n != "" {
			to = withNotice(to, t.ID, n)
		}
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// renderNumberLogError keeps a failed Log again inside the tracker that was submitted.
// HTMX swaps that tracker's log control. A plain submit re-renders the tracker page
// with the message on the control, not in a banner above the page.
func (a *app) renderNumberLogError(w http.ResponseWriter, r *http.Request, t Tracker, role, msg string) {
	u := currentUser(r)
	loc := location(u.TimeZone)
	if r.Header.Get("HX-Request") == "true" {
		c, err := a.card(t, loc, a.now(), &u.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		applyInlineLogError(&c, r, t, msg)
		a.writeLogControl(w, r, http.StatusUnprocessableEntity, "tracker_show.html", c)
		return
	}
	overlay, err := a.resolveOverlay(u.ID, t, requestedOverlayID(r))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	p, err := a.trackerPage(t, role, u, overlay)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	applyInlineLogError(&p.Card, r, t, msg)
	a.render(w, r, http.StatusUnprocessableEntity, "tracker_show.html", p)
}

// handleLogHistoryDay records one event on an earlier count-history day that is still
// a gap: no entries and no recorded zero. The time is local noon. Owners only.
func (a *app) handleLogHistoryDay(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, role, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	if role != roleOwner {
		a.ownerOnly(w, r)
		return
	}
	if !t.IsCount() {
		a.renderTrackerError(w, r, t, role, "Only a count tracker can be logged for an earlier day.")
		return
	}
	loc := location(u.TimeZone)
	now := a.now()
	day := r.PostFormValue("day")
	if _, err := time.ParseInLocation(dayLayout, day, loc); err != nil || day >= localDay(now, loc) {
		a.renderTrackerError(w, r, t, role, "Pick an earlier day.")
		return
	}
	occurred, err := historyDayTime(day, loc)
	if err != nil {
		a.renderTrackerError(w, r, t, role, "Pick an earlier day.")
		return
	}
	e := Entry{TrackerID: t.ID, OccurredAt: occurred, RecordedByID: &u.ID, CreatedAt: now}
	msg, err := a.logGapDay(t, day, loc, &e)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if msg != "" {
		a.renderTrackerError(w, r, t, role, msg)
		return
	}
	redirectBack(w, r, trackerURL(t))
}

// logGapDay writes an entry when the local day has no entries and no recorded zero.
// A non-empty message means nothing was written. The check and the insert share one
// transaction. A recorded zero that appears in that same moment is cleared, because
// a day with an event is not none.
func (a *app) logGapDay(t Tracker, day string, loc *time.Location, e *Entry) (string, error) {
	start, end, err := dayBounds(day, loc)
	if err != nil {
		return "", err
	}
	var msg string
	err = a.db.Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&Entry{}).
			Where("tracker_id = ? AND occurred_at >= ? AND occurred_at < ?", t.ID, start, end).
			Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			msg = "That day already has entries."
			return nil
		}
		var zeros int64
		if err := tx.Model(&RecordedZero{}).
			Where("tracker_id = ? AND day = ?", t.ID, day).
			Count(&zeros).Error; err != nil {
			return err
		}
		if zeros > 0 {
			msg = "That day is already recorded as none."
			return nil
		}
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		return tx.Where("tracker_id = ? AND day = ?", t.ID, day).Delete(&RecordedZero{}).Error
	})
	return msg, err
}

// handleRecordZero marks today, or for an owner an earlier day, as none.
func (a *app) handleRecordZero(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, role, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	if !t.IsCount() {
		a.renderTrackerError(w, r, t, role, "Only a count tracker can be recorded as none.")
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
	number, duration, valueMsg := readEntryValue(r, t)
	if msg == "" {
		msg = valueMsg
	}
	occurred := e.OccurredAt
	if msg == "" {
		occurred, msg = parseEntryTime(r.PostFormValue("time"), loc, a.now())
	}
	if msg != "" {
		a.renderTrackerError(w, r, t, role, msg)
		return
	}
	updates := map[string]any{"occurred_at": occurred, "note": note}
	switch {
	case t.IsNumber(), t.IsCumulative():
		updates["number"] = *number
		updates["duration_sec"] = nil
	case t.IsDuration():
		updates["duration_sec"] = *duration
		updates["number"] = nil
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&e).Updates(updates).Error; err != nil {
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
