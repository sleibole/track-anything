package main

import (
	"net/http"
	"time"
)

// Share links are bearer tokens: anyone with the URL can log, record none for today, and
// undo recent entries on that one tracker. Nothing else is reachable through them.

type sharePage struct {
	Card    trackerCard
	Token   string
	Today   todayState
	ZeroNew bool
	Entries []entryView
	Error   string
}

func (p sharePage) Tracker() Tracker { return p.Card.Tracker }

// sharedTracker loads the tracker behind the link and the zone its "today" follows:
// the household creator's, so everyone in the house shares one day. ok is false when
// it has already written the response.
func (a *app) sharedTracker(w http.ResponseWriter, r *http.Request) (t Tracker, loc *time.Location, ok bool) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
	t, err := trackerForShareToken(a.db, r.PathValue("token"))
	if err != nil {
		a.lookupError(w, r, err)
		return t, nil, false
	}
	creator, err := householdCreator(a.db, t.HouseholdID)
	if err != nil {
		a.serverError(w, r, err)
		return t, nil, false
	}
	return t, location(creator.TimeZone), true
}

// sharePost is sharedTracker plus the rate limit on posts through a link.
func (a *app) sharePost(w http.ResponseWriter, r *http.Request) (Tracker, *time.Location, bool) {
	if !a.shareLimiter.allow(clientIP(r)) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		a.message(w, r, http.StatusTooManyRequests, "Slow down", "Too many changes from this network. Try again in a minute.")
		return Tracker{}, nil, false
	}
	return a.sharedTracker(w, r)
}

func shareURL(t Tracker) string {
	return "/s/" + *t.ShareToken
}

func (a *app) renderShare(w http.ResponseWriter, r *http.Request, status int, t Tracker, loc *time.Location, msg string) {
	now := a.now()
	today, err := a.loadToday(t, loc, now)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p := sharePage{
		Card:    cardFor(t, today, now, nil),
		Token:   *t.ShareToken,
		Today:   today,
		ZeroNew: today.Zero != nil && recent(today.Zero.CreatedAt, now),
		Entries: entryViews(t, today.Entries, loc, now, nil, nil, false),
		Error:   msg,
	}
	a.render(w, r, status, "share.html", p)
}

func (a *app) handleShare(w http.ResponseWriter, r *http.Request) {
	t, loc, ok := a.sharedTracker(w, r)
	if !ok {
		return
	}
	a.renderShare(w, r, http.StatusOK, t, loc, "")
}

func (a *app) handleShareQuickLog(w http.ResponseWriter, r *http.Request) {
	t, loc, ok := a.sharePost(w, r)
	if !ok {
		return
	}
	now := a.now()
	e := Entry{TrackerID: t.ID, OccurredAt: now.UTC(), ViaLink: true, CreatedAt: now}
	if err := a.logEntry(&e, loc); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, shareURL(t), http.StatusSeeOther)
}

func (a *app) handleShareZero(w http.ResponseWriter, r *http.Request) {
	t, loc, ok := a.sharePost(w, r)
	if !ok {
		return
	}
	now := a.now()
	ok, err := a.recordZero(t, localDay(now, loc), loc, RecordedZero{ViaLink: true, CreatedAt: now})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !ok {
		a.renderShare(w, r, http.StatusUnprocessableEntity, t, loc, "Today already has entries, so it can't be none.")
		return
	}
	http.Redirect(w, r, shareURL(t), http.StatusSeeOther)
}

func (a *app) handleShareUndoEntry(w http.ResponseWriter, r *http.Request) {
	t, _, ok := a.sharePost(w, r)
	if !ok {
		return
	}
	var e Entry
	if err := a.db.Take(&e, "id = ? AND tracker_id = ?", pathID(r, "eid"), t.ID).Error; err != nil {
		a.lookupError(w, r, err)
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
	http.Redirect(w, r, shareURL(t), http.StatusSeeOther)
}

func (a *app) handleShareUndoZero(w http.ResponseWriter, r *http.Request) {
	t, _, ok := a.sharePost(w, r)
	if !ok {
		return
	}
	var z RecordedZero
	if err := a.db.Take(&z, "id = ? AND tracker_id = ?", pathID(r, "zid"), t.ID).Error; err != nil {
		a.lookupError(w, r, err)
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
	http.Redirect(w, r, shareURL(t), http.StatusSeeOther)
}
