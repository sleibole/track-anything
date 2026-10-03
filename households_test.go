package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
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
		fmt.Sprintf("/households/%d", h.ID),
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
	if strings.Contains(page, "Remove") || strings.Contains(page, "/join/") || strings.Contains(page, "New tracker") || strings.Contains(page, "Rename") {
		t.Error("member sees owner controls")
	}
	if !strings.Contains(page, "Leave household") {
		t.Error("member has no way to leave")
	}
	ownerPage := owner.get(fmt.Sprintf("/households/%d", h.ID)).body
	if !strings.Contains(ownerPage, "Make owner") || !strings.Contains(ownerPage, "Rename") {
		t.Error("owner doesn't see member controls")
	}
	if strings.Contains(ownerPage, "Leave household") {
		t.Error("owner sees leave")
	}
	if !strings.Contains(ownerPage, fmt.Sprintf(`/trackers/new?household=%d`, h.ID)) {
		t.Error("owner has no way to add a tracker from the household page")
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

func TestRemovingAMemberRotatesTheInviteLink(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	old := *h.InviteToken
	gone := ts.browser(t)
	gone.signup("gone@example.com", "")
	gone.post("/join/"+old, nil)

	owner.post(fmt.Sprintf("/households/%d/members/%d/delete", h.ID, ts.user(t, "gone@example.com").ID), nil)
	h = ts.household(t, h.ID)
	if h.InviteToken == nil || *h.InviteToken == old {
		t.Fatal("invite token was not rotated")
	}
	again := ts.browser(t)
	again.signup("again@example.com", "")
	if r := again.post("/join/"+old, nil); r.status != http.StatusNotFound || ts.role(t, h, "again@example.com") != "" {
		t.Fatalf("old invite still works: %d", r.status)
	}
	if r := again.post("/join/"+*h.InviteToken, nil); ts.role(t, h, "again@example.com") != roleMember {
		t.Fatalf("new invite: %d at %s", r.status, r.url)
	}

	// Invites that are off stay off. Removing a member must not turn them back on.
	owner.post(fmt.Sprintf("/households/%d/invite/delete", h.ID), nil)
	owner.post(fmt.Sprintf("/households/%d/members/%d/delete", h.ID, ts.user(t, "again@example.com").ID), nil)
	if ts.household(t, h.ID).InviteToken != nil {
		t.Fatal("removing a member turned invites back on")
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
	if r := stranger.post(fmt.Sprintf("/households/%d", h.ID), url.Values{"name": {"Nope"}}); r.status != http.StatusNotFound {
		t.Errorf("POST rename: %d", r.status)
	}
	if r := stranger.post(fmt.Sprintf("/households/%d/leave", h.ID), nil); r.status != http.StatusNotFound {
		t.Errorf("POST leave: %d", r.status)
	}
	if r := stranger.get("/households/999"); r.status != http.StatusNotFound {
		t.Errorf("missing household: %d", r.status)
	}
}

func (ts *testServer) ownedNamed(t *testing.T, email, name string) Household {
	t.Helper()
	var h Household
	err := ts.app.db.Table("households").
		Select("households.*").
		Joins("JOIN household_members hm ON hm.household_id = households.id").
		Where("hm.user_id = ? AND hm.role = ? AND households.name = ?", ts.user(t, email).ID, roleOwner, name).
		Take(&h).Error
	if err != nil {
		t.Fatalf("household %q owned by %s: %v", name, email, err)
	}
	return h
}

func headingShows(body string, id uint, email string) bool {
	needle := fmt.Sprintf(`/households/%d">`, id)
	i := strings.Index(body, needle)
	if i < 0 {
		return false
	}
	rest := body[i:]
	end := strings.Index(rest, "</h2>")
	if end < 0 {
		return false
	}
	return strings.Contains(rest[:end], email)
}

func TestCreateAndRenameHousehold(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	if !strings.Contains(owner.get("/").body, "New household") {
		t.Fatal("dashboard has no way to create a household")
	}

	if r := owner.post("/households", url.Values{"name": {"   "}}); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "Give the household a name.") {
		t.Fatalf("blank name: %d", r.status)
	}
	if r := owner.post("/households", url.Values{"name": {strings.Repeat("a", maxTrackerName+1)}}); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "60 characters") {
		t.Fatalf("long name: %d", r.status)
	}
	exact := strings.Repeat("b", maxTrackerName)
	if r := owner.post("/households", url.Values{"name": {exact}}); r.status != http.StatusOK {
		t.Fatalf("name at the limit: %d", r.status)
	}

	r := owner.post("/households", url.Values{"name": {"  Family  "}})
	h := ts.ownedNamed(t, "owner@example.com", "Family")
	if r.url.Path != fmt.Sprintf("/households/%d", h.ID) {
		t.Fatalf("create landed at %s", r.url)
	}
	if h.InviteToken != nil {
		t.Fatal("a new household starts with invites on")
	}
	if ts.role(t, h, "owner@example.com") != roleOwner {
		t.Fatal("creator is not the owner")
	}
	if !strings.Contains(r.body, "Invite someone") {
		t.Fatal("new household page has no invite control")
	}

	// Renaming to the same name, including spacing and case, is allowed.
	if r := owner.post(fmt.Sprintf("/households/%d", h.ID), url.Values{"name": {" family "}}); r.url.Path != fmt.Sprintf("/households/%d", h.ID) {
		t.Fatalf("rename landed at %s", r.url)
	}
	if got := ts.household(t, h.ID).Name; got != "family" {
		t.Fatalf("renamed to %q", got)
	}

	member := ts.browser(t)
	member.signup("member@example.com", "")
	if r := member.post(fmt.Sprintf("/households/%d", h.ID), url.Values{"name": {"Nope"}}); r.status != http.StatusNotFound || ts.household(t, h.ID).Name != "family" {
		t.Fatalf("non-member rename: %d", r.status)
	}
}

func TestOwnedHouseholdNamesStayDistinctForOnePerson(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	owner.post("/households", url.Values{"name": {"Family"}})
	h := ts.ownedNamed(t, "owner@example.com", "Family")

	for _, name := range []string{" family ", "FAMILY"} {
		r := owner.post("/households", url.Values{"name": {name}})
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "already have a household") {
			t.Fatalf("create %q: %d", name, r.status)
		}
	}
	if r := owner.post(fmt.Sprintf("/households/%d", ts.personalHousehold(t, "owner@example.com").ID), url.Values{"name": {" family "}}); r.status != http.StatusUnprocessableEntity || ts.household(t, h.ID).Name != "Family" {
		t.Fatalf("rename onto an owned name: %d", r.status)
	}
	personal := ts.personalHousehold(t, "owner@example.com")
	if personal.Name != "My trackers" {
		t.Fatalf("rejected rename changed the personal household to %q", personal.Name)
	}

	other := ts.browser(t)
	other.signup("other@example.com", "")
	if r := other.post("/households", url.Values{"name": {"Family"}}); r.status != http.StatusOK {
		t.Fatalf("another user owning Family: %d", r.status)
	}
	if ts.ownedNamed(t, "other@example.com", "Family").ID == h.ID {
		t.Fatal("the other user joined the existing Family instead of creating one")
	}

	owner.post(fmt.Sprintf("/households/%d/invite", h.ID), nil)
	h = ts.household(t, h.ID)
	member := ts.browser(t)
	member.signup("member@example.com", "")
	member.post("/join/"+*h.InviteToken, nil)
	if ts.role(t, h, "member@example.com") != roleMember {
		t.Fatal("did not join")
	}
	if r := member.post("/households", url.Values{"name": {"Family"}}); r.status != http.StatusOK {
		t.Fatalf("member of someone else's Family creating their own: %d", r.status)
	}
	if ts.role(t, ts.ownedNamed(t, "member@example.com", "Family"), "member@example.com") != roleOwner {
		t.Fatal("membership blocked owning a household of the same name")
	}
}

func TestDashboardDisambiguatesDuplicateNames(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	owner.post("/households", url.Values{"name": {"Family"}})
	h := ts.ownedNamed(t, "owner@example.com", "Family")
	owner.post(fmt.Sprintf("/households/%d/invite", h.ID), nil)
	h = ts.household(t, h.ID)

	member := ts.browser(t)
	member.signup("member@example.com", "")
	member.post("/join/"+*h.InviteToken, nil)

	// "My trackers" and "Family" don't match, so the heading is just the name.
	home := member.get("/").body
	if headingShows(home, h.ID, "owner@example.com") || strings.Contains(home, "member@example.com") {
		t.Fatalf("unique names were disambiguated:\n%s", home)
	}

	// The member can rename their own household to Family. They don't own the other one.
	personal := ts.personalHousehold(t, "member@example.com")
	member.post(fmt.Sprintf("/households/%d", personal.ID), url.Values{"name": {" family "}})
	personal = ts.household(t, personal.ID)
	if personal.Name != "family" {
		t.Fatalf("rename to own name stored %q", personal.Name)
	}
	home = member.get("/").body
	if !headingShows(home, h.ID, "owner@example.com") {
		t.Fatal("a partner's same-named household doesn't show who created it")
	}
	if headingShows(home, personal.ID, "member@example.com") || strings.Contains(home, "member@example.com") {
		t.Fatal("the viewer's own household shows their email")
	}
}

func TestMoveTrackerBetweenOwnedHouseholds(t *testing.T) {
	ts, owner, member, personal := sharedHouse(t)
	tr := owner.createTracker(personal, "Millie ate", url.Values{
		"icon": {"paw"}, "accent": {"green"}, "log_label": {"+ Ate"}, "summary_display": {"done"},
	})
	edit := fmt.Sprintf("/trackers/%d/edit", tr.ID)
	if strings.Contains(owner.get(edit).body, "Move to household") {
		t.Fatal("move is offered when the owner has nowhere to move it")
	}

	owner.post("/households", url.Values{"name": {"Family"}})
	family := ts.ownedNamed(t, "owner@example.com", "Family")
	other := owner.createTracker(family, "Already there", nil)

	owner.post(fmt.Sprintf("/households/%d/invite", family.ID), nil)
	family = ts.household(t, family.ID)
	dest := ts.browser(t)
	dest.signup("dest@example.com", "")
	dest.post("/join/"+*family.InviteToken, nil)

	if r := member.get(fmt.Sprintf("/trackers/%d", tr.ID)); r.status != http.StatusOK {
		t.Fatal("source member can't see the tracker yet")
	}
	if r := dest.get(fmt.Sprintf("/trackers/%d", tr.ID)); r.status != http.StatusNotFound {
		t.Fatal("destination member can see the tracker before the move")
	}

	owner.post(fmt.Sprintf("/trackers/%d/quick", tr.ID), nil)
	entryID := ts.entries(t, tr.ID)[0].ID
	ts.clock.advance(25 * time.Hour)
	owner.post(fmt.Sprintf("/trackers/%d/zero", tr.ID), nil)
	zeroID := ts.zeros(t, tr.ID)[0].ID
	owner.post(fmt.Sprintf("/trackers/%d/share", tr.ID), nil)
	tr = ts.tracker(t, tr.ID)
	token := *tr.ShareToken

	page := owner.get(edit).body
	for _, want := range []string{"Move to household", "lose access", "gain access", family.Name, "share link will keep working"} {
		if !strings.Contains(page, want) {
			t.Errorf("move confirmation missing %q", want)
		}
	}

	move := fmt.Sprintf("/trackers/%d/move", tr.ID)
	if r := owner.post(move, url.Values{"household": {fmt.Sprint(personal.ID)}}); r.status != http.StatusUnprocessableEntity || ts.tracker(t, tr.ID).HouseholdID != personal.ID {
		t.Fatalf("move into the current household: %d", r.status)
	}
	if r := member.post(move, url.Values{"household": {fmt.Sprint(family.ID)}}); r.status != http.StatusForbidden || ts.tracker(t, tr.ID).HouseholdID != personal.ID {
		t.Fatalf("member move: %d", r.status)
	}
	if r := dest.post(move, url.Values{"household": {fmt.Sprint(family.ID)}}); r.status != http.StatusNotFound || ts.tracker(t, tr.ID).HouseholdID != personal.ID {
		t.Fatalf("non-member move: %d", r.status)
	}
	// A household the owner only belongs to, and one they can't see, are both refused.
	if r := member.post(fmt.Sprintf("/trackers/%d/move", member.createTracker(ts.personalHousehold(t, "member@example.com"), "Private", nil).ID), url.Values{"household": {fmt.Sprint(personal.ID)}}); r.status != http.StatusForbidden {
		t.Fatalf("move into a household where the user is only a member: %d", r.status)
	}
	strangerHouse := ts.personalHousehold(t, "dest@example.com")
	private := ts.tracker(t, member.createTracker(ts.personalHousehold(t, "member@example.com"), "Still private", nil).ID)
	if r := member.post(fmt.Sprintf("/trackers/%d/move", private.ID), url.Values{"household": {fmt.Sprint(strangerHouse.ID)}}); r.status != http.StatusNotFound || ts.tracker(t, private.ID).HouseholdID != private.HouseholdID {
		t.Fatalf("move into an unseen household: %d", r.status)
	}

	if r := owner.post(move, url.Values{"household": {fmt.Sprint(family.ID)}}); r.url.Path != fmt.Sprintf("/trackers/%d", tr.ID) {
		t.Fatalf("move landed at %s", r.url)
	}
	got := ts.tracker(t, tr.ID)
	if got.ID != tr.ID || got.HouseholdID != family.ID || got.Name != "Millie ate" || got.Icon != "paw" || got.Accent != "green" || got.LogLabel != "+ Ate" || got.SummaryDisplay != "done" {
		t.Fatalf("tracker after move: %+v", got)
	}
	if got.ShareToken == nil || *got.ShareToken != token {
		t.Fatal("move replaced the share link")
	}
	if got.Position <= ts.tracker(t, other.ID).Position {
		t.Fatalf("position %d is not last (other is %d)", got.Position, ts.tracker(t, other.ID).Position)
	}
	es := ts.entries(t, tr.ID)
	zs := ts.zeros(t, tr.ID)
	if len(es) != 1 || es[0].ID != entryID || len(zs) != 1 || zs[0].ID != zeroID {
		t.Fatalf("history changed: entries %+v zeros %+v", es, zs)
	}
	if r := ts.browser(t).get("/s/" + token); r.status != http.StatusOK || !strings.Contains(r.body, "Millie ate") {
		t.Fatalf("share link after move: %d", r.status)
	}
	if r := member.get(fmt.Sprintf("/trackers/%d", tr.ID)); r.status != http.StatusNotFound {
		t.Fatal("a member of only the old household can still see the tracker")
	}
	if r := member.post(fmt.Sprintf("/trackers/%d/quick", tr.ID), nil); r.status != http.StatusNotFound || len(ts.entries(t, tr.ID)) != 1 {
		t.Fatal("a member of only the old household can still log")
	}
	if r := dest.post(fmt.Sprintf("/trackers/%d/quick", tr.ID), nil); r.status != http.StatusOK || len(ts.entries(t, tr.ID)) != 2 {
		t.Fatalf("destination member log: %d", r.status)
	}

	archived := owner.createTracker(personal, "Old", nil)
	owner.post(fmt.Sprintf("/trackers/%d/archive", archived.ID), nil)
	if r := owner.post(fmt.Sprintf("/trackers/%d/move", archived.ID), url.Values{"household": {fmt.Sprint(family.ID)}}); r.status != http.StatusNotFound || ts.tracker(t, archived.ID).HouseholdID != personal.ID {
		t.Fatalf("move archived: %d", r.status)
	}
}

func TestLeaveHousehold(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	token := *h.InviteToken
	personal := ts.personalHousehold(t, "member@example.com")

	owner.post("/households", url.Values{"name": {"Home"}})
	home := ts.ownedNamed(t, "owner@example.com", "Home")
	owner.post(fmt.Sprintf("/households/%d/invite", home.ID), nil)
	home = ts.household(t, home.ID)
	member.post("/join/"+*home.InviteToken, nil)
	if ts.role(t, home, "member@example.com") != roleMember {
		t.Fatal("did not join the second household")
	}

	if r := owner.post(fmt.Sprintf("/households/%d/leave", h.ID), nil); r.status != http.StatusForbidden || ts.role(t, h, "owner@example.com") != roleOwner {
		t.Fatalf("owner leave: %d", r.status)
	}
	if got := ts.household(t, h.ID).InviteToken; got == nil || *got != token {
		t.Fatal("owner leave changed the invite link")
	}

	r := member.post(fmt.Sprintf("/households/%d/leave", h.ID), nil)
	if r.url.Path != "/" || ts.role(t, h, "member@example.com") != "" {
		t.Fatalf("member leave: %d at %s, role %q", r.status, r.url, ts.role(t, h, "member@example.com"))
	}
	if got := ts.household(t, h.ID).InviteToken; got == nil || *got != token {
		t.Fatal("leaving rotated the invite link")
	}
	if ts.role(t, personal, "member@example.com") != roleOwner {
		t.Fatal("leaving changed the personal household")
	}
	if ts.role(t, home, "member@example.com") != roleMember {
		t.Fatal("leaving one household dropped another membership")
	}
	if r := member.get(fmt.Sprintf("/households/%d", h.ID)); r.status != http.StatusNotFound {
		t.Fatal("someone who left can still open the household")
	}
}
