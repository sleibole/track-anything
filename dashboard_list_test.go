package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestUnifiedTrackerList(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	personal := ts.personalHousehold(t, "owner@example.com")
	homeMeal := owner.createTracker(personal, "Home meal", nil)
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	familyMeal := owner.createTracker(family, "Family meal", nil)

	home := owner.get("/").body
	if strings.Contains(home, "household-group") || strings.Contains(home, "<h2") || strings.Contains(home, "Drag to reorder this household") {
		t.Fatalf("dashboard still groups households:\n%s", home)
	}
	assertOrder(t, home, "Home meal", "Family meal")
	if strings.Contains(home, householdHref(personal.ID)) {
		t.Fatal("a personal tracker shows its household")
	}
	if !strings.Contains(home, householdHref(family.ID)) || !strings.Contains(home, ">Family</a>") {
		t.Fatal("a shared tracker has no household label")
	}
	if strings.Contains(home, "owner@example.com") {
		t.Fatal("a uniquely named household shows the creator")
	}
	if strings.Count(home, "Drag to reorder this tracker") != 2 || !strings.Contains(home, `data-reorder-url="/trackers/order"`) {
		t.Fatal("trackers lost their reorder handles")
	}
	if !strings.Contains(home, fmt.Sprintf(`action="/trackers/%d/quick"`, homeMeal.ID)) || !strings.Contains(home, fmt.Sprintf(`action="/trackers/%d/quick"`, familyMeal.ID)) {
		t.Fatal("the list dropped a log button")
	}

	logged := owner.post(fmt.Sprintf("/trackers/%d/quick", familyMeal.ID), url.Values{"back": {"/"}})
	if logged.status != http.StatusOK || len(ts.entries(t, familyMeal.ID)) != 1 || !strings.Contains(logged.body, "1 time today") {
		t.Fatalf("logging from the list: %d, entries %d", logged.status, len(ts.entries(t, familyMeal.ID)))
	}
	if !strings.Contains(logged.body, ">Family</a>") || strings.Contains(logged.body, householdHref(personal.ID)) {
		t.Fatal("logging changed which households are labeled")
	}
}

func TestUnifiedListLabelsSharedHouseholds(t *testing.T) {
	ts, owner, member, shared := sharedHouse(t)
	owner.createTracker(shared, "Shared meal", nil)
	personal := ts.personalHousehold(t, "member@example.com")
	member.createTracker(personal, "Private meal", nil)

	home := member.get("/").body
	assertOrder(t, home, "Private meal", "Shared meal")
	if strings.Contains(home, householdHref(personal.ID)) || strings.Contains(home, "member@example.com") {
		t.Fatalf("the member's own household is labeled:\n%s", home)
	}
	want := fmt.Sprintf(`href="/households/%d">My trackers · owner@example.com</a>`, shared.ID)
	if !strings.Contains(home, want) {
		t.Fatalf("shared tracker label:\n%s", home)
	}
	if strings.Contains(home, fmt.Sprintf(`/households/%d/order`, shared.ID)) {
		t.Fatal("a member's home posts to the household's shared order")
	}
	if strings.Count(home, "Drag to reorder this tracker") != 2 || !strings.Contains(home, `data-reorder-url="/trackers/order"`) {
		t.Fatal("a member lost reorder on their home list")
	}

	owner.post(fmt.Sprintf("/households/%d", shared.ID), url.Values{"name": {"Family"}})
	home = member.get("/").body
	if strings.Contains(home, "owner@example.com") || !strings.Contains(home, fmt.Sprintf(`href="/households/%d">Family</a>`, shared.ID)) {
		t.Fatalf("a unique shared name still shows the creator:\n%s", home)
	}
}

func TestUnifiedListFollowsSavedOrder(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	personal := ts.personalHousehold(t, "owner@example.com")
	owner.createTracker(personal, "Home meal", nil)
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	first := owner.createTracker(family, "First", nil)
	second := owner.createTracker(family, "Second", nil)

	if r := owner.post(fmt.Sprintf("/households/%d/order", family.ID), idValues(second.ID, first.ID)); r.status != http.StatusNoContent {
		t.Fatalf("tracker reorder: %d", r.status)
	}
	assertOrder(t, owner.get("/").body, "Home meal", "Second", "First")

	if r := owner.post("/households/order", idValues(family.ID, personal.ID)); r.status != http.StatusNoContent {
		t.Fatalf("household reorder: %d", r.status)
	}
	assertOrder(t, owner.get("/").body, "Second", "First", "Home meal")
	if strings.Contains(owner.get("/").body, "Drag to reorder this household") {
		t.Fatal("household drag handles are back on the list")
	}
}

func TestUnifiedListEmptyState(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	home := owner.get("/").body
	if !strings.Contains(home, "No trackers yet.") || !strings.Contains(home, "Create one") {
		t.Fatalf("empty list:\n%s", home)
	}
	if strings.Contains(home, "household-group") || strings.Contains(home, "<h2") || strings.Contains(home, "No trackers here yet") {
		t.Fatalf("empty list still has household sections:\n%s", home)
	}
	if !strings.Contains(home, "New household") {
		t.Fatal("creating a household disappeared")
	}

	personal := ts.personalHousehold(t, "owner@example.com")
	owner.createTracker(personal, "Only meal", nil)
	home = owner.get("/").body
	if strings.Contains(home, "No trackers yet") {
		t.Fatal("a tracker still shows the empty state")
	}
}

func TestUnifiedListHidesArchivedTrackers(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	personal := ts.personalHousehold(t, "owner@example.com")
	kept := owner.createTracker(personal, "Kept", nil)
	gone := owner.createTracker(personal, "Gone", nil)
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	shared := owner.createTracker(family, "Shared", nil)
	if r := owner.post(fmt.Sprintf("/trackers/%d/archive", gone.ID), nil); r.status != http.StatusOK && r.status != http.StatusSeeOther {
		t.Fatalf("archive: %d", r.status)
	}
	home := owner.get("/").body
	if strings.Contains(home, "Gone") || !strings.Contains(home, "Kept") || !strings.Contains(home, "Shared") {
		t.Fatalf("archived tracker on the list:\n%s", home)
	}
	if !strings.Contains(home, fmt.Sprintf(`action="/trackers/%d/quick"`, kept.ID)) || !strings.Contains(home, fmt.Sprintf(`action="/trackers/%d/quick"`, shared.ID)) {
		t.Fatal("archiving removed a tracker that is still active")
	}
}

func TestRenamedPersonalHouseholdStaysUnlabeled(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	personal := ts.personalHousehold(t, "owner@example.com")
	owner.post(fmt.Sprintf("/households/%d", personal.ID), url.Values{"name": {"Private"}})
	owner.createTracker(personal, "Private meal", nil)
	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	owner.createTracker(family, "Family meal", nil)

	home := owner.get("/").body
	if strings.Contains(home, householdHref(personal.ID)) || strings.Contains(home, ">Private</a>") {
		t.Fatalf("renaming the personal household put a label on its trackers:\n%s", home)
	}
	if !strings.Contains(home, fmt.Sprintf(`href="/households/%d">Family</a>`, family.ID)) {
		t.Fatalf("the other household lost its label:\n%s", home)
	}
}
