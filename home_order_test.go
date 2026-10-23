package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHomeOrderCrossesHouseholds(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	personal := ts.personalHousehold(t, "owner@example.com")
	homeMeal := owner.createTracker(personal, "Home meal", nil)
	homeWalk := owner.createTracker(personal, "Home walk", nil)
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	familyMeal := owner.createTracker(family, "Family meal", nil)

	home := owner.get("/").body
	if !strings.Contains(home, `data-reorder-url="/trackers/order"`) {
		t.Fatalf("home list is not one reorder list:\n%s", home)
	}
	if strings.Count(home, "data-reorder-list") != 1 {
		t.Fatal("home list is split into several reorder lists")
	}

	ids := idValues(homeMeal.ID, familyMeal.ID, homeWalk.ID)
	if r := owner.post("/trackers/order", ids); r.status != http.StatusNoContent {
		t.Fatalf("home reorder: %d\n%s", r.status, r.body)
	}
	if r := owner.post("/trackers/order", ids); r.status != http.StatusNoContent {
		t.Fatalf("repeat home reorder: %d", r.status)
	}
	assertOrder(t, owner.get("/").body, "Home meal", "Family meal", "Home walk")

	if ts.tracker(t, familyMeal.ID).HouseholdID != family.ID || ts.tracker(t, homeWalk.ID).HouseholdID != personal.ID {
		t.Fatal("home reorder moved a tracker to another household")
	}
	if ts.tracker(t, homeMeal.ID).Position != 1 || ts.tracker(t, homeWalk.ID).Position != 2 {
		t.Fatal("home reorder changed the household's shared order")
	}
	assertOrder(t, owner.get(fmt.Sprintf("/households/%d", personal.ID)).body, "Home meal", "Home walk")
}

func TestHomeOrderIsPersonal(t *testing.T) {
	ts, owner, member, shared := sharedHouse(t)
	alpha := owner.createTracker(shared, "Alpha", nil)
	beta := owner.createTracker(shared, "Beta", nil)
	personal := ts.personalHousehold(t, "member@example.com")
	mine := member.createTracker(personal, "Mine", nil)

	home := member.get("/").body
	if strings.Count(home, "Drag to reorder this tracker") != 3 {
		t.Fatalf("a member's home list is missing handles:\n%s", home)
	}

	if r := member.post("/trackers/order", idValues(beta.ID, mine.ID, alpha.ID)); r.status != http.StatusNoContent {
		t.Fatalf("member home reorder: %d\n%s", r.status, r.body)
	}
	assertOrder(t, member.get("/").body, "Beta", "Mine", "Alpha")
	if ts.tracker(t, alpha.ID).Position != 1 || ts.tracker(t, beta.ID).Position != 2 {
		t.Fatal("a member's home order changed the shared household order")
	}
	assertOrder(t, owner.get("/").body, "Alpha", "Beta")
	assertOrder(t, owner.get(fmt.Sprintf("/households/%d", shared.ID)).body, "Alpha", "Beta")
}

func TestHomeOrderRejectsWrongSet(t *testing.T) {
	ts, owner, member, shared := sharedHouse(t)
	alpha := owner.createTracker(shared, "Alpha", nil)
	beta := owner.createTracker(shared, "Beta", nil)
	outsider := ts.browser(t)
	outsider.signup("outsider@example.com", "")
	hidden := outsider.createTracker(ts.personalHousehold(t, "outsider@example.com"), "Hidden", nil)

	if r := member.post("/trackers/order", idValues(beta.ID, alpha.ID)); r.status != http.StatusNoContent {
		t.Fatalf("home reorder: %d", r.status)
	}
	for _, tc := range []struct {
		form url.Values
		want int
	}{
		{idValues(alpha.ID), http.StatusConflict},
		{idValues(alpha.ID, beta.ID, hidden.ID), http.StatusConflict},
		{idValues(alpha.ID, hidden.ID), http.StatusConflict},
		{idValues(alpha.ID, beta.ID, beta.ID), http.StatusUnprocessableEntity},
		{url.Values{"id": {"nope"}}, http.StatusUnprocessableEntity},
		{url.Values{}, http.StatusUnprocessableEntity},
	} {
		if r := member.post("/trackers/order", tc.form); r.status != tc.want {
			t.Fatalf("form %v: got %d, want %d", tc.form, r.status, tc.want)
		}
	}
	assertOrder(t, member.get("/").body, "Beta", "Alpha")
	if strings.Contains(member.get("/").body, "Hidden") {
		t.Fatal("a rejected reorder exposed another person's tracker")
	}

	guest := ts.browser(t)
	if r := guest.post("/trackers/order", idValues(alpha.ID, beta.ID)); r.url.Path != "/login" {
		t.Fatalf("logged out ended at %s", r.url.Path)
	}
}

func TestHomeOrderAppendsAndSkipsArchived(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	personal := ts.personalHousehold(t, "owner@example.com")
	alpha := owner.createTracker(personal, "Alpha", nil)
	beta := owner.createTracker(personal, "Beta", nil)
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	gamma := owner.createTracker(family, "Gamma", nil)

	if r := owner.post("/trackers/order", idValues(gamma.ID, beta.ID, alpha.ID)); r.status != http.StatusNoContent {
		t.Fatalf("home reorder: %d", r.status)
	}
	owner.createTracker(personal, "Delta", nil)
	assertOrder(t, owner.get("/").body, "Gamma", "Beta", "Alpha", "Delta")

	owner.post(fmt.Sprintf("/trackers/%d/archive", beta.ID), nil)
	if r := owner.post("/trackers/order", idValues(gamma.ID, beta.ID, alpha.ID)); r.status != http.StatusConflict {
		t.Fatalf("reorder with an archived tracker: %d", r.status)
	}
	assertOrder(t, owner.get("/").body, "Gamma", "Alpha", "Delta")
	owner.post(fmt.Sprintf("/trackers/%d/restore", beta.ID), nil)
	assertOrder(t, owner.get("/").body, "Gamma", "Beta", "Alpha", "Delta")
}
