package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Helpers shared by the phase 2 tests.

func (ts *testServer) personalHousehold(t *testing.T, email string) Household {
	t.Helper()
	var h Household
	err := ts.app.db.Table("households").
		Select("households.*").
		Joins("JOIN household_members hm ON hm.household_id = households.id").
		Where("hm.user_id = ? AND hm.role = ?", ts.user(t, email).ID, roleOwner).
		Order("households.id").
		Take(&h).Error
	if err != nil {
		t.Fatalf("personal household for %s: %v", email, err)
	}
	return h
}

func (ts *testServer) household(t *testing.T, id uint) Household {
	t.Helper()
	var h Household
	if err := ts.app.db.Take(&h, id).Error; err != nil {
		t.Fatal(err)
	}
	return h
}

func (ts *testServer) role(t *testing.T, h Household, email string) string {
	t.Helper()
	var m HouseholdMember
	if err := ts.app.db.Take(&m, "household_id = ? AND user_id = ?", h.ID, ts.user(t, email).ID).Error; err != nil {
		return ""
	}
	return m.Role
}

// sharedHouse is the dog tracker setup: an owner, and a member who joined through the invite link.
func sharedHouse(t *testing.T) (ts *testServer, owner, member *browser, h Household) {
	t.Helper()
	ts = newTestServer(t)
	owner = ts.browser(t)
	owner.signup("owner@example.com", "")
	h = ts.personalHousehold(t, "owner@example.com")
	owner.post(fmt.Sprintf("/households/%d/invite", h.ID), nil)
	h = ts.household(t, h.ID)
	member = ts.browser(t)
	member.signup("member@example.com", "")
	member.post("/join/"+*h.InviteToken, nil)
	if ts.role(t, h, "member@example.com") != roleMember {
		t.Fatal("member did not join")
	}
	return ts, owner, member, h
}

func (b *browser) createTracker(h Household, name string, extra url.Values) Tracker {
	b.t.Helper()
	form := url.Values{"household": {fmt.Sprint(h.ID)}, "name": {name}}
	for k, v := range extra {
		form[k] = v
	}
	if r := b.post("/trackers", form); r.status != http.StatusOK {
		b.t.Fatalf("create tracker %q: %d\n%s", name, r.status, r.body)
	}
	var tr Tracker
	if err := b.ts.app.db.Take(&tr, "name = ? AND household_id = ?", name, h.ID).Error; err != nil {
		b.t.Fatal(err)
	}
	return tr
}

// Households

func TestSignupCreatesPersonalHousehold(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")

	h := ts.personalHousehold(t, "dog@example.com")
	if h.Name != "My trackers" || h.InviteToken != nil {
		t.Fatalf("household %+v", h)
	}
	home := b.get("/")
	if !strings.Contains(home.body, "My trackers") || !strings.Contains(home.body, "No trackers yet") {
		t.Fatal("dashboard doesn't show the personal household")
	}
}

func TestBackfillGivesPhase1UsersAHousehold(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("new@example.com", "")
	db := ts.app.db
	for _, email := range []string{"old1@example.com", "old2@example.com"} {
		if err := db.Create(&User{Email: email, TimeZone: "UTC"}).Error; err != nil {
			t.Fatal(err)
		}
	}

	for range 2 {
		if err := backfillPersonalHouseholds(db); err != nil {
			t.Fatal(err)
		}
	}
	var households, members int64
	db.Model(&Household{}).Count(&households)
	db.Model(&HouseholdMember{}).Count(&members)
	if households != 3 || members != 3 {
		t.Fatalf("%d households and %d memberships for 3 users", households, members)
	}
	for _, email := range []string{"old1@example.com", "old2@example.com", "new@example.com"} {
		if h := ts.personalHousehold(t, email); h.Name != "My trackers" {
			t.Errorf("%s: household %q", email, h.Name)
		}
	}
}

func TestInviteLinkJoinFlow(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")

	r := owner.post(fmt.Sprintf("/households/%d/invite", h.ID), nil)
	h = ts.household(t, h.ID)
	if h.InviteToken == nil || !strings.Contains(r.body, "/join/"+*h.InviteToken) {
		t.Fatal("invite link not shown")
	}
	join := "/join/" + *h.InviteToken

	// A new person opens the link, signs up, and comes back to the invitation.
	b := ts.browser(t)
	r = b.get(join)
	if r.url.Path != "/login" || r.url.Query().Get("next") != join {
		t.Fatalf("logged-out invite ended at %s", r.url)
	}
	r = b.post("/signup", url.Values{"email": {"member@example.com"}, "timezone": {"UTC"}, "next": {join}})
	if r.url.Path != join || !strings.Contains(r.body, "Join household") {
		t.Fatalf("after signup ended at %s", r.url)
	}
	if ts.role(t, h, "member@example.com") != "" {
		t.Fatal("opening the invite joined without a POST")
	}
	r = b.post(join, nil)
	if ts.role(t, h, "member@example.com") != roleMember {
		t.Fatal("not joined as member")
	}
	// Two households named "My trackers": the partner's shows its creator.
	if !strings.Contains(r.body, "owner@example.com") {
		t.Error("dashboard doesn't show whose household it is")
	}
	if strings.Count(r.body, "My trackers") != 2 {
		t.Error("expected both households on the dashboard")
	}
	if r := b.get(join); !strings.Contains(r.body, "already in this household") {
		t.Error("second visit doesn't say already a member")
	}

	// Regenerating kills the old link; turning invites off kills the new one.
	owner.post(fmt.Sprintf("/households/%d/invite", h.ID), nil)
	late := ts.browser(t)
	late.signup("late@example.com", "")
	if r := late.post(join, nil); r.status != http.StatusNotFound || ts.role(t, h, "late@example.com") != "" {
		t.Fatalf("old invite after regenerate: %d", r.status)
	}
	h = ts.household(t, h.ID)
	fresh := "/join/" + *h.InviteToken
	owner.post(fmt.Sprintf("/households/%d/invite/delete", h.ID), nil)
	if r := late.post(fresh, nil); r.status != http.StatusNotFound || ts.role(t, h, "late@example.com") != "" {
		t.Fatalf("invite after turning off: %d", r.status)
	}
}

func TestMembersCantManageHousehold(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	token := *h.InviteToken
	ownerID := ts.user(t, "owner@example.com").ID
	for _, path := range []string{
		fmt.Sprintf("/households/%d/invite", h.ID),
		fmt.Sprintf("/households/%d/invite/delete", h.ID),
		fmt.Sprintf("/households/%d/members/%d/delete", h.ID, ownerID),
		fmt.Sprintf("/households/%d/members/%d/owner", h.ID, ts.user(t, "member@example.com").ID),
	} {
		if r := member.post(path, nil); r.status != http.StatusForbidden {
			t.Errorf("member POST %s: %d", path, r.status)
		}
	}
	if h = ts.household(t, h.ID); h.InviteToken == nil || *h.InviteToken != token {
		t.Error("member changed the invite link")
	}
	if ts.role(t, h, "member@example.com") != roleMember || ts.role(t, h, "owner@example.com") != roleOwner {
		t.Error("member changed roles")
	}
	page := member.get(fmt.Sprintf("/households/%d", h.ID)).body
	if strings.Contains(page, "Remove") || strings.Contains(page, "/join/") {
		t.Error("member sees owner controls")
	}
	if !strings.Contains(owner.get(fmt.Sprintf("/households/%d", h.ID)).body, "Make owner") {
		t.Error("owner doesn't see member controls")
	}
}

func TestOwnerRemovesAndPromotesMembers(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	third := ts.browser(t)
	third.signup("third@example.com", "")
	third.post("/join/"+*h.InviteToken, nil)

	owner.post(fmt.Sprintf("/households/%d/members/%d/owner", h.ID, ts.user(t, "member@example.com").ID), nil)
	if ts.role(t, h, "member@example.com") != roleOwner {
		t.Fatal("promote failed")
	}
	owner.post(fmt.Sprintf("/households/%d/members/%d/delete", h.ID, ts.user(t, "third@example.com").ID), nil)
	if ts.role(t, h, "third@example.com") != "" {
		t.Fatal("remove failed")
	}
	if r := third.get(fmt.Sprintf("/households/%d", h.ID)); r.status != http.StatusNotFound {
		t.Fatalf("removed member still sees the household: %d", r.status)
	}

	// No owner can be removed, including yourself, and there is no demotion.
	ownerID := ts.user(t, "owner@example.com").ID
	for name, b := range map[string]*browser{"self": owner, "other owner": member} {
		if r := b.post(fmt.Sprintf("/households/%d/members/%d/delete", h.ID, ownerID), nil); r.status != http.StatusForbidden {
			t.Errorf("%s removing an owner: %d", name, r.status)
		}
	}
	if ts.role(t, h, "owner@example.com") != roleOwner {
		t.Fatal("an owner was removed")
	}
}

func TestNonMembersGet404ForHouseholds(t *testing.T) {
	ts, _, _, h := sharedHouse(t)
	stranger := ts.browser(t)
	stranger.signup("stranger@example.com", "")
	if r := stranger.get(fmt.Sprintf("/households/%d", h.ID)); r.status != http.StatusNotFound {
		t.Errorf("GET: %d", r.status)
	}
	if r := stranger.post(fmt.Sprintf("/households/%d/invite", h.ID), nil); r.status != http.StatusNotFound {
		t.Errorf("POST invite: %d", r.status)
	}
	if r := stranger.get("/households/999"); r.status != http.StatusNotFound {
		t.Errorf("missing household: %d", r.status)
	}
}
