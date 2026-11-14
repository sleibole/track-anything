package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func (ts *testServer) groupNamed(t *testing.T, email, name string) TrackerGroup {
	t.Helper()
	var g TrackerGroup
	err := ts.app.db.Where("user_id = ? AND name = ?", ts.user(t, email).ID, name).Take(&g).Error
	if err != nil {
		t.Fatalf("group %q for %s: %v", name, email, err)
	}
	return g
}

func TestPersonalGroupsStayOnOneList(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	owner.createTracker(h, "Alpha", nil)
	owner.createTracker(h, "Beta", nil)
	owner.createTracker(h, "Gamma", nil)

	before := owner.get("/").body
	assertOrder(t, before, "Alpha", "Beta", "Gamma")
	if strings.Contains(before, "Ungrouped") || strings.Contains(before, "data-group") {
		t.Fatal("a list with no groups invented a section")
	}
	if strings.Count(before, "data-reorder-list") != 1 || strings.Count(before, "Drag to reorder this tracker") != 3 {
		t.Fatal("grouping changed the single home reorder list")
	}

	if r := owner.post("/groups", url.Values{"name": {"Pack"}}); r.url.Path != "/" || !strings.Contains(r.body, "Pack") {
		t.Fatalf("create group: %d %s", r.status, r.url)
	}
	home := owner.get("/").body
	assertOrder(t, home, "Alpha", "Beta", "Gamma", "Pack")
	if strings.Contains(home, "Ungrouped") {
		t.Fatal("ungrouped trackers got a heading")
	}
	pack := ts.groupNamed(t, "owner@example.com", "Pack")
	if pack.Collapsed {
		t.Fatal("a new group started collapsed")
	}
	if !strings.Contains(home, `aria-expanded="true"`) || strings.Contains(home, `id="group-body-`+itoa(pack.ID)+`" hidden`) {
		t.Fatalf("new group was not expanded:\n%s", home)
	}
	if !strings.Contains(home, "· 0 trackers") || !strings.Contains(home, `aria-controls="group-body-`+itoa(pack.ID)+`"`) {
		t.Fatal("group header missing the count or aria-controls")
	}
	if !strings.Contains(home, "Drag to reorder group Pack") || !strings.Contains(home, "Actions for Pack") {
		t.Fatal("group header missing the reorder handle or actions menu")
	}
	if strings.Count(home, "data-reorder-list") != 1 {
		t.Fatal("a group added another top-level reorder list")
	}
}

func TestPersonalGroupsAreNotShared(t *testing.T) {
	ts, owner, member, shared := sharedHouse(t)
	alpha := owner.createTracker(shared, "Alpha", nil)
	beta := owner.createTracker(shared, "Beta", nil)
	if strings.Contains(member.get(trackerURL(alpha)).body, `href="`+fmt.Sprintf("/trackers/%d/edit", alpha.ID)+`"`) {
		t.Fatal("member can edit a shared tracker")
	}
	page := member.get(trackerURL(alpha)).body
	if !strings.Contains(page, "Your list") || !strings.Contains(page, "Create group…") || !strings.Contains(page, "only organizes your list") {
		t.Fatal("tracker page missing the personal group control")
	}

	if r := member.post(trackerURL(alpha)+"/group", url.Values{"group": {"new"}, "group_name": {"Mine"}}); r.url.Path != trackerURL(alpha) {
		t.Fatalf("member group: %d %s\n%s", r.status, r.url, r.body)
	}
	memberHome := member.get("/").body
	if !strings.Contains(memberHome, "Mine") || !strings.Contains(memberHome, "Alpha") {
		t.Fatalf("member home:\n%s", memberHome)
	}
	ownerHome := owner.get("/").body
	if strings.Contains(ownerHome, "Mine") || strings.Contains(ownerHome, "data-group") {
		t.Fatal("a member's group changed the owner's list")
	}
	if ts.tracker(t, alpha.ID).HouseholdID != shared.ID || ts.tracker(t, beta.ID).Name != "Beta" {
		t.Fatal("grouping changed the shared tracker")
	}

	mine := ts.groupNamed(t, "member@example.com", "Mine")
	if r := owner.post(fmt.Sprintf("/groups/%d/delete", mine.ID), nil); r.status != http.StatusNotFound {
		t.Fatalf("owner delete of member group: %d", r.status)
	}
	if r := owner.post(fmt.Sprintf("/groups/%d/collapse", mine.ID), url.Values{"collapsed": {"1"}}); r.status != http.StatusNotFound {
		t.Fatalf("owner collapse of member group: %d", r.status)
	}
	if ts.groupNamed(t, "member@example.com", "Mine").Collapsed {
		t.Fatal("another person collapsed this group")
	}
	if r := owner.post(trackerURL(alpha)+"/group", url.Values{"group": {itoa(mine.ID)}}); r.status != http.StatusNotFound {
		t.Fatalf("owner used the member's group: %d", r.status)
	}

	outsider := ts.browser(t)
	outsider.signup("outsider@example.com", "")
	hidden := outsider.createTracker(ts.personalHousehold(t, "outsider@example.com"), "Hidden", nil)
	if r := member.post(trackerURL(hidden)+"/group", url.Values{"group": {"new"}, "group_name": {"Nope"}}); r.status != http.StatusNotFound {
		t.Fatalf("grouped a hidden tracker: %d", r.status)
	}
	if strings.Contains(member.get("/").body, "Hidden") {
		t.Fatal("a rejected group exposed another person's tracker")
	}

	guest := ts.browser(t)
	if r := guest.post("/groups", url.Values{"name": {"Nope"}}); r.url.Path != "/login" {
		t.Fatalf("logged out ended at %s", r.url.Path)
	}
}

func TestDeleteGroupKeepsTrackers(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	alpha := owner.createTracker(h, "Alpha", nil)
	beta := owner.createTracker(h, "Beta", nil)
	if r := owner.post(trackerURL(beta)+"/quick", nil); r.status != http.StatusOK || len(ts.entries(t, beta.ID)) != 1 {
		t.Fatalf("log: %d", r.status)
	}
	owner.post("/groups", url.Values{"name": {"Pack"}})
	pack := ts.groupNamed(t, "owner@example.com", "Pack")
	owner.post(trackerURL(beta)+"/group", url.Values{"group": {itoa(pack.ID)}})
	owner.post(trackerURL(alpha)+"/group", url.Values{"group": {itoa(pack.ID)}})

	if r := owner.post(fmt.Sprintf("/groups/%d/delete", pack.ID), nil); r.url.Path != "/" {
		t.Fatalf("delete: %d %s", r.status, r.url)
	}
	home := owner.get("/").body
	if strings.Contains(home, "Pack") || strings.Contains(home, "data-group") {
		t.Fatal("deleted group still on the list")
	}
	assertOrder(t, home, "Beta", "Alpha")
	if ts.tracker(t, alpha.ID).ID == 0 || ts.tracker(t, beta.ID).Name != "Beta" || len(ts.entries(t, beta.ID)) != 1 {
		t.Fatal("deleting a group removed a tracker or its entries")
	}
	var n int64
	if err := ts.app.db.Model(&TrackerGroupItem{}).Where("group_id = ?", pack.ID).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("membership rows left: %d %v", n, err)
	}
}

func TestGroupOrderAndCollapse(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	alpha := owner.createTracker(h, "Alpha", nil)
	beta := owner.createTracker(h, "Beta", nil)
	gamma := owner.createTracker(h, "Gamma", nil)

	owner.post("/groups", url.Values{"name": {"Pack"}})
	pack := ts.groupNamed(t, "owner@example.com", "Pack")
	owner.post(trackerURL(beta)+"/group", url.Values{"group": {itoa(pack.ID)}})
	assertOrder(t, owner.get("/").body, "Alpha", "Gamma", "Pack", "Beta")

	owner.post(trackerURL(gamma)+"/group", url.Values{"group": {itoa(pack.ID)}})
	assertOrder(t, owner.get("/").body, "Alpha", "Pack", "Beta", "Gamma")

	if r := owner.post(fmt.Sprintf("/groups/%d/order", pack.ID), idValues(gamma.ID, beta.ID)); r.status != http.StatusNoContent {
		t.Fatalf("group order: %d\n%s", r.status, r.body)
	}
	assertOrder(t, owner.get("/").body, "Alpha", "Pack", "Gamma", "Beta")
	if ts.tracker(t, gamma.ID).Position == 0 && ts.tracker(t, beta.ID).HouseholdID != h.ID {
		t.Fatal("group order changed the household")
	}

	slots := url.Values{}
	slots.Add("slot", "g:"+itoa(pack.ID))
	slots.Add("slot", "t:"+itoa(alpha.ID))
	if r := owner.post("/trackers/order", slots); r.status != http.StatusNoContent {
		t.Fatalf("slot order: %d\n%s", r.status, r.body)
	}
	assertOrder(t, owner.get("/").body, "Pack", "Gamma", "Beta", "Alpha")

	bad := url.Values{}
	bad.Add("slot", "t:"+itoa(alpha.ID))
	if r := owner.post("/trackers/order", bad); r.status != http.StatusConflict {
		t.Fatalf("partial slots: %d", r.status)
	}
	assertOrder(t, owner.get("/").body, "Pack", "Gamma", "Beta", "Alpha")

	owner.post(trackerURL(beta)+"/group", url.Values{"group": {""}})
	assertOrder(t, owner.get("/").body, "Pack", "Gamma", "Beta", "Alpha")

	owner.post("/groups", url.Values{"name": {"Other"}})
	other := ts.groupNamed(t, "owner@example.com", "Other")
	if r := owner.post(fmt.Sprintf("/groups/%d/collapse", pack.ID), url.Values{"collapsed": {"1"}}); r.url.Path != "/" {
		t.Fatalf("collapse: %d %s", r.status, r.url)
	}
	home := owner.get("/").body
	if !groupBodyHidden(home, pack.ID) || !strings.Contains(home, `aria-expanded="false"`) {
		t.Fatal("collapsed group did not stay collapsed")
	}
	if groupBodyHidden(home, other.ID) {
		t.Fatal("collapsing one group collapsed the other")
	}
	if ts.groupNamed(t, "owner@example.com", "Pack").Collapsed != true || ts.groupNamed(t, "owner@example.com", "Other").Collapsed {
		t.Fatal("collapse was not stored per group")
	}
	if r := owner.post("/groups", url.Values{"name": {" pack "}}); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "already have a group") {
		t.Fatalf("duplicate name: %d\n%s", r.status, r.body)
	}
}

func groupBodyHidden(body string, id uint) bool {
	needle := `id="group-body-` + itoa(id) + `"`
	i := strings.Index(body, needle)
	if i < 0 {
		return false
	}
	end := strings.Index(body[i:], ">")
	if end < 0 {
		return false
	}
	return strings.Contains(body[i:i+end], " hidden")
}

func TestGroupSurvivesArchiveAndAMissingTracker(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	alpha := owner.createTracker(h, "Alpha", nil)
	beta := owner.createTracker(h, "Beta", nil)
	owner.post("/groups", url.Values{"name": {"Pack"}})
	pack := ts.groupNamed(t, "owner@example.com", "Pack")
	owner.post(trackerURL(alpha)+"/group", url.Values{"group": {itoa(pack.ID)}})
	owner.post(trackerURL(beta)+"/group", url.Values{"group": {itoa(pack.ID)}})

	owner.post(trackerURL(beta)+"/archive", nil)
	if strings.Contains(owner.get("/").body, "Beta") {
		t.Fatal("archived tracker stayed in the group on screen")
	}
	owner.post(trackerURL(beta)+"/restore", nil)
	home := owner.get("/").body
	packAt := strings.Index(home, "Pack")
	betaAt := strings.Index(home, "Beta")
	alphaAt := strings.Index(home, "Alpha")
	if packAt < 0 || alphaAt < packAt || betaAt < packAt {
		t.Fatalf("restored tracker left the group:\n%s", home)
	}

	if err := ts.app.db.Create(&TrackerGroupItem{UserID: ts.user(t, "owner@example.com").ID, TrackerID: 999999, GroupID: pack.ID, Position: 9}).Error; err != nil {
		t.Fatal(err)
	}
	if r := owner.get("/"); r.status != http.StatusOK || !strings.Contains(r.body, "Alpha") {
		t.Fatalf("missing tracker broke the list: %d", r.status)
	}
}

func TestHomeRefreshKeepsGroupChrome(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("refresh-group@example.com", "")
	h := ts.personalHousehold(t, "refresh-group@example.com")
	alpha := owner.createTracker(h, "Alpha", nil)
	owner.post("/groups", url.Values{"name": {"Pack"}})
	pack := ts.groupNamed(t, "refresh-group@example.com", "Pack")
	owner.post(trackerURL(alpha)+"/group", url.Values{"group": {itoa(pack.ID)}})
	owner.post(fmt.Sprintf("/groups/%d/collapse", pack.ID), url.Values{"collapsed": {"1"}})

	page := owner.get("/").body
	if !strings.Contains(page, `data-layout="g:`+itoa(pack.ID)+`:`+itoa(alpha.ID)+`"`) {
		t.Fatalf("layout marker:\n%s", page)
	}
	if !groupBodyHidden(page, pack.ID) {
		t.Fatal("reload forgot the collapse")
	}
	ids, revs := pageViewRevs(t, page)
	owner.post(trackerURL(alpha)+"/quick", nil)
	body := refreshGet(t, owner, "/?"+url.Values{"present": {ids}, "rev": {revs}}.Encode()).body
	if !strings.Contains(body, `id="tracker-`+itoa(alpha.ID)+`"`) || !strings.Contains(body, `hx-swap-oob="outerHTML"`) {
		t.Fatalf("refresh missed the card:\n%s", body)
	}
	if strings.Contains(body, `id="group-`+itoa(pack.ID)+`"`) || strings.Contains(body, "aria-expanded") {
		t.Fatalf("refresh redrew the group header:\n%s", body)
	}
}
