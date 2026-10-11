package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func idValues(ids ...uint) url.Values {
	v := url.Values{}
	for _, id := range ids {
		v.Add("id", strconv.FormatUint(uint64(id), 10))
	}
	return v
}

func assertOrder(t *testing.T, body string, parts ...string) {
	t.Helper()
	prev := -1
	for _, part := range parts {
		i := strings.Index(body, part)
		if i < 0 || i < prev {
			t.Fatalf("%q not in order (index %d, previous %d)", part, i, prev)
		}
		prev = i
	}
}

func householdHref(id uint) string {
	return fmt.Sprintf(`href="/households/%d"`, id)
}

func (ts *testServer) membership(t *testing.T, h Household, email string) HouseholdMember {
	t.Helper()
	var m HouseholdMember
	if err := ts.app.db.Take(&m, "household_id = ? AND user_id = ?", h.ID, ts.user(t, email).ID).Error; err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReorderTrackersPersists(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	alpha := owner.createTracker(h, "Alpha", nil)
	beta := owner.createTracker(h, "Beta", nil)
	gamma := owner.createTracker(h, "Gamma", nil)

	home := owner.get("/").body
	assertOrder(t, home, "Alpha", "Beta", "Gamma")
	if !strings.Contains(home, "/static/reorder.js") || !strings.Contains(home, "Drag to reorder this tracker") {
		t.Fatal("dashboard missing reorder controls")
	}
	if !strings.Contains(home, fmt.Sprintf(`action="/trackers/%d/quick"`, alpha.ID)) {
		t.Fatal("log button missing")
	}

	order := fmt.Sprintf("/households/%d/order", h.ID)
	ids := idValues(gamma.ID, alpha.ID, beta.ID)
	if r := owner.post(order, ids); r.status != http.StatusNoContent {
		t.Fatalf("reorder: %d\n%s", r.status, r.body)
	}
	if r := owner.post(order, ids); r.status != http.StatusNoContent {
		t.Fatalf("repeat reorder: %d", r.status)
	}
	if ts.tracker(t, gamma.ID).Position != 1 || ts.tracker(t, alpha.ID).Position != 2 || ts.tracker(t, beta.ID).Position != 3 {
		t.Fatalf("positions gamma=%d alpha=%d beta=%d", ts.tracker(t, gamma.ID).Position, ts.tracker(t, alpha.ID).Position, ts.tracker(t, beta.ID).Position)
	}
	assertOrder(t, owner.get("/").body, "Gamma", "Alpha", "Beta")
	assertOrder(t, owner.get(fmt.Sprintf("/households/%d", h.ID)).body, "Gamma", "Alpha", "Beta")

	if r := owner.post(fmt.Sprintf("/trackers/%d/quick", alpha.ID), url.Values{"back": {"/"}}); r.status != http.StatusOK || len(ts.entries(t, alpha.ID)) != 1 {
		t.Fatalf("log after reorder: %d, entries %d", r.status, len(ts.entries(t, alpha.ID)))
	}
	assertOrder(t, owner.get("/").body, "Gamma", "Alpha", "Beta")

	// A missing tracker conflicts with the saved list. A duplicate or a non-numeric id is rejected first.
	before := []int{ts.tracker(t, gamma.ID).Position, ts.tracker(t, alpha.ID).Position, ts.tracker(t, beta.ID).Position}
	for _, tc := range []struct {
		form url.Values
		want int
	}{
		{idValues(gamma.ID, alpha.ID), http.StatusConflict},
		{idValues(gamma.ID, alpha.ID, beta.ID, beta.ID), http.StatusUnprocessableEntity},
		{url.Values{"id": {"nope"}}, http.StatusUnprocessableEntity},
		{url.Values{}, http.StatusUnprocessableEntity},
	} {
		if r := owner.post(order, tc.form); r.status != tc.want {
			t.Fatalf("form %v: got %d, want %d", tc.form, r.status, tc.want)
		}
	}
	if ts.tracker(t, gamma.ID).Position != before[0] || ts.tracker(t, alpha.ID).Position != before[1] || ts.tracker(t, beta.ID).Position != before[2] {
		t.Fatal("a rejected reorder changed positions")
	}
}

func TestReorderDoesNotMoveTrackersBetweenHouseholds(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	personal := ts.personalHousehold(t, "owner@example.com")
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	homeTracker := owner.createTracker(personal, "Home meal", nil)
	familyTracker := owner.createTracker(family, "Family meal", nil)

	beforeHome := ts.tracker(t, homeTracker.ID).Position
	beforeFamily := ts.tracker(t, familyTracker.ID).Position
	r := owner.post(fmt.Sprintf("/households/%d/order", personal.ID), idValues(homeTracker.ID, familyTracker.ID))
	if r.status != http.StatusConflict {
		t.Fatalf("mixed households: %d", r.status)
	}
	if ts.tracker(t, homeTracker.ID).HouseholdID != personal.ID || ts.tracker(t, familyTracker.ID).HouseholdID != family.ID {
		t.Fatal("reorder moved a tracker")
	}
	if ts.tracker(t, homeTracker.ID).Position != beforeHome || ts.tracker(t, familyTracker.ID).Position != beforeFamily {
		t.Fatal("rejected reorder changed positions")
	}
	assertOrder(t, owner.get("/").body, "Home meal", "Family meal")
}

func TestReorderTrackerPermissions(t *testing.T) {
	ts, owner, member, shared := sharedHouse(t)
	alpha := owner.createTracker(shared, "Alpha", nil)
	beta := owner.createTracker(shared, "Beta", nil)
	order := fmt.Sprintf("/households/%d/order", shared.ID)
	form := idValues(beta.ID, alpha.ID)

	if r := member.post(order, form); r.status != http.StatusForbidden {
		t.Fatalf("member reorder: %d", r.status)
	}
	if ts.tracker(t, alpha.ID).Position != 1 || ts.tracker(t, beta.ID).Position != 2 {
		t.Fatal("member changed tracker order")
	}
	memberPage := member.get("/").body
	if strings.Contains(memberPage, "Drag to reorder this tracker") || strings.Contains(memberPage, `/households/`+fmt.Sprint(shared.ID)+`/order`) {
		t.Fatal("member dashboard offers tracker reorder")
	}
	if !strings.Contains(memberPage, "Drag to reorder this household") {
		t.Fatal("member dashboard missing household handles")
	}

	other := ts.browser(t)
	other.signup("other@example.com", "")
	if r := other.post(order, form); r.status != http.StatusNotFound {
		t.Fatalf("outsider reorder: %d", r.status)
	}
	if ts.tracker(t, alpha.ID).Position != 1 || ts.tracker(t, beta.ID).Position != 2 {
		t.Fatal("outsider changed tracker order")
	}

	guest := ts.browser(t)
	if r := guest.post(order, form); r.url.Path != "/login" {
		t.Fatalf("logged out ended at %s", r.url.Path)
	}

	owner.post(fmt.Sprintf("/trackers/%d/archive", beta.ID), nil)
	if r := owner.post(order, idValues(alpha.ID)); r.status != http.StatusNoContent {
		t.Fatalf("reorder with an archived tracker: %d", r.status)
	}
	if ts.tracker(t, alpha.ID).Position != 1 || ts.tracker(t, beta.ID).Position != 2 {
		t.Fatalf("archived position alpha=%d beta=%d", ts.tracker(t, alpha.ID).Position, ts.tracker(t, beta.ID).Position)
	}
	owner.post(fmt.Sprintf("/trackers/%d/restore", beta.ID), nil)
	assertOrder(t, owner.get("/").body, "Alpha", "Beta")
}

func TestNewTrackerAppends(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	alpha := owner.createTracker(h, "Alpha", nil)
	beta := owner.createTracker(h, "Beta", nil)
	if r := owner.post(fmt.Sprintf("/households/%d/order", h.ID), idValues(beta.ID, alpha.ID)); r.status != http.StatusNoContent {
		t.Fatalf("reorder: %d", r.status)
	}
	gamma := owner.createTracker(h, "Gamma", nil)
	if gamma.Position <= ts.tracker(t, beta.ID).Position || gamma.Position <= ts.tracker(t, alpha.ID).Position {
		t.Fatalf("new tracker position %d", gamma.Position)
	}
	assertOrder(t, owner.get("/").body, "Beta", "Alpha", "Gamma")
}

func TestReorderHouseholdsPersists(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	personal := ts.personalHousehold(t, "owner@example.com")
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	owner.post("/households", url.Values{"name": {"Camp"}})
	camp := ts.ownedNamed(t, "owner@example.com", "Camp")

	assertOrder(t, owner.get("/").body, householdHref(personal.ID), householdHref(family.ID), householdHref(camp.ID))
	if ts.membership(t, personal, "owner@example.com").Position != 1 ||
		ts.membership(t, family, "owner@example.com").Position != 2 ||
		ts.membership(t, camp, "owner@example.com").Position != 3 {
		t.Fatal("new households were not appended")
	}

	ids := idValues(camp.ID, personal.ID, family.ID)
	if r := owner.post("/households/order", ids); r.status != http.StatusNoContent {
		t.Fatalf("reorder: %d\n%s", r.status, r.body)
	}
	if r := owner.post("/households/order", ids); r.status != http.StatusNoContent {
		t.Fatalf("repeat reorder: %d", r.status)
	}
	if ts.membership(t, camp, "owner@example.com").Position != 1 ||
		ts.membership(t, personal, "owner@example.com").Position != 2 ||
		ts.membership(t, family, "owner@example.com").Position != 3 {
		t.Fatal("saved household order is wrong")
	}
	assertOrder(t, owner.get("/").body, householdHref(camp.ID), householdHref(personal.ID), householdHref(family.ID))

	owner.post("/households", url.Values{"name": {"Extra"}})
	extra := ts.ownedNamed(t, "owner@example.com", "Extra")
	if ts.membership(t, extra, "owner@example.com").Position != 4 {
		t.Fatalf("new household position %d", ts.membership(t, extra, "owner@example.com").Position)
	}
	assertOrder(t, owner.get("/").body, householdHref(camp.ID), householdHref(personal.ID), householdHref(family.ID), householdHref(extra.ID))

	if r := owner.post("/households/order", idValues(camp.ID, personal.ID)); r.status != http.StatusConflict {
		t.Fatalf("partial list: %d", r.status)
	}
	if ts.membership(t, family, "owner@example.com").Position != 3 {
		t.Fatal("partial list changed the saved order")
	}
	guest := ts.browser(t)
	if r := guest.post("/households/order", ids); r.url.Path != "/login" {
		t.Fatalf("logged out ended at %s", r.url.Path)
	}
}

func TestHouseholdOrderIsPersonal(t *testing.T) {
	ts, owner, member, shared := sharedHouse(t)
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	personal := ts.personalHousehold(t, "member@example.com")

	memberPage := member.get("/").body
	assertOrder(t, memberPage, householdHref(personal.ID), householdHref(shared.ID))
	ownerBefore := ts.membership(t, shared, "owner@example.com").Position
	familyBefore := ts.membership(t, family, "owner@example.com").Position

	if r := member.post("/households/order", idValues(shared.ID, personal.ID)); r.status != http.StatusNoContent {
		t.Fatalf("member reorder: %d\n%s", r.status, r.body)
	}
	assertOrder(t, member.get("/").body, householdHref(shared.ID), householdHref(personal.ID))
	if ts.membership(t, shared, "owner@example.com").Position != ownerBefore || ts.membership(t, family, "owner@example.com").Position != familyBefore {
		t.Fatal("member changed the owner's household order")
	}

	// The owner's households are not this member's list, so the post changes nothing.
	if r := member.post("/households/order", idValues(family.ID, shared.ID, personal.ID)); r.status != http.StatusConflict {
		t.Fatalf("member posting someone else's household: %d", r.status)
	}
	if ts.membership(t, family, "owner@example.com").Position != familyBefore {
		t.Fatal("member changed a household they do not belong to")
	}
	if ts.membership(t, shared, "member@example.com").Position != 1 {
		t.Fatal("rejected reorder changed the member's order")
	}
}

func TestOrderBackfillPreservesCurrentOrder(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	personal := ts.personalHousehold(t, "owner@example.com")
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	userID := ts.user(t, "owner@example.com").ID

	personalMember := ts.membership(t, personal, "owner@example.com")
	db := ts.app.db
	if err := db.Model(&HouseholdMember{}).Where("household_id = ? AND user_id = ?", family.ID, userID).
		UpdateColumn("created_at", personalMember.CreatedAt.Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&HouseholdMember{}).Where("user_id = ?", userID).UpdateColumn("position", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillOrder(db); err != nil {
		t.Fatal(err)
	}
	// Family was joined in the row's created_at even though its id is newer.
	if ts.membership(t, family, "owner@example.com").Position != 1 || ts.membership(t, personal, "owner@example.com").Position != 2 {
		t.Fatalf("backfill family=%d personal=%d", ts.membership(t, family, "owner@example.com").Position, ts.membership(t, personal, "owner@example.com").Position)
	}
	if err := db.Model(&HouseholdMember{}).Where("household_id = ? AND user_id = ?", family.ID, userID).UpdateColumn("position", 5).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&HouseholdMember{}).Where("household_id = ? AND user_id = ?", personal.ID, userID).UpdateColumn("position", 4).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillOrder(db); err != nil {
		t.Fatal(err)
	}
	if ts.membership(t, family, "owner@example.com").Position != 5 || ts.membership(t, personal, "owner@example.com").Position != 4 {
		t.Fatal("backfill reset a saved household order")
	}

	alpha := owner.createTracker(personal, "Alpha", nil)
	beta := owner.createTracker(personal, "Beta", nil)
	if err := db.Model(&Tracker{}).Where("household_id = ?", personal.ID).UpdateColumn("position", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillOrder(db); err != nil {
		t.Fatal(err)
	}
	if ts.tracker(t, alpha.ID).Position != 1 || ts.tracker(t, beta.ID).Position != 2 {
		t.Fatalf("tracker backfill alpha=%d beta=%d", ts.tracker(t, alpha.ID).Position, ts.tracker(t, beta.ID).Position)
	}
	if err := db.Model(&Tracker{}).Where("id = ?", alpha.ID).UpdateColumn("position", 2).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Tracker{}).Where("id = ?", beta.ID).UpdateColumn("position", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillOrder(db); err != nil {
		t.Fatal(err)
	}
	if ts.tracker(t, alpha.ID).Position != 2 || ts.tracker(t, beta.ID).Position != 1 {
		t.Fatal("backfill reset a saved tracker order")
	}
	if err := db.Model(&Tracker{}).Where("household_id = ?", personal.ID).UpdateColumn("position", 4).Error; err != nil {
		t.Fatal(err)
	}
	if err := backfillOrder(db); err != nil {
		t.Fatal(err)
	}
	// Equal positions already display by id, so the repair keeps that order.
	if ts.tracker(t, alpha.ID).Position != 1 || ts.tracker(t, beta.ID).Position != 2 {
		t.Fatalf("duplicate repair alpha=%d beta=%d", ts.tracker(t, alpha.ID).Position, ts.tracker(t, beta.ID).Position)
	}
}
