package main

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCumulativeMath(t *testing.T) {
	if _, msg := parseAmount("-1"); msg == "" {
		t.Fatal("negative amount was accepted")
	}
	if v, msg := parseAmount("0"); msg != "" || v != 0 {
		t.Fatalf("zero amount: %v %q", v, msg)
	}
	if v, msg := parseAmount("10.5"); msg != "" || v != 10.5 {
		t.Fatalf("decimal amount: %v %q", v, msg)
	}
	if g, msg := parseGoal(""); msg != "" || g != nil {
		t.Fatal("blank goal should be allowed")
	}
	for _, raw := range []string{"0", "-1", "-0.5"} {
		if _, msg := parseGoal(raw); msg == "" {
			t.Errorf("goal %q was accepted", raw)
		}
	}

	none := progressStateOf(nil, nil, "points")
	if none.Headline != "0 points" || none.HasGoal || none.Completed != "" {
		t.Fatalf("empty tracker: %+v", none)
	}
	sum := progressStateOf([]float64{1.5, 2.5}, nil, "points")
	if sum.Headline != "4 points" {
		t.Fatalf("sum: %+v", sum)
	}

	goal := 32.0
	below := progressStateOf([]float64{30}, &goal, "points")
	if below.Headline != "30 / 32 points" || below.Completed != "0 goals completed" || below.Earned != "30 points total earned" {
		t.Fatalf("below goal: %+v", below)
	}
	carry := progressStateOf([]float64{30, 10}, &goal, "points")
	if carry.Headline != "8 / 32 points" || carry.Completed != "1 goal completed" || carry.Earned != "40 points total earned" || carry.Progress != "8" {
		t.Fatalf("carryover: %+v", carry)
	}
	exact := progressStateOf([]float64{32}, &goal, "points")
	if exact.Headline != "0 / 32 points" || exact.Completed != "1 goal completed" || exact.Progress != "0" {
		t.Fatalf("exact hit: %+v", exact)
	}
	many := progressStateOf([]float64{70}, &goal, "points")
	if many.Headline != "6 / 32 points" || many.Completed != "2 goals completed" || many.Earned != "70 points total earned" {
		t.Fatalf("several goals: %+v", many)
	}
	if completedDelta([]float64{30}, []float64{30, 10}, goal) != "1" {
		t.Fatal("one crossing was not noticed")
	}
	if completedDelta([]float64{}, []float64{70}, goal) != "2" {
		t.Fatal("two crossings were not noticed")
	}
	if completedDelta([]float64{30}, []float64{30, 1}, goal) != "" {
		t.Fatal("an entry under the goal looked like a crossing")
	}

	tenth := 0.1
	tenths := progressStateOf([]float64{0.1, 0.1, 0.1}, &tenth, "points")
	if tenths.Completed != "3 goals completed" || tenths.Progress != "0" || tenths.Headline != "0 / 0.1 points" {
		t.Fatalf("0.1 boundary: %+v", tenths)
	}
	third := 0.3
	parts := progressStateOf([]float64{0.1, 0.2}, &third, "points")
	if parts.Completed != "1 goal completed" || parts.Progress != "0" || parts.Earned != "0.3 points total earned" {
		t.Fatalf("0.3 boundary: %+v", parts)
	}
}

func TestCumulativeCreateAndAmounts(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	empty := owner.createTracker(h, "Plain total", url.Values{"kind": {kindCumulative}, "unit": {"points"}})
	if empty.Kind != kindCumulative || empty.Unit != "points" || empty.Goal != nil {
		t.Fatalf("created %+v", empty)
	}
	page := owner.get(fmt.Sprintf("/trackers/%d", empty.ID))
	if page.status != http.StatusOK || !strings.Contains(page.body, "0 points") || strings.Contains(page.body, "goal-progress") || strings.Contains(page.body, "Log again") {
		t.Fatal("empty cumulative tracker did not show zero")
	}
	if !strings.Contains(page.body, ">Add points</button>") {
		t.Fatal("button was not Add points")
	}
	home := owner.get("/")
	if !strings.Contains(home.body, "0 points") || !strings.Contains(home.body, ">Add points</button>") {
		t.Fatal("home card did not show the total and add button")
	}

	for _, goal := range []string{"0", "-2"} {
		bad := owner.post("/trackers", url.Values{
			"household": {fmt.Sprint(h.ID)}, "name": {"Nope"}, "kind": {kindCumulative}, "unit": {"points"}, "goal": {goal},
		})
		if bad.status != http.StatusUnprocessableEntity || !strings.Contains(bad.body, "greater than zero") {
			t.Fatalf("goal %q: %d", goal, bad.status)
		}
	}
	symbol := owner.createTracker(h, "Temp", url.Values{"kind": {kindCumulative}, "unit": {"°F"}})
	if !strings.Contains(owner.get(fmt.Sprintf("/trackers/%d", symbol.ID)).body, ">Add amount</button>") {
		t.Fatal("a symbolic unit should fall back to Add amount")
	}

	tr := owner.createTracker(h, "Test points", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	if tr.Goal == nil || *tr.Goal != 32 {
		t.Fatalf("goal %+v", tr.Goal)
	}
	start := owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body
	for _, want := range []string{"0 / 32 points", "0 goals completed", "0 points total earned", `value="0"`, `max="32"`} {
		if !strings.Contains(start, want) {
			t.Errorf("new goal tracker missing %q", want)
		}
	}
	edit := owner.get(fmt.Sprintf("/trackers/%d/edit", tr.ID)).body
	if !strings.Contains(edit, "extra amount counts toward the next one") || !strings.Contains(edit, "recalculates progress from the full total") {
		t.Fatal("goal help was missing")
	}

	neg := owner.post(fmt.Sprintf("/trackers/%d/entries", tr.ID), url.Values{"number": {"-1"}, "add_amount": {"1"}})
	if neg.status != http.StatusUnprocessableEntity || !strings.Contains(neg.body, "Enter zero or a positive number.") || len(ts.entries(t, tr.ID)) != 0 {
		t.Fatalf("negative: %d entries %d", neg.status, len(ts.entries(t, tr.ID)))
	}
	if r := owner.post(fmt.Sprintf("/trackers/%d/quick", tr.ID), nil); r.status != http.StatusUnprocessableEntity || len(ts.entries(t, tr.ID)) != 0 {
		t.Fatal("quick log repeated or created an amount")
	}

	zero := owner.post(fmt.Sprintf("/trackers/%d/entries", tr.ID), url.Values{"number": {"0"}, "note": {"none earned"}})
	if zero.status != http.StatusOK || len(ts.entries(t, tr.ID)) != 1 || ts.entries(t, tr.ID)[0].Number == nil || *ts.entries(t, tr.ID)[0].Number != 0 {
		t.Fatalf("zero amount: %d %+v", zero.status, ts.entries(t, tr.ID))
	}
	if strings.Contains(zero.body, "Goal completed!") || !strings.Contains(zero.body, "none earned") {
		t.Fatal("a zero amount changed the celebration or dropped the note")
	}
}

func TestCumulativeProgress(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Test points", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	path := fmt.Sprintf("/trackers/%d/entries", tr.ID)

	first := owner.post(path, url.Values{"number": {"30"}, "note": {"quiz"}})
	if first.status != http.StatusOK || strings.Contains(first.url.RawQuery, "notice=goal") || strings.Contains(first.body, "Goal completed!") {
		t.Fatalf("30 points looked like a goal: %s", first.url)
	}
	body := first.body
	for _, want := range []string{"30 / 32 points", "0 goals completed", "30 points total earned", "— 30 points", "30 points total"} {
		if !strings.Contains(body, want) {
			t.Errorf("after 30, missing %q", want)
		}
	}
	if strings.Contains(owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body, "Goal completed!") {
		t.Fatal("opening the tracker celebrated a goal")
	}

	second := owner.post(path, url.Values{"number": {"10"}})
	if second.status != http.StatusOK || !strings.Contains(second.url.RawQuery, "notice=goal") || !strings.Contains(second.body, "Goal completed!") {
		t.Fatalf("carryover notice: %s", second.url)
	}
	for _, want := range []string{"8 / 32 points", "1 goal completed", "40 points total earned", `value="8"`, `max="32"`} {
		if !strings.Contains(second.body, want) {
			t.Errorf("after 40, missing %q", want)
		}
	}
	memberPage := member.get(fmt.Sprintf("/trackers/%d", tr.ID)).body
	if !strings.Contains(memberPage, "8 / 32 points") || !strings.Contains(memberPage, "1 goal completed") || !strings.Contains(memberPage, "40 points total earned") || strings.Contains(memberPage, "Goal completed!") {
		t.Fatal("member page did not keep the lifetime progress without the celebration")
	}
	if strings.Contains(owner.get("/").body, "Goal completed!") || !strings.Contains(owner.get("/").body, "8 / 32 points") {
		t.Fatal("home repeated the celebration or lost the progress")
	}

	chart := valueChartFromPage(t, second.body)
	if chart.Kind != kindCumulative || len(chart.Points) != 2 || chart.Points[0].Y != 30 || chart.Points[1].Y != 40 {
		t.Fatalf("chart %+v", chart.Points)
	}
	if !strings.Contains(chart.Points[1].Text, "40 total") {
		t.Fatalf("chart text %q", chart.Points[1].Text)
	}

	owner.post(fmt.Sprintf("/trackers/%d/share", tr.ID), nil)
	token := *ts.tracker(t, tr.ID).ShareToken
	anon := ts.browser(t)
	shared := anon.post("/s/"+token+"/entries", url.Values{"number": {"2"}})
	if shared.status != http.StatusOK || !strings.Contains(shared.body, "10 / 32 points") || !strings.Contains(shared.body, "42 points total earned") {
		t.Fatalf("share log did not add to the same total")
	}
	if !strings.Contains(owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body, "42 points total earned") {
		t.Fatal("owner did not see the share-link amount")
	}

	got := csvRows(t, owner, tr.ID)
	if len(got) != 4 || got[1][3] != "30" || got[2][3] != "10" || got[3][3] != "2" || got[1][4] != "points" {
		t.Fatalf("export %+v", got)
	}
}

func TestCumulativeSeveralGoalsAndExactHit(t *testing.T) {
	_, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "Books", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	hit := owner.post(fmt.Sprintf("/trackers/%d/entries", tr.ID), url.Values{"number": {"32"}})
	if !strings.Contains(hit.body, "Goal completed!") || !strings.Contains(hit.body, "0 / 32 points") || !strings.Contains(hit.body, "1 goal completed") || !strings.Contains(hit.body, "32 points total earned") {
		t.Fatal("an exact hit did not reset progress and keep the count")
	}
	again := owner.get(fmt.Sprintf("/trackers/%d", tr.ID))
	if strings.Contains(again.body, "Goal completed!") || !strings.Contains(again.body, "0 / 32 points") {
		t.Fatal("reloading repeated the celebration or lost the exact hit")
	}

	more := owner.createTracker(h, "One entry", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	multi := owner.post(fmt.Sprintf("/trackers/%d/entries", more.ID), url.Values{"number": {"70"}, "back": {"/"}})
	if !strings.Contains(multi.body, "2 goals completed!") || !strings.Contains(multi.url.RawQuery, "notice=goal") {
		t.Fatalf("home notice: %s", multi.url)
	}
	if !strings.Contains(multi.body, "6 / 32 points") || !strings.Contains(multi.body, "70 points total earned") {
		t.Fatal("one entry did not finish two goals")
	}
	if strings.Contains(owner.get("/").body, "goals completed!") {
		t.Fatal("home celebrated again on a normal load")
	}
}

func TestCumulativeDecimals(t *testing.T) {
	_, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "Tenths", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"0.1"}})
	path := fmt.Sprintf("/trackers/%d/entries", tr.ID)
	var last response
	for range 3 {
		last = owner.post(path, url.Values{"number": {"0.1"}})
		if last.status != http.StatusOK {
			t.Fatalf("log 0.1: %d", last.status)
		}
	}
	if !strings.Contains(last.body, "0 / 0.1 points") || !strings.Contains(last.body, "3 goals completed") || !strings.Contains(last.body, "0.3 points total earned") {
		t.Fatal("three tenths did not land on the goal")
	}
	other := owner.createTracker(h, "Three tenths", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"0.3"}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", other.ID), url.Values{"number": {"0.1"}})
	landed := owner.post(fmt.Sprintf("/trackers/%d/entries", other.ID), url.Values{"number": {"0.2"}})
	if !strings.Contains(landed.body, "0 / 0.3 points") || !strings.Contains(landed.body, "1 goal completed") || !strings.Contains(landed.body, "Goal completed!") {
		t.Fatal("0.1 + 0.2 did not finish a 0.3 goal")
	}
}

func TestCumulativeEditDeleteAndGoal(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Test points", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", tr.ID), url.Values{"number": {"70"}})
	entries := ts.entries(t, tr.ID)
	if len(entries) != 1 {
		t.Fatal(len(entries))
	}
	when := entries[0].OccurredAt.In(location("America/Los_Angeles")).Format(timeInputLayout)
	edited := owner.post(fmt.Sprintf("/entries/%d", entries[0].ID), url.Values{"time": {when}, "number": {"30"}, "note": {"corrected"}})
	if edited.status != http.StatusOK || strings.Contains(edited.url.RawQuery, "notice=goal") || strings.Contains(edited.body, "Goal completed!") {
		t.Fatal("editing an entry celebrated a goal")
	}
	if !strings.Contains(edited.body, "30 / 32 points") || !strings.Contains(edited.body, "0 goals completed") || !strings.Contains(edited.body, "30 points total earned") || !strings.Contains(edited.body, "corrected") {
		t.Fatal("editing the amount did not recalculate")
	}
	if ts.tracker(t, tr.ID).Goal == nil || *ts.tracker(t, tr.ID).Goal != 32 {
		t.Fatal("editing an entry changed the goal")
	}

	locked := owner.post(fmt.Sprintf("/trackers/%d", tr.ID), url.Values{"name": {"Test points"}, "kind": {kindNumber}, "unit": {"lb"}})
	if locked.status != http.StatusUnprocessableEntity || ts.tracker(t, tr.ID).Kind != kindCumulative {
		t.Fatalf("kind changed after history: %d %s", locked.status, ts.tracker(t, tr.ID).Kind)
	}

	retarget := owner.post(fmt.Sprintf("/trackers/%d", tr.ID), url.Values{
		"name": {"Test points"}, "kind": {kindCumulative}, "unit": {"points"}, "goal": {"10"},
	})
	if retarget.status != http.StatusOK || !strings.Contains(retarget.body, "0 / 10 points") || !strings.Contains(retarget.body, "3 goals completed") || !strings.Contains(retarget.body, "30 points total earned") {
		t.Fatal("a new goal did not recalculate the same entries")
	}
	if len(ts.entries(t, tr.ID)) != 1 || *ts.entries(t, tr.ID)[0].Number != 30 {
		t.Fatal("changing the goal changed an entry")
	}
	cleared := owner.post(fmt.Sprintf("/trackers/%d", tr.ID), url.Values{
		"name": {"Test points"}, "kind": {kindCumulative}, "unit": {"points"}, "goal": {""},
	})
	if cleared.status != http.StatusOK || ts.tracker(t, tr.ID).Goal != nil || !strings.Contains(cleared.body, "30 points") || strings.Contains(cleared.body, "goal-progress") || strings.Contains(cleared.body, "goals completed") {
		t.Fatal("removing the goal did not return to the lifetime total")
	}
	owner.post(fmt.Sprintf("/trackers/%d", tr.ID), url.Values{
		"name": {"Test points"}, "kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"},
	})
	if r := member.post(fmt.Sprintf("/trackers/%d", tr.ID), url.Values{"name": {"Test points"}, "kind": {kindCumulative}, "goal": {"1"}}); r.status != http.StatusForbidden {
		t.Fatalf("member edited the goal: %d", r.status)
	}

	gone := owner.post(fmt.Sprintf("/entries/%d/delete", entries[0].ID), nil)
	if gone.status != http.StatusOK || len(ts.entries(t, tr.ID)) != 0 || !strings.Contains(gone.body, "0 / 32 points") || !strings.Contains(gone.body, "0 goals completed") {
		t.Fatal("deleting an entry did not recalculate")
	}
}

func TestCumulativeOrderAndHistoryWindow(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	loc := location("America/Los_Angeles")
	tr := owner.createTracker(h, "Test points", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	path := fmt.Sprintf("/trackers/%d/entries", tr.ID)
	now := ts.clock.now().In(loc)
	stamp := now.Format(timeInputLayout)
	owner.post(path, url.Values{"number": {"10"}, "time": {stamp}})
	owner.post(path, url.Values{"number": {"20"}, "time": {stamp}})
	same := owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body
	total30 := strings.Index(same, `entry-running">30 points total`)
	total10 := strings.Index(same, `entry-running">10 points total`)
	if total30 < 0 || total10 < 0 || total30 > total10 {
		t.Fatal("same-time entries were not ordered by id")
	}

	yesterday := now.AddDate(0, 0, -1).Format(timeInputLayout)
	owner.post(path, url.Values{"number": {"5"}, "time": {yesterday}})
	back := owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body
	if !strings.Contains(back, `entry-running">5 points total`) || !strings.Contains(back, `entry-running">15 points total`) || !strings.Contains(back, `entry-running">35 points total`) {
		t.Fatal("a backdated entry did not take its place in the running total")
	}
	if !strings.Contains(back, "3 / 32 points") || !strings.Contains(back, "35 points total earned") {
		t.Fatal("lifetime progress ignored the backdated amount")
	}

	older := now.AddDate(0, 0, -40).Format(timeInputLayout)
	wide := owner.createTracker(h, "Long total", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", wide.ID), url.Values{"number": {"100"}, "time": {older}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", wide.ID), url.Values{"number": {"10"}})
	page := owner.get(fmt.Sprintf("/trackers/%d", wide.ID))
	if !strings.Contains(page.body, "14 / 32 points") || !strings.Contains(page.body, "110 points total earned") || !strings.Contains(page.body, "3 goals completed") {
		t.Fatal("the history window replaced the lifetime total")
	}
	if !strings.Contains(page.body, "In this history: 10 points") || !strings.Contains(page.body, "The total above includes every entry.") {
		t.Fatal("the filtered subtotal was missing or unlabeled")
	}
	chart := valueChartFromPage(t, page.body)
	if len(chart.Points) != 1 || chart.Points[0].Y != 110 || !strings.Contains(chart.Points[0].Text, "110 total") {
		t.Fatalf("chart dropped earlier points: %+v", chart.Points)
	}
	charts := owner.get(fmt.Sprintf("/charts?tracker=%d&range=30d", wide.ID))
	if !strings.Contains(charts.body, "14 / 32 points") || !strings.Contains(charts.body, "110 points total earned") || !strings.Contains(charts.body, "including anything earlier") {
		t.Fatal("the charts range replaced the lifetime total")
	}
	long := valueChartFromPage(t, charts.body)
	if len(long.Points) != 1 || long.Points[0].Y != 110 {
		t.Fatalf("charts page %+v", long.Points)
	}
}

func TestNumberTrackerStaysIndependent(t *testing.T) {
	_, owner, _, h := sharedHouse(t)
	club := owner.createTracker(h, "Weight", url.Values{"kind": {kindNumber}, "unit": {"lb"}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", club.ID), url.Values{"number": {"10"}})
	page := owner.post(fmt.Sprintf("/trackers/%d/entries", club.ID), url.Values{"number": {"12"}})
	if !strings.Contains(page.body, "Last value: 12 lb") || !strings.Contains(page.body, ">Log again</button>") || strings.Contains(page.body, "goals completed") || strings.Contains(page.body, "22 lb") {
		t.Fatal("a number tracker was treated as a running total")
	}
	if r := owner.post("/trackers", url.Values{
		"household": {fmt.Sprint(h.ID)}, "name": {"No goal"}, "kind": {kindNumber}, "unit": {"lb"}, "goal": {"32"},
	}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("number tracker accepted a goal: %d", r.status)
	}
}

func TestGoalColumnMigrationKeepsExistingTrackers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&trackerBeforeGoal{}); err != nil {
		t.Fatal(err)
	}
	row := trackerBeforeGoal{
		HouseholdID: 1, Name: "Weight", Kind: kindNumber, Unit: "lb", SummaryDisplay: summaryTimes,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	var tr Tracker
	if err := opened.Where("name = ?", "Weight").Take(&tr).Error; err != nil {
		t.Fatal(err)
	}
	if tr.Kind != kindNumber || tr.Unit != "lb" || tr.Goal != nil || tr.Name != "Weight" {
		t.Fatalf("migrated %+v", tr)
	}
}

// trackerBeforeGoal is a tracker row from before the repeating goal existed.
type trackerBeforeGoal struct {
	ID             uint
	HouseholdID    uint   `gorm:"not null"`
	Name           string `gorm:"not null"`
	Kind           string `gorm:"not null;default:count"`
	Unit           string
	SummaryDisplay string `gorm:"not null;default:times"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (trackerBeforeGoal) TableName() string { return "trackers" }
