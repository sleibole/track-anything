package main

import (
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestRewardMath(t *testing.T) {
	goal := 32.0
	total := sumRats([]float64{119})
	g := ratFromFloat(goal)
	earned := goalsCompleted(total, g)
	if earned.String() != "3" || formatRat(goalProgress(total, g, earned)) != "23" {
		t.Fatalf("119 / 32: earned %s progress %s", earned, formatRat(goalProgress(total, g, earned)))
	}
	if got := rewardsAvailable(earned, 2); got.String() != "1" {
		t.Fatalf("available %s", got)
	}
	if got := rewardsAvailable(earned, 0); got.String() != "3" {
		t.Fatalf("none redeemed: %s", got)
	}
	if got := rewardsAvailable(earned, 5); got.Sign() != 0 {
		t.Fatalf("over-redeemed available %s", got)
	}
	if rewardsLine(rewardsAvailable(earned, 2), 2) != "1 reward available · 2 redeemed." {
		t.Fatalf("line %q", rewardsLine(rewardsAvailable(earned, 2), 2))
	}
	if rewardsLine(rewardsAvailable(big.NewInt(1), 0), 0) != "1 reward available · 0 redeemed." {
		t.Fatal(rewardsLine(big.NewInt(1), 0))
	}
	if rewardGapText(2, big.NewInt(1), true) != "2 rewards redeemed; current points qualify for 1." {
		t.Fatal(rewardGapText(2, big.NewInt(1), true))
	}
	if rewardGapText(2, big.NewInt(2), true) != "" || rewardGapText(0, big.NewInt(0), true) != "" {
		t.Fatal("gap shown when redeemed does not exceed earned")
	}
	if rewardGapText(1, big.NewInt(0), false) != "1 reward redeemed. New redemptions stay off until a goal is set." {
		t.Fatal(rewardGapText(1, big.NewInt(0), false))
	}

	tenth := 0.1
	tenths := earnedGoals([]float64{0.1, 0.1, 0.1}, &tenth)
	if tenths.String() != "3" || rewardsAvailable(tenths, 1).String() != "2" {
		t.Fatalf("decimal rewards: earned %s", tenths)
	}
}

func TestRedeemShowsAvailabilityWithoutChangingPoints(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Reading", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	path := fmt.Sprintf("/trackers/%d", tr.ID)

	start := owner.get(path)
	if strings.Contains(start.body, "Redeem reward") || !strings.Contains(start.body, "0 rewards available · 0 redeemed.") {
		t.Fatal("a new goal tracker offered a redemption or hid the zero counts")
	}
	forged := owner.post(path+"/redeem", url.Values{"request": {"0123456789abcdef0123456789abcdef"}})
	if forged.status != http.StatusUnprocessableEntity || !strings.Contains(forged.body, "No reward is available.") || len(ts.redemptions(t, tr.ID)) != 0 {
		t.Fatalf("redeem with nothing earned: %d redemptions %d", forged.status, len(ts.redemptions(t, tr.ID)))
	}

	before := owner.post(path+"/entries", url.Values{"number": {"119"}})
	if !strings.Contains(before.body, "23 / 32 points toward next goal") || !strings.Contains(before.body, "3 goals completed · 119 points total earned") || !strings.Contains(before.body, "3 rewards available · 0 redeemed.") {
		t.Fatal("119 points did not show three available rewards")
	}
	if !strings.Contains(before.body, ">Redeem reward</button>") || !strings.Contains(before.body, "Add a note") {
		t.Fatal("redeem action or optional note was missing")
	}
	points := append([]float64(nil), entryTotals(t, ts, tr.ID)...)

	firstKey := redeemKey(t, before.body)
	first := owner.post(path+"/redeem", url.Values{"request": {firstKey}, "note": {"<script>The Hobbit</script>"}})
	if first.status != http.StatusOK || !strings.Contains(first.body, "Reward redeemed.") || !strings.Contains(first.url.RawQuery, "notice=reward") {
		t.Fatalf("first redeem: %d %s", first.status, first.url)
	}
	if !strings.Contains(first.body, "23 / 32 points toward next goal") || !strings.Contains(first.body, "3 goals completed · 119 points total earned") || !strings.Contains(first.body, "2 rewards available · 1 redeemed.") {
		t.Fatal("redeeming changed points, goals, or progress")
	}
	if strings.Contains(first.body, "<script>The Hobbit</script>") || !strings.Contains(first.body, "&lt;script&gt;The Hobbit&lt;/script&gt;") {
		t.Fatal("the note was not escaped")
	}
	if !strings.Contains(first.body, "by owner@example.com") || !strings.Contains(first.body, `hx-confirm="Undo this redemption?"`) {
		t.Fatal("redemption history omitted the member or the undo confirmation")
	}
	if sameTotals(points, entryTotals(t, ts, tr.ID)) == false || len(ts.redemptions(t, tr.ID)) != 1 {
		t.Fatal("points changed or the redemption was not stored")
	}
	again := owner.get(path)
	if strings.Contains(again.body, "Reward redeemed.") {
		t.Fatal("opening the tracker repeated the redeem notice")
	}

	second := owner.post(path+"/redeem", url.Values{"request": {redeemKey(t, again.body)}, "note": {"The Two Towers"}})
	if !strings.Contains(second.body, "1 reward available · 2 redeemed.") || !strings.Contains(second.body, "23 / 32 points toward next goal") || !strings.Contains(second.body, "3 goals completed · 119 points total earned") {
		t.Fatal("two redemptions did not leave one reward and the same progress")
	}

	memberRedeem := member.post(path+"/redeem", url.Values{"request": {redeemKey(t, member.get(path).body)}})
	if memberRedeem.status != http.StatusOK || !strings.Contains(memberRedeem.body, "0 rewards available · 3 redeemed.") || !strings.Contains(memberRedeem.body, "by member@example.com") {
		t.Fatalf("member redeem: %d", memberRedeem.status)
	}
	if strings.Contains(member.get(path).body, ">Redeem reward</button>") {
		t.Fatal("a spent tracker still offered redeem")
	}

	undoPath := redemptionUndoPath(t, owner.get(path).body)
	undo := owner.post(undoPath, nil)
	if undo.status != http.StatusOK || !strings.Contains(undo.body, "Redemption undone.") || !strings.Contains(undo.body, "1 reward available · 2 redeemed.") {
		t.Fatal("undo did not restore one reward")
	}
	if sameTotals(points, entryTotals(t, ts, tr.ID)) == false {
		t.Fatal("undo changed points")
	}
	repeat := owner.post(undoPath, nil)
	if repeat.status != http.StatusOK || strings.Contains(repeat.url.RawQuery, "result=undone") || strings.Contains(repeat.body, "Redemption undone.") || len(ts.redemptions(t, tr.ID)) != 2 {
		t.Fatalf("repeat undo: %d %s redemptions %d", repeat.status, repeat.url, len(ts.redemptions(t, tr.ID)))
	}

	rows := csvRows(t, owner, tr.ID)
	var redemptions int
	for _, row := range rows[1:] {
		if row[0] == "redemption" {
			redemptions++
			if row[8] == "" || row[9] != "false" {
				t.Fatalf("redemption csv %+v", row)
			}
		}
	}
	if redemptions != 2 || rows[1][0] != "entry" || rows[1][3] != "119" {
		t.Fatalf("export %+v", rows)
	}
}

func TestRedeemPermissionsAndOtherTrackers(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Reading", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", tr.ID), url.Values{"number": {"32"}})
	key := redeemKey(t, owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body)

	outsider := ts.browser(t)
	outsider.signup("outsider@example.com", "")
	if r := outsider.post(fmt.Sprintf("/trackers/%d/redeem", tr.ID), url.Values{"request": {key}}); r.status != http.StatusNotFound || len(ts.redemptions(t, tr.ID)) != 0 {
		t.Fatalf("outsider redeem: %d", r.status)
	}
	guest := ts.browser(t)
	if r := guest.post(fmt.Sprintf("/trackers/%d/redeem", tr.ID), url.Values{"request": {key}}); r.url.Path != "/login" || len(ts.redemptions(t, tr.ID)) != 0 {
		t.Fatalf("logged-out redeem landed on %s", r.url.Path)
	}

	plain := owner.createTracker(h, "No goal", url.Values{"kind": {kindCumulative}, "unit": {"points"}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", plain.ID), url.Values{"number": {"40"}})
	if r := owner.post(fmt.Sprintf("/trackers/%d/redeem", plain.ID), url.Values{"request": {key}}); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "with a goal") || len(ts.redemptions(t, plain.ID)) != 0 {
		t.Fatalf("no goal: %d", r.status)
	}
	count := owner.createTracker(h, "Meals", nil)
	if r := owner.post(fmt.Sprintf("/trackers/%d/redeem", count.ID), url.Values{"request": {key}}); r.status != http.StatusUnprocessableEntity || len(ts.redemptions(t, count.ID)) != 0 {
		t.Fatalf("count tracker: %d", r.status)
	}
	if strings.Contains(owner.get(fmt.Sprintf("/trackers/%d", count.ID)).body, "Redeem reward") || strings.Contains(owner.get(fmt.Sprintf("/trackers/%d", count.ID)).body, "rewards available") {
		t.Fatal("a count tracker showed rewards")
	}

	owner.post(fmt.Sprintf("/trackers/%d/share", tr.ID), nil)
	token := *ts.tracker(t, tr.ID).ShareToken
	shared := ts.browser(t).get("/s/" + token)
	if strings.Contains(shared.body, "Redeem reward") || strings.Contains(shared.body, "Undo redemption") || !strings.Contains(shared.body, "1 reward available · 0 redeemed.") {
		t.Fatal("share link could redeem, or lost the reward counts")
	}

	done := member.post(fmt.Sprintf("/trackers/%d/redeem", tr.ID), url.Values{"request": {key}})
	if done.status != http.StatusOK || len(ts.redemptions(t, tr.ID)) != 1 {
		t.Fatalf("member redeem: %d", done.status)
	}
	undoPath := redemptionUndoPath(t, member.get(fmt.Sprintf("/trackers/%d", tr.ID)).body)
	if r := outsider.post(undoPath, nil); r.status != http.StatusNotFound || len(ts.redemptions(t, tr.ID)) != 1 {
		t.Fatalf("outsider undo: %d", r.status)
	}
	if r := guest.post(undoPath, nil); r.url.Path != "/login" || len(ts.redemptions(t, tr.ID)) != 1 {
		t.Fatalf("logged-out undo landed on %s", r.url.Path)
	}
	if strings.Contains(ts.browser(t).get("/s/"+token).body, "Undo redemption") {
		t.Fatal("share page offered undo")
	}
}

func TestRedeemConcurrencyAndDuplicateSubmit(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Reading", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	path := fmt.Sprintf("/trackers/%d/redeem", tr.ID)
	owner.post(fmt.Sprintf("/trackers/%d/entries", tr.ID), url.Values{"number": {"32"}})
	points := entryTotals(t, ts, tr.ID)

	ownerKey := redeemKey(t, owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body)
	memberKey := redeemKey(t, member.get(fmt.Sprintf("/trackers/%d", tr.ID)).body)
	if ownerKey == memberKey {
		t.Fatal("both forms shared one request key")
	}
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	bodies := make([]string, 2)
	browsers := []*browser{owner, member}
	keys := []string{ownerKey, memberKey}
	for i := range browsers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := browsers[i].post(path, url.Values{"request": {keys[i]}})
			statuses[i] = r.status
			bodies[i] = r.body
		}(i)
	}
	wg.Wait()
	ok, rejected := 0, 0
	for i, status := range statuses {
		switch {
		case status == http.StatusOK && strings.Contains(bodies[i], "Reward redeemed."):
			ok++
		case status == http.StatusUnprocessableEntity && strings.Contains(bodies[i], "No reward is available."):
			rejected++
		default:
			t.Errorf("racer %d: %d\n%s", i, status, bodies[i])
		}
	}
	if ok != 1 || rejected != 1 || len(ts.redemptions(t, tr.ID)) != 1 || !sameTotals(points, entryTotals(t, ts, tr.ID)) {
		t.Fatalf("race ok %d rejected %d rows %d", ok, rejected, len(ts.redemptions(t, tr.ID)))
	}

	wide := owner.createTracker(h, "Several", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", wide.ID), url.Values{"number": {"96"}})
	page := owner.get(fmt.Sprintf("/trackers/%d", wide.ID))
	dupKey := redeemKey(t, page.body)
	first := owner.post(fmt.Sprintf("/trackers/%d/redeem", wide.ID), url.Values{"request": {dupKey}, "note": {"once"}})
	second := owner.post(fmt.Sprintf("/trackers/%d/redeem", wide.ID), url.Values{"request": {dupKey}, "note": {"twice"}})
	if first.status != http.StatusOK || second.status != http.StatusOK || len(ts.redemptions(t, wide.ID)) != 1 || ts.redemptions(t, wide.ID)[0].Note != "once" {
		t.Fatalf("duplicate submit created %d rows", len(ts.redemptions(t, wide.ID)))
	}
	undo := owner.post(redemptionUndoPath(t, first.body), nil)
	if len(ts.redemptions(t, wide.ID)) != 0 {
		t.Fatal("undo left the row")
	}
	replay := owner.post(fmt.Sprintf("/trackers/%d/redeem", wide.ID), url.Values{"request": {dupKey}, "note": {"again"}})
	if replay.status != http.StatusOK || strings.Contains(replay.body, "Reward redeemed.") || len(ts.redemptions(t, wide.ID)) != 0 {
		t.Fatalf("replay after undo created a redemption: %d rows %d", replay.status, len(ts.redemptions(t, wide.ID)))
	}
	_ = undo
}

func TestRewardHistorySurvivesPointAndGoalChanges(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "Reading", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	path := fmt.Sprintf("/trackers/%d", tr.ID)
	owner.post(path+"/entries", url.Values{"number": {"119"}})
	for range 3 {
		page := owner.get(path)
		owner.post(path+"/redeem", url.Values{"request": {redeemKey(t, page.body)}, "note": {"book"}})
	}
	if len(ts.redemptions(t, tr.ID)) != 3 {
		t.Fatal(len(ts.redemptions(t, tr.ID)))
	}
	entry := ts.entries(t, tr.ID)[0]
	when := entry.OccurredAt.In(location("America/Los_Angeles")).Format(timeInputLayout)
	edited := owner.post(fmt.Sprintf("/entries/%d", entry.ID), url.Values{"time": {when}, "number": {"40"}, "note": {"corrected"}})
	if !strings.Contains(edited.body, "8 / 32 points toward next goal") || !strings.Contains(edited.body, "1 goal completed · 40 points total earned") || !strings.Contains(edited.body, "0 rewards available · 3 redeemed.") {
		t.Fatal("lowering points did not keep the redeemed count at zero available")
	}
	if !strings.Contains(edited.body, "3 rewards redeemed; current points qualify for 1.") || len(ts.redemptions(t, tr.ID)) != 3 || *ts.entries(t, tr.ID)[0].Number != 40 {
		t.Fatal("the shortfall deleted redemptions or changed the saved amount")
	}

	caught := owner.post(path+"/entries", url.Values{"number": {"56"}})
	if !strings.Contains(caught.body, "0 rewards available · 3 redeemed.") || strings.Contains(caught.body, "qualify for") || !strings.Contains(caught.body, "3 goals completed · 96 points total earned") {
		t.Fatal("points that only closed the difference made a reward available")
	}
	opened := owner.post(path+"/entries", url.Values{"number": {"32"}})
	if !strings.Contains(opened.body, "1 reward available · 3 redeemed.") || !strings.Contains(opened.body, "4 goals completed · 128 points total earned") || !strings.Contains(opened.body, ">Redeem reward</button>") {
		t.Fatal("the next goal after the difference did not become available")
	}

	undo := owner.post(redemptionUndoPath(t, opened.body), nil)
	if !strings.Contains(undo.body, "2 rewards available · 2 redeemed.") || len(ts.redemptions(t, tr.ID)) != 2 {
		t.Fatal("undo did not shrink the redeemed count")
	}

	retarget := owner.post(path, url.Values{"name": {"Reading"}, "kind": {kindCumulative}, "unit": {"points"}, "goal": {"100"}})
	if !strings.Contains(retarget.body, "0 rewards available · 2 redeemed.") || !strings.Contains(retarget.body, "2 rewards redeemed; current points qualify for 1.") || len(ts.redemptions(t, tr.ID)) != 2 {
		t.Fatal("a larger goal dropped redemption history")
	}
	cleared := owner.post(path, url.Values{"name": {"Reading"}, "kind": {kindCumulative}, "unit": {"points"}, "goal": {""}})
	if ts.tracker(t, tr.ID).Goal != nil || strings.Contains(cleared.body, ">Redeem reward</button>") || strings.Contains(cleared.body, "goal-progress") || !strings.Contains(cleared.body, "128 points") {
		t.Fatal("removing the goal still allowed a redemption or hid the total")
	}
	if !strings.Contains(cleared.body, "book") || !strings.Contains(cleared.body, "New redemptions stay off until a goal is set.") || len(ts.redemptions(t, tr.ID)) != 2 {
		t.Fatal("removing the goal deleted redemption history")
	}
	restored := owner.post(path, url.Values{"name": {"Reading"}, "kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	if !strings.Contains(restored.body, "2 rewards available · 2 redeemed.") || !strings.Contains(restored.body, "4 goals completed · 128 points total earned") || len(ts.redemptions(t, tr.ID)) != 2 {
		t.Fatal("restoring the goal did not count the kept redemptions")
	}
	if !strings.Contains(owner.get(path+"/edit").body, "Redeemed rewards are kept") || !strings.Contains(owner.get(path+"/edit").body, "extra amount counts toward the next one") {
		t.Fatal("goal help did not explain redemption history")
	}
}

func TestRewardTotalsIgnoreHistoryFilters(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	loc := location("America/Los_Angeles")
	tr := owner.createTracker(h, "Reading", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	path := fmt.Sprintf("/trackers/%d", tr.ID)
	older := ts.clock.now().In(loc).AddDate(0, 0, -40).Format(timeInputLayout)
	owner.post(path+"/entries", url.Values{"number": {"100"}, "time": {older}})
	owner.post(path+"/entries", url.Values{"number": {"19"}})
	owner.post(path+"/redeem", url.Values{"request": {redeemKey(t, owner.get(path).body)}, "note": {"kept"}})
	owner.post(path+"/redeem", url.Values{"request": {redeemKey(t, owner.get(path).body)}})

	page := owner.get(path)
	for _, want := range []string{
		"23 / 32 points toward next goal",
		"1 reward available · 2 redeemed.",
		"3 goals completed · 119 points total earned",
		"In this history: 19 points",
		"Every redemption. The dates in History do not limit this list.",
		"kept",
	} {
		if !strings.Contains(page.body, want) {
			t.Errorf("filtered history missing %q", want)
		}
	}
	charts := owner.get(fmt.Sprintf("/charts?tracker=%d&range=30d", tr.ID))
	if !strings.Contains(charts.body, "23 / 32 points toward next goal") || !strings.Contains(charts.body, "1 reward available · 2 redeemed.") || !strings.Contains(charts.body, "119 points total earned") {
		t.Fatal("the 30-day chart replaced the lifetime reward totals")
	}
	longer := owner.get(fmt.Sprintf("/charts?tracker=%d&range=90d", tr.ID))
	if !strings.Contains(longer.body, "1 reward available · 2 redeemed.") || !strings.Contains(longer.body, "119 points total earned") {
		t.Fatal("a longer range changed the lifetime reward totals")
	}
}

func TestRedeemRefreshesInPlaceWithoutRepeatingTheNotice(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "Reading", url.Values{"kind": {kindCumulative}, "unit": {"points"}, "goal": {"32"}})
	pagePath := fmt.Sprintf("/trackers/%d", tr.ID)
	owner.post(pagePath+"/entries", url.Values{"number": {"32"}})
	key := redeemKey(t, owner.get(pagePath).body)

	req, err := http.NewRequest(http.MethodPost, ts.srv.URL+pagePath+"/redeem", strings.NewReader(url.Values{"request": {key}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", ts.srv.URL+pagePath)
	logged := refreshDo(t, owner, req)
	if logged.status != http.StatusOK || logged.header.Get("HX-Reswap") != "none" || !strings.Contains(logged.body, "Reward redeemed.") || !strings.Contains(logged.body, `hx-swap-oob="outerHTML"`) {
		t.Fatalf("in-place redeem: %d %q\n%s", logged.status, logged.header.Get("HX-Reswap"), logged.body)
	}
	if strings.Contains(logged.body, "<title>") {
		t.Fatal("in-place redeem replaced the document")
	}

	ids, revs := pageViewRevs(t, owner.get(pagePath).body)
	refresh := refreshGet(t, owner, pagePath+"?"+url.Values{"present": {ids}, "rev": {revs}}.Encode())
	if refresh.status != http.StatusNoContent || strings.Contains(refresh.body, "Reward redeemed.") {
		t.Fatalf("refresh repeated the notice: %d %s", refresh.status, refresh.body)
	}
}

func TestRedemptionMigrationStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&User{}, &Household{}, &HouseholdMember{}, &Tracker{}, &Entry{}, &RecordedZero{}, &TrackerOrder{}); err != nil {
		t.Fatal(err)
	}
	row := Tracker{HouseholdID: 1, Name: "Reading", Kind: kindCumulative, Unit: "points", SummaryDisplay: summaryTimes}
	goal := 32.0
	row.Goal = &goal
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
	var n int64
	if err := opened.Model(&Redemption{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("migration created %d redemptions", n)
	}
	var requests int64
	if err := opened.Model(&RewardRequest{}).Count(&requests).Error; err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatalf("migration created %d request keys", requests)
	}
	var tr Tracker
	if err := opened.Where("name = ?", "Reading").Take(&tr).Error; err != nil {
		t.Fatal(err)
	}
	if tr.Kind != kindCumulative || tr.Goal == nil || *tr.Goal != 32 {
		t.Fatalf("migrated %+v", tr)
	}
}

func (ts *testServer) redemptions(t *testing.T, trackerID uint) []Redemption {
	t.Helper()
	var rows []Redemption
	if err := ts.app.db.Where("tracker_id = ?", trackerID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func entryTotals(t *testing.T, ts *testServer, trackerID uint) []float64 {
	t.Helper()
	var totals []float64
	for _, e := range ts.entries(t, trackerID) {
		if e.Number != nil {
			totals = append(totals, *e.Number)
		}
	}
	return totals
}

func sameTotals(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func redeemKey(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`name="request" value="([0-9a-f]{32})"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("redeem form had no request key:\n%s", body)
	}
	return m[1]
}

func redemptionUndoPath(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`/trackers/\d+/redemptions/\d+/undo`).FindString(body)
	if m == "" {
		t.Fatalf("no undo path:\n%s", body)
	}
	return m
}
