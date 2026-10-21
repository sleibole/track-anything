package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLogUnloggedHistoryDay(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Followed diet", url.Values{"log_label": {"+ Ate"}})
	base := fmt.Sprintf("/trackers/%d", tr.ID)
	la := location("America/Los_Angeles")
	now := ts.clock.now()
	yesterday := now.In(la).AddDate(0, 0, -1).Format(dayLayout)
	today := localDay(now, la)
	tomorrow := now.In(la).AddDate(0, 0, 1).Format(dayLayout)
	dayAction := fmt.Sprintf(`action="/trackers/%d/day"`, tr.ID)

	page := owner.get(base).body
	if !strings.Contains(page, dayAction) || !strings.Contains(page, `name="day" value="`+yesterday+`"`) {
		t.Fatal("owner history is missing the day log")
	}
	if strings.Count(page, ">&#43; Ate</button>") < 2 || !strings.Contains(page, ">Record none</button>") {
		t.Fatal("unlogged day is missing the log label beside Record none")
	}
	if strings.Contains(member.get(base).body, dayAction) {
		t.Fatal("member history offers the day log")
	}

	if r := member.post(base+"/day", url.Values{"day": {yesterday}}); r.status != http.StatusForbidden || len(ts.entries(t, tr.ID)) != 0 {
		t.Fatalf("member log: %d, entries %d", r.status, len(ts.entries(t, tr.ID)))
	}
	outsider := ts.browser(t)
	outsider.signup("outsider@example.com", "")
	if r := outsider.post(base+"/day", url.Values{"day": {yesterday}}); r.status != http.StatusNotFound || len(ts.entries(t, tr.ID)) != 0 {
		t.Fatalf("outsider: %d", r.status)
	}
	if r := ts.browser(t).post(base+"/day", url.Values{"day": {yesterday}}); r.url.Path != "/login" || len(ts.entries(t, tr.ID)) != 0 {
		t.Fatalf("logged out ended at %s", r.url.Path)
	}

	for _, day := range []string{"", today, tomorrow, "2026-02-30", "nope"} {
		before := len(ts.entries(t, tr.ID))
		r := owner.post(base+"/day", url.Values{"day": {day}})
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "Pick an earlier day.") || len(ts.entries(t, tr.ID)) != before {
			t.Fatalf("day %q: %d, entries %d", day, r.status, len(ts.entries(t, tr.ID)))
		}
	}

	r := owner.post(base+"/day", url.Values{"day": {yesterday}})
	es := ts.entries(t, tr.ID)
	if r.status != http.StatusOK || len(es) != 1 {
		t.Fatalf("log: %d, entries %d\n%s", r.status, len(es), r.body)
	}
	wantAt, err := historyDayTime(yesterday, la)
	if err != nil {
		t.Fatal(err)
	}
	if !es[0].OccurredAt.Equal(wantAt) || localDay(es[0].OccurredAt, la) != yesterday {
		t.Fatalf("occurred %s, want %s on %s", es[0].OccurredAt, wantAt, yesterday)
	}
	if es[0].Note != "" || es[0].Number != nil || es[0].DurationSec != nil || es[0].ViaLink {
		t.Fatalf("entry %+v", es[0])
	}
	ownerID := ts.user(t, "owner@example.com").ID
	if es[0].RecordedByID == nil || *es[0].RecordedByID != ownerID {
		t.Fatal("entry doesn't record the owner")
	}
	if d := now.Sub(es[0].CreatedAt); d < 0 || d > time.Minute {
		t.Fatalf("created %s, now %s", es[0].CreatedAt, now)
	}
	if !strings.Contains(r.body, "12:00 PM") || !strings.Contains(r.body, `<span class="day-summary">1 time</span>`) {
		t.Fatal("history didn't show the logged day")
	}
	if !strings.Contains(r.body, "Nothing logged today") {
		t.Fatal("yesterday's log changed today's summary")
	}
	if strings.Contains(r.body, `name="day" value="`+yesterday+`"`) {
		t.Fatal("logged day still offers Log or Record none")
	}

	if r := owner.post(base+"/day", url.Values{"day": {yesterday}}); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "That day already has entries.") || len(ts.entries(t, tr.ID)) != 1 {
		t.Fatalf("second log: %d, entries %d", r.status, len(ts.entries(t, tr.ID)))
	}

	owner.post(fmt.Sprintf("/entries/%d/undo", es[0].ID), nil)
	if len(ts.entries(t, tr.ID)) != 0 {
		t.Fatal("undo within the window failed")
	}
	if r := owner.post(base+"/day", url.Values{"day": {yesterday}}); r.status != http.StatusOK {
		t.Fatalf("log after undo: %d", r.status)
	}
	es = ts.entries(t, tr.ID)
	if len(es) != 1 {
		t.Fatal("logging again after undo failed")
	}
	ts.clock.advance(16 * time.Minute)
	if r := owner.post(fmt.Sprintf("/entries/%d/undo", es[0].ID), nil); r.status != http.StatusForbidden || len(ts.entries(t, tr.ID)) != 1 {
		t.Fatalf("undo after the window: %d, entries %d", r.status, len(ts.entries(t, tr.ID)))
	}
	owner.post(fmt.Sprintf("/entries/%d/delete", es[0].ID), nil)
	if len(ts.entries(t, tr.ID)) != 0 {
		t.Fatal("owner couldn't delete the history entry")
	}

	owner.post(base+"/zero", url.Values{"day": {yesterday}})
	if r := owner.post(base+"/day", url.Values{"day": {yesterday}}); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "That day is already recorded as none.") || len(ts.entries(t, tr.ID)) != 0 || len(ts.zeros(t, tr.ID)) != 1 {
		t.Fatalf("log on none: %d, entries %d, zeros %d", r.status, len(ts.entries(t, tr.ID)), len(ts.zeros(t, tr.ID)))
	}
	nonePage := owner.get(base).body
	if strings.Contains(nonePage, `name="day" value="`+yesterday+`"`) {
		t.Fatal("a recorded zero still offers Log or Record none")
	}

	club := owner.createTracker(h, "Club", url.Values{"kind": {kindNumber}, "unit": {"lb"}})
	if r := owner.post(fmt.Sprintf("/trackers/%d/day", club.ID), url.Values{"day": {yesterday}}); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "Only a count tracker") || len(ts.entries(t, club.ID)) != 0 {
		t.Fatalf("number: %d, entries %d", r.status, len(ts.entries(t, club.ID)))
	}
	if strings.Contains(owner.get(fmt.Sprintf("/trackers/%d", club.ID)).body, fmt.Sprintf("/trackers/%d/day", club.ID)) {
		t.Fatal("number history offers a day log")
	}

	plain := owner.get(fmt.Sprintf("/trackers/%d", owner.createTracker(h, "Plain", nil).ID)).body
	if !strings.Contains(plain, ">&#43; Log</button>") || !strings.Contains(plain, "/day") {
		t.Fatal("default log label is missing on an unlogged history day")
	}

	owner.post(base+"/archive", nil)
	if r := owner.post(base+"/day", url.Values{"day": {yesterday}}); r.status != http.StatusNotFound {
		t.Fatalf("archived tracker: %d", r.status)
	}
}
