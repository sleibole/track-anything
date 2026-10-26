package main

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func cookieValue(b *browser, name string) string {
	b.t.Helper()
	u, err := url.Parse(b.ts.srv.URL)
	if err != nil {
		b.t.Fatal(err)
	}
	for _, c := range b.c.Jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func setCookie(b *browser, name, value string) {
	b.t.Helper()
	u, err := url.Parse(b.ts.srv.URL)
	if err != nil {
		b.t.Fatal(err)
	}
	b.c.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
}

func countUsers(t *testing.T, ts *testServer) int64 {
	t.Helper()
	var n int64
	if err := ts.app.db.Model(&User{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHomepageVisitCreatesNoGuest(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.get("/")
	b.get("/")
	if countUsers(t, ts) != 0 {
		t.Fatalf("users %d", countUsers(t, ts))
	}
	if cookieValue(b, sessionCookie) != "" {
		t.Fatal("visit set a session")
	}
}

func TestStartRequiresARealName(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.get("/")
	r := b.post("/start", url.Values{"name": {"  "}})
	if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "Give the tracker a name.") {
		t.Fatalf("status %d", r.status)
	}
	if countUsers(t, ts) != 0 {
		t.Fatal("invalid name created a user")
	}
	long := strings.Repeat("n", maxTrackerName+1)
	r = b.post("/start", url.Values{"name": {long}})
	if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, long) {
		t.Fatalf("long name was not kept: %d", r.status)
	}
	if countUsers(t, ts) != 0 {
		t.Fatal("long name created a user")
	}
}

func TestStartCreatesOneGuestAndRetryReusesIt(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.get("/")
	start := cookieValue(b, startCookie)
	if start == "" {
		t.Fatal("no start cookie")
	}
	r := b.post("/start", url.Values{"name": {"Weight"}, "timezone": {"America/Los_Angeles"}})
	if r.status != http.StatusOK || !strings.Contains(r.body, "What you record") || !strings.Contains(r.body, `value="Weight"`) {
		t.Fatalf("setup %d %s", r.status, r.url)
	}
	if countUsers(t, ts) != 1 {
		t.Fatalf("users %d", countUsers(t, ts))
	}
	var u User
	if err := ts.app.db.Take(&u).Error; err != nil {
		t.Fatal(err)
	}
	if !u.IsGuest() || u.TimeZone != "America/Los_Angeles" {
		t.Fatalf("user %+v", u)
	}
	var houses int64
	ts.app.db.Model(&Household{}).Count(&houses)
	if houses != 1 {
		t.Fatalf("households %d", houses)
	}

	again := ts.browser(t)
	setCookie(again, startCookie, start)
	r = again.post("/start", url.Values{"name": {"Weight"}})
	if r.status != http.StatusOK || countUsers(t, ts) != 1 {
		t.Fatalf("retry users %d status %d", countUsers(t, ts), r.status)
	}
	settings := again.get("/settings")
	if !strings.Contains(settings.body, "guest account") {
		t.Fatal("retry did not sign in the guest")
	}
}

func TestStartCookieDoesNotRestoreAccess(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.get("/")
	start := cookieValue(b, startCookie)
	b.post("/start", url.Values{"name": {"Weight"}})
	b.post("/logout", nil)
	if strings.Contains(b.get("/settings").body, "guest account") {
		t.Fatal("still signed in after logout")
	}
	other := ts.browser(t)
	setCookie(other, startCookie, start)
	r := other.post("/start", url.Values{"name": {"Weight"}})
	if countUsers(t, ts) != 1 || strings.Contains(r.body, "What you record") {
		t.Fatalf("logout retry users %d url %s", countUsers(t, ts), r.url)
	}

	b.get("/")
	start = cookieValue(b, startCookie)
	b.post("/start", url.Values{"name": {"Books read"}})
	if countUsers(t, ts) != 2 {
		t.Fatalf("second guest %d", countUsers(t, ts))
	}
	b.post("/account/email", url.Values{"email": {"keep@example.com"}})
	b.post(ts.lastVerifyLink(t), nil)
	revived := ts.browser(t)
	setCookie(revived, startCookie, start)
	revived.post("/start", url.Values{"name": {"Books read"}})
	if strings.Contains(revived.get("/settings").body, "Logged in as keep@example.com") {
		t.Fatal("start cookie signed in the registered user")
	}

}

func TestExpiredStartDoesNotMint(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.get("/")
	held := cookieValue(b, startCookie)
	b.post("/start", url.Values{"name": {"Hours slept"}})
	ts.clock.advance(startAttemptLifetime + time.Minute)
	expired := ts.browser(t)
	setCookie(expired, startCookie, held)
	expired.post("/start", url.Values{"name": {"Hours slept"}})
	if strings.Contains(expired.get("/settings").body, "guest account") {
		t.Fatal("expired start minted a session")
	}
	if countUsers(t, ts) != 1 {
		t.Fatalf("users %d", countUsers(t, ts))
	}
}

func TestConcurrentStartsCreateOneGuest(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.get("/")
	start := cookieValue(b, startCookie)
	var wg sync.WaitGroup
	var statuses [2]int
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			form := url.Values{"name": {"Miles walked"}}
			req, err := http.NewRequest(http.MethodPost, ts.srv.URL+"/start", strings.NewReader(form.Encode()))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: startCookie, Value: start})
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
			statuses[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()
	if countUsers(t, ts) != 1 {
		t.Fatalf("users %d statuses %v", countUsers(t, ts), statuses)
	}
}

func TestSignedInStartReusesTheAccount(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	before := countUsers(t, ts)
	r := owner.post("/start", url.Values{"name": {"Cups of coffee"}})
	if countUsers(t, ts) != before || !strings.Contains(r.body, `value="Cups of coffee"`) {
		t.Fatalf("users %d", countUsers(t, ts))
	}
	if !owner.loggedInAs("owner@example.com") {
		t.Fatal("start replaced the session")
	}
	guest := ts.browser(t)
	guest.get("/")
	guest.post("/start", url.Values{"name": {"Plants watered"}})
	id := guestUserID(t, ts)
	guest.post("/start", url.Values{"name": {"Money saved"}})
	if guestUserID(t, ts) != id || countUsers(t, ts) != before+1 {
		t.Fatal("signed-in guest was replaced")
	}
}

func guestUserID(t *testing.T, ts *testServer) uint {
	t.Helper()
	var u User
	if err := ts.app.db.Where("email = ''").Take(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestGuestClaimKeepsTheSameUser(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.get("/")
	b.post("/start", url.Values{"name": {"Weight"}})
	guestID := guestUserID(t, ts)
	h := householdOf(t, ts, guestID)
	tr := b.createTracker(h, "Weight", nil)
	b.post("/trackers/"+(User{ID: tr.ID}).IDString()+"/quick", nil)
	sent := b.post("/account/email", url.Values{"email": {"later@example.com"}})
	if strings.Contains(sent.body, "already") || strings.Contains(sent.body, "registered") {
		t.Fatal("add-email disclosed an account")
	}
	link := ts.lastVerifyLink(t)
	if strings.Contains(b.get(link).body, "data-autosubmit") {
		t.Fatal("guest claim autosubmitted")
	}
	var before User
	if err := ts.app.db.Take(&before, guestID).Error; err != nil || before.Email != "" {
		t.Fatal("opening the link attached the email")
	}
	other := ts.browser(t)
	other.post(link, nil)
	if strings.Contains(other.get("/settings").body, "Logged in as") {
		t.Fatal("logged-out confirm signed in")
	}
	var after User
	if err := ts.app.db.Take(&after, guestID).Error; err != nil || after.Email != "later@example.com" || after.EmailVerifiedAt == nil {
		t.Fatalf("attached %+v", after)
	}
	var still Tracker
	if err := ts.app.db.Take(&still, tr.ID).Error; err != nil || still.Name != "Weight" {
		t.Fatal("tracker was not kept")
	}
	if r := other.post(link, nil); r.status != http.StatusGone {
		t.Fatalf("reused %d", r.status)
	}
}

func householdOf(t *testing.T, ts *testServer, userID uint) Household {
	t.Helper()
	var m HouseholdMember
	if err := ts.app.db.Take(&m, "user_id = ?", userID).Error; err != nil {
		t.Fatal(err)
	}
	var h Household
	if err := ts.app.db.Take(&h, m.HouseholdID).Error; err != nil {
		t.Fatal(err)
	}
	return h
}

func (h Household) IDString() string {
	return (User{ID: h.ID}).IDString()
}

func confirmSignup(t *testing.T, ts *testServer, b *browser) {
	t.Helper()
	if r := b.post(ts.lastVerifyLink(t), nil); r.url.Path != "/verify/done" {
		t.Fatalf("verify %d %s", r.status, r.url)
	}
}

func TestVerifiedEmailIsRequiredToInviteAndJoin(t *testing.T) {
	ts := newTestServer(t)
	guest := ts.browser(t)
	guest.get("/")
	guest.post("/start", url.Values{"name": {"Weight"}})
	h := householdOf(t, ts, guestUserID(t, ts))
	r := guest.post("/households/"+h.IDString()+"/invite", nil)
	if r.status != http.StatusForbidden {
		t.Fatalf("guest invite %d", r.status)
	}
	h = householdOf(t, ts, guestUserID(t, ts))
	if h.InviteToken != nil {
		t.Fatal("guest invite was created")
	}

	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	own := householdOf(t, ts, ts.user(t, "owner@example.com").ID)
	r = owner.post("/households/"+own.IDString()+"/invite", nil)
	if r.status != http.StatusForbidden {
		t.Fatalf("unverified invite %d", r.status)
	}
	own = householdOf(t, ts, ts.user(t, "owner@example.com").ID)
	if own.InviteToken != nil {
		t.Fatal("unverified invite was created")
	}
	confirmSignup(t, ts, owner)
	r = owner.post("/households/"+own.IDString()+"/invite", nil)
	own = householdOf(t, ts, ts.user(t, "owner@example.com").ID)
	if r.status != http.StatusOK || own.InviteToken == nil {
		t.Fatalf("verified invite %d token %v", r.status, own.InviteToken)
	}
	joiner := ts.browser(t)
	joiner.signup("join@example.com", "")
	if r := joiner.post("/join/"+*own.InviteToken, nil); r.status != http.StatusForbidden {
		t.Fatalf("unverified join %d", r.status)
	}
	if ts.role(t, own, "join@example.com") != "" {
		t.Fatal("unverified user joined")
	}
}

func TestTransferRequiresBothProofs(t *testing.T) {
	ts := newTestServer(t)
	dest := ts.browser(t)
	dest.signup("dest@example.com", "")
	confirmSignup(t, ts, dest)
	third := ts.browser(t)
	third.signup("third@example.com", "")
	confirmSignup(t, ts, third)

	guest := ts.browser(t)
	guest.get("/")
	guest.post("/start", url.Values{"name": {"Weight"}})
	guestID := guestUserID(t, ts)
	h := householdOf(t, ts, guestID)
	tr := guest.createTracker(h, "Weight", nil)
	sent := guest.post("/account/email", url.Values{"email": {"dest@example.com"}})
	if strings.Contains(sent.body, "already") || strings.Contains(sent.body, "registered") {
		t.Fatal("add-email disclosed an account")
	}
	proof := cookieValue(guest, transferCookie)
	if proof == "" {
		t.Fatal("no source proof")
	}
	link := ts.lastLoginLink(t)
	if r := guest.post("/account/transfer", url.Values{"confirm": {"move"}}); strings.Contains(r.body, "Those trackers are on this account") {
		t.Fatal("moved before the destination was verified")
	}
	var still User
	if err := ts.app.db.Take(&still, guestID).Error; err != nil || still.Email != "" {
		t.Fatal("guest changed before verification")
	}

	other := ts.browser(t)
	other.post(link, nil)
	if !other.loggedInAs("dest@example.com") {
		t.Fatal("destination link did not sign in")
	}
	page := other.get("/account/transfer")
	if strings.Contains(page.body, ">Move trackers</button>") {
		t.Fatalf("destination device can move without the source proof cookie %q", cookieValue(other, transferCookie))
	}
	other.post("/account/transfer", url.Values{"confirm": {"move"}})
	if ts.tracker(t, tr.ID).HouseholdID != h.ID {
		t.Fatal("destination link moved the tracker")
	}

	setCookie(third, transferCookie, proof)
	third.post("/account/transfer", url.Values{"confirm": {"move"}})
	if err := ts.app.db.Take(&still, guestID).Error; err != nil {
		t.Fatal("third account consumed the guest")
	}

	if !strings.Contains(guest.get("/account/transfer").body, ">Move trackers</button>") {
		t.Fatal("original browser cannot finish the move")
	}
	guest.post("/account/transfer", url.Values{"confirm": {"move"}})
	if err := ts.app.db.Take(&still, guestID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("guest row remains")
	}
	if ts.tracker(t, tr.ID).HouseholdID == h.ID {
		t.Fatal("tracker was not moved")
	}
	var n int64
	ts.app.db.Model(&Tracker{}).Where("name = ?", "Weight").Count(&n)
	if n != 1 {
		t.Fatalf("trackers %d", n)
	}
	setCookie(other, transferCookie, proof)
	other.post("/account/transfer", url.Values{"confirm": {"move"}})
	ts.app.db.Model(&Tracker{}).Where("name = ?", "Weight").Count(&n)
	if n != 1 {
		t.Fatal("retry duplicated the tracker")
	}
}

func TestSupersededClaimDoesNotAttach(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.get("/")
	b.post("/start", url.Values{"name": {"Weight"}})
	b.post("/account/email", url.Values{"email": {"first@example.com"}})
	first := ts.lastVerifyLink(t)
	b.post("/account/email", url.Values{"email": {"second@example.com"}})
	second := ts.lastVerifyLink(t)
	if r := b.post(first, nil); r.status != http.StatusGone || strings.Contains(r.body, "Logged in as first@example.com") {
		t.Fatalf("superseded %d", r.status)
	}
	var u User
	if err := ts.app.db.Where("email = ''").Take(&u).Error; err != nil {
		t.Fatal("superseded link attached an email")
	}
	b.post(second, nil)
	if err := ts.app.db.Take(&u, u.ID).Error; err != nil || u.Email != "second@example.com" {
		t.Fatalf("second claim %+v", u)
	}
}

func TestEmailRegisteredBeforeConfirmStaysAGuest(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.get("/")
	b.post("/start", url.Values{"name": {"Weight"}})
	id := guestUserID(t, ts)
	b.post("/account/email", url.Values{"email": {"race@example.com"}})
	link := ts.lastVerifyLink(t)
	other := ts.browser(t)
	other.signup("race@example.com", "")
	r := b.post(link, nil)
	if !strings.Contains(r.body, "could not be added") {
		t.Fatalf("race page %d %s", r.status, r.body)
	}
	var u User
	if err := ts.app.db.Take(&u, id).Error; err != nil || u.Email != "" {
		t.Fatalf("guest %+v err %v", u, err)
	}
}

func TestTransferRollsBackWhenItCannotFinish(t *testing.T) {
	ts := newTestServer(t)
	dest := ts.browser(t)
	dest.signup("dest@example.com", "")
	confirmSignup(t, ts, dest)
	dest.post("/households", url.Values{"name": {"Family"}})
	third := ts.browser(t)
	third.signup("third@example.com", "")
	confirmSignup(t, ts, third)

	guest := ts.browser(t)
	guest.get("/")
	guest.post("/start", url.Values{"name": {"Weight"}})
	guestID := guestUserID(t, ts)
	h := householdOf(t, ts, guestID)
	tr := guest.createTracker(h, "Weight", nil)
	guest.post("/households", url.Values{"name": {"Family"}})
	var family Household
	if err := ts.app.db.Where("name = ? AND id <> ?", "Family", h.ID).Where("id IN (?)",
		ts.app.db.Model(&HouseholdMember{}).Select("household_id").Where("user_id = ?", guestID)).Take(&family).Error; err != nil {
		t.Fatal(err)
	}
	if err := ts.app.db.Create(&HouseholdMember{HouseholdID: family.ID, UserID: ts.user(t, "third@example.com").ID, Role: roleMember, Position: 2}).Error; err != nil {
		t.Fatal(err)
	}
	guest.post("/account/email", url.Values{"email": {"dest@example.com"}})
	dest.post(ts.lastLoginLink(t), nil)
	guest.post("/account/transfer", url.Values{"confirm": {"move"}})
	var u User
	if err := ts.app.db.Take(&u, guestID).Error; err != nil {
		t.Fatal("rolled-back transfer deleted the guest")
	}
	if ts.tracker(t, tr.ID).HouseholdID != h.ID {
		t.Fatal("rolled-back transfer moved the tracker")
	}
}
