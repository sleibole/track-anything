package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock lets a test move time forward, e.g. past a session's expiry. It starts at the
// real time so the cookie jar, which uses the real clock, doesn't drop cookies as expired.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// testServer is an app behind a real HTTP server, so tests can use cookie jars.
type testServer struct {
	app    *app
	srv    *httptest.Server
	clock  *fakeClock
	mailer *memMailer
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	a := newTestApp(t)
	clock := &fakeClock{t: time.Now()}
	a.now = clock.now
	srv := httptest.NewServer(a.routes())
	t.Cleanup(srv.Close)
	return &testServer{app: a, srv: srv, clock: clock, mailer: a.mailer.(*memMailer)}
}

// browser is one cookie jar, like one device.
type browser struct {
	t  *testing.T
	ts *testServer
	c  *http.Client
}

func (ts *testServer) browser(t *testing.T) *browser {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &browser{t: t, ts: ts, c: &http.Client{Jar: jar}}
}

type response struct {
	status int
	body   string
	url    *url.URL // after redirects
}

func (b *browser) do(req *http.Request) response {
	b.t.Helper()
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	return response{status: resp.StatusCode, body: string(body), url: resp.Request.URL}
}

func (b *browser) get(path string) response {
	b.t.Helper()
	req, err := http.NewRequest(http.MethodGet, b.ts.srv.URL+path, nil)
	if err != nil {
		b.t.Fatal(err)
	}
	return b.do(req)
}

func (b *browser) post(path string, form url.Values) response {
	b.t.Helper()
	req, err := http.NewRequest(http.MethodPost, b.ts.srv.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return b.do(req)
}

func (b *browser) signup(email, password string) response {
	b.t.Helper()
	return b.post("/signup", url.Values{"email": {email}, "password": {password}, "timezone": {"America/Los_Angeles"}})
}

func (b *browser) login(email, password string) response {
	b.t.Helper()
	return b.post("/login", url.Values{"email": {email}, "password": {password}})
}

func (b *browser) loggedInAs(email string) bool {
	b.t.Helper()
	return strings.Contains(b.get("/").body, "Logged in as "+email)
}

var linkRE = regexp.MustCompile(`http://example\.test(/login/link/[A-Za-z0-9_-]+(?:\?next=\S+)?)`)

// lastLoginLink returns the path of the most recent emailed login link.
func (ts *testServer) lastLoginLink(t *testing.T) string {
	t.Helper()
	msgs := ts.mailer.messages()
	if len(msgs) == 0 {
		t.Fatal("no email sent")
	}
	m := linkRE.FindStringSubmatch(msgs[len(msgs)-1].Body)
	if m == nil {
		t.Fatalf("no login link in email body: %q", msgs[len(msgs)-1].Body)
	}
	return m[1]
}

func (ts *testServer) user(t *testing.T, email string) User {
	t.Helper()
	var u User
	if err := ts.app.db.Take(&u, "email = ?", email).Error; err != nil {
		t.Fatalf("load user %s: %v", email, err)
	}
	return u
}

// Signup

func TestSignupLogsIn(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)

	r := b.signup("Dog@Example.COM ", "correct horse")
	if r.status != http.StatusOK || r.url.Path != "/" {
		t.Fatalf("signup: %d at %s", r.status, r.url.Path)
	}
	if !b.loggedInAs("dog@example.com") {
		t.Fatal("not logged in after signup")
	}

	u := ts.user(t, "dog@example.com")
	if u.TimeZone != "America/Los_Angeles" {
		t.Errorf("time zone %q", u.TimeZone)
	}
	if u.PasswordHash == "" || u.PasswordHash == "correct horse" {
		t.Errorf("password not hashed: %q", u.PasswordHash)
	}
}

func TestSignupWithoutPassword(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)

	b.signup("nopass@example.com", "")
	if !b.loggedInAs("nopass@example.com") {
		t.Fatal("not logged in after signup")
	}
	if u := ts.user(t, "nopass@example.com"); u.HasPassword() {
		t.Error("expected no password")
	}
}

func TestSignupValidation(t *testing.T) {
	ts := newTestServer(t)
	for _, tc := range []struct{ name, email, password, want string }{
		{"bad email", "not-an-email", "", "valid email"},
		{"display name", "Dog <dog@example.com>", "", "valid email"},
		{"short password", "a@example.com", "short", "at least 8"},
		{"long password", "b@example.com", strings.Repeat("x", 73), "at most 72"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ts.browser(t).signup(tc.email, tc.password)
			if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, tc.want) {
				t.Fatalf("got %d, body missing %q", r.status, tc.want)
			}
		})
	}
	var count int64
	ts.app.db.Model(&User{}).Count(&count)
	if count != 0 {
		t.Fatalf("%d users created from invalid signups", count)
	}
}

func TestSignupDuplicateEmailIgnoresCase(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")

	r := ts.browser(t).signup("DOG@example.com", "")
	if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "already exists") {
		t.Fatalf("got %d", r.status)
	}
}

func TestSignupInvalidTimeZoneFallsBackToUTC(t *testing.T) {
	ts := newTestServer(t)
	for email, tz := range map[string]string{"a@example.com": "Nowhere/Land", "b@example.com": "Local", "c@example.com": ""} {
		ts.browser(t).post("/signup", url.Values{"email": {email}, "timezone": {tz}})
		if got := ts.user(t, email).TimeZone; got != "UTC" {
			t.Errorf("timezone %q: stored %q, want UTC", tz, got)
		}
	}
}

func TestSignupAndLoginPagesRedirectWhenLoggedIn(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")
	for _, path := range []string{"/signup", "/login"} {
		if r := b.get(path); r.url.Path != "/" {
			t.Errorf("%s: ended at %s", path, r.url.Path)
		}
	}
}

// Password login and logout

func TestLogoutThenPasswordLogin(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "correct horse")

	b.post("/logout", nil)
	if b.loggedInAs("dog@example.com") {
		t.Fatal("still logged in after logout")
	}
	var sessions int64
	ts.app.db.Model(&Session{}).Count(&sessions)
	if sessions != 0 {
		t.Errorf("%d sessions left after logout", sessions)
	}

	if r := b.login("dog@example.com", "wrong password"); r.status != http.StatusUnauthorized || !strings.Contains(r.body, "incorrect") {
		t.Fatalf("wrong password: %d", r.status)
	}
	if b.loggedInAs("dog@example.com") {
		t.Fatal("logged in with the wrong password")
	}

	b.login("DOG@Example.com", "correct horse")
	if !b.loggedInAs("dog@example.com") {
		t.Fatal("password login failed")
	}
}

func TestPasswordLoginFailsForUnknownAndPasswordlessAccounts(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("nopass@example.com", "")

	for _, email := range []string{"nobody@example.com", "nopass@example.com"} {
		b := ts.browser(t)
		if r := b.login(email, ""); r.status != http.StatusUnauthorized {
			t.Errorf("%s: status %d", email, r.status)
		}
		if b.loggedInAs(email) {
			t.Errorf("%s: logged in", email)
		}
	}
}

func TestLoginRedirectsBackToNext(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "correct horse")

	b := ts.browser(t)
	r := b.get("/settings")
	if r.url.Path != "/login" || r.url.Query().Get("next") != "/settings" {
		t.Fatalf("logged-out /settings ended at %s", r.url)
	}

	r = b.post("/login", url.Values{"email": {"dog@example.com"}, "password": {"correct horse"}, "next": {"/settings"}})
	if r.url.Path != "/settings" {
		t.Fatalf("after login ended at %s", r.url.Path)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"":                "/",
		"/settings":       "/settings",
		"/trackers/1?x=2": "/trackers/1?x=2",
		"https://evil.io": "/",
		"//evil.io":       "/",
		"/\\evil.io":      "/",
		"settings":        "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecondAccountIsIndependent(t *testing.T) {
	ts := newTestServer(t)
	one, two := ts.browser(t), ts.browser(t)
	one.signup("one@example.com", "")
	two.signup("two@example.com", "")

	if !one.loggedInAs("one@example.com") || !two.loggedInAs("two@example.com") {
		t.Fatal("each browser should see its own account")
	}
	one.post("/logout", nil)
	if !two.loggedInAs("two@example.com") {
		t.Fatal("logging out one account logged out the other")
	}
}

// Magic links

func TestMagicLinkLogsInOnce(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")

	b := ts.browser(t)
	r := b.post("/login/link", url.Values{"email": {"Dog@Example.com"}})
	if r.status != http.StatusOK || !strings.Contains(r.body, "Check your email") {
		t.Fatalf("request link: %d", r.status)
	}
	msgs := ts.mailer.messages()
	if len(msgs) != 1 || msgs[0].To != "dog@example.com" {
		t.Fatalf("emails: %+v", msgs)
	}
	link := ts.lastLoginLink(t)

	// Opening the link doesn't log in by itself (email scanners open links too).
	r = b.get(link)
	if r.status != http.StatusOK || !strings.Contains(r.body, "Log in as dog@example.com") {
		t.Fatalf("confirm page: %d", r.status)
	}
	if b.loggedInAs("dog@example.com") {
		t.Fatal("GET of the link logged in")
	}

	b.post(link, nil)
	if !b.loggedInAs("dog@example.com") {
		t.Fatal("magic link did not log in")
	}

	other := ts.browser(t)
	if r := other.post(link, nil); r.status != http.StatusGone {
		t.Fatalf("reused link: status %d", r.status)
	}
	if other.loggedInAs("dog@example.com") {
		t.Fatal("reused link logged in")
	}
	if r := other.get(link); r.status != http.StatusGone || !strings.Contains(r.body, "expired") {
		t.Fatalf("used link page: status %d", r.status)
	}
}

func TestMagicLinkStoresOnlyAHash(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	ts.browser(t).post("/login/link", url.Values{"email": {"dog@example.com"}})

	token := strings.TrimPrefix(ts.lastLoginLink(t), "/login/link/")
	var lt LoginToken
	if err := ts.app.db.Take(&lt).Error; err != nil {
		t.Fatal(err)
	}
	if lt.TokenHash == token || lt.TokenHash != hashToken(token) {
		t.Fatalf("stored %q for token %q", lt.TokenHash, token)
	}
}

func TestMagicLinkExpires(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	b := ts.browser(t)
	b.post("/login/link", url.Values{"email": {"dog@example.com"}})
	link := ts.lastLoginLink(t)

	ts.clock.advance(16 * time.Minute)
	if r := b.post(link, nil); r.status != http.StatusGone {
		t.Fatalf("expired link: status %d", r.status)
	}
	if b.loggedInAs("dog@example.com") {
		t.Fatal("expired link logged in")
	}
}

func TestMagicLinkKeepsNext(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	b := ts.browser(t)
	b.post("/login/link", url.Values{"email": {"dog@example.com"}, "next": {"/settings"}})

	link := ts.lastLoginLink(t)
	confirm := b.get(link)
	if !strings.Contains(confirm.body, `value="/settings"`) {
		t.Fatal("confirm page lost next")
	}
	path, _, _ := strings.Cut(link, "?")
	if r := b.post(path, url.Values{"next": {"/settings"}}); r.url.Path != "/settings" {
		t.Fatalf("ended at %s", r.url.Path)
	}
}

func TestMagicLinkForUnknownEmailSendsNothing(t *testing.T) {
	ts := newTestServer(t)
	r := ts.browser(t).post("/login/link", url.Values{"email": {"nobody@example.com"}})
	if r.status != http.StatusOK || !strings.Contains(r.body, "Check your email") {
		t.Fatalf("status %d", r.status)
	}
	if n := len(ts.mailer.messages()); n != 0 {
		t.Fatalf("%d emails sent for an unknown address", n)
	}
}

// Sessions

func TestSessionExpires(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")

	ts.clock.advance(sessionLifetime + time.Minute)
	if r := b.get("/settings"); r.url.Path != "/login" {
		t.Fatalf("expired session reached %s", r.url.Path)
	}
	var sessions int64
	ts.app.db.Model(&Session{}).Count(&sessions)
	if sessions != 0 {
		t.Errorf("expired session not deleted")
	}
}

func TestSessionRenewsWhenUsed(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")

	ts.clock.advance(20 * 24 * time.Hour)
	b.get("/")

	var s Session
	ts.app.db.Take(&s)
	if want := ts.clock.now().Add(sessionLifetime); !s.ExpiresAt.Equal(want) {
		t.Fatalf("expires %v, want %v", s.ExpiresAt, want)
	}

	ts.clock.advance(20 * 24 * time.Hour) // 40 days after signup, 20 after renewal
	if !b.loggedInAs("dog@example.com") {
		t.Fatal("renewed session expired")
	}
}

func TestSessionCookieFlags(t *testing.T) {
	a := newTestApp(t)
	for env, wantSecure := range map[string]bool{"dev": false, "prod": true} {
		a.cfg.Env = env
		rec := httptest.NewRecorder()
		a.setSessionCookie(rec, Session{ID: "x", ExpiresAt: time.Now().Add(time.Hour)})
		c := rec.Result().Cookies()[0]
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure != wantSecure || c.Path != "/" {
			t.Errorf("%s: cookie %+v", env, c)
		}
	}
}

// Rate limits and CSRF

func TestLoginRateLimit(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	for i := range 10 {
		if r := b.login("dog@example.com", "guess"); r.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d", i+1, r.status)
		}
	}
	if r := b.login("dog@example.com", "guess"); r.status != http.StatusTooManyRequests {
		t.Fatalf("11th attempt: status %d", r.status)
	}
	ts.clock.advance(time.Minute)
	if r := b.login("dog@example.com", "guess"); r.status != http.StatusUnauthorized {
		t.Fatalf("after the window: status %d", r.status)
	}
}

func TestMagicLinkRateLimit(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	b := ts.browser(t)
	for range 5 {
		b.post("/login/link", url.Values{"email": {"dog@example.com"}})
	}
	if r := b.post("/login/link", url.Values{"email": {"dog@example.com"}}); r.status != http.StatusTooManyRequests {
		t.Fatalf("6th request: status %d", r.status)
	}
	if n := len(ts.mailer.messages()); n != 5 {
		t.Fatalf("%d emails sent, want 5", n)
	}
}

func TestSignupRateLimit(t *testing.T) {
	ts := newTestServer(t)
	for i := range 10 {
		ts.browser(t).signup("user"+string(rune('a'+i))+"@example.com", "")
	}
	if r := ts.browser(t).signup("one-too-many@example.com", ""); r.status != http.StatusTooManyRequests {
		t.Fatalf("11th signup: status %d", r.status)
	}
}

func TestRateLimiterWindowsAndKeys(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	l := newRateLimiter(2, time.Minute, clock.now)
	if !l.allow("a") || !l.allow("a") || l.allow("a") {
		t.Fatal("limit of 2 not enforced")
	}
	if !l.allow("b") {
		t.Fatal("keys should be independent")
	}
	clock.advance(time.Minute)
	if !l.allow("a") {
		t.Fatal("window did not reset")
	}
}

func TestClientIP(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"remote addr", nil, "192.0.2.1"},
		{"cloudflare", map[string]string{"CF-Connecting-IP": "203.0.113.5", "X-Forwarded-For": "198.51.100.1"}, "203.0.113.5"},
		{"forwarded for", map[string]string{"X-Forwarded-For": "198.51.100.1, 10.0.0.1"}, "198.51.100.1"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil) // RemoteAddr 192.0.2.1:1234
		for k, v := range tc.headers {
			r.Header.Set(k, v)
		}
		if got := clientIP(r); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCrossOriginPostsAreRejected(t *testing.T) {
	h := newTestApp(t).routes()
	for name, headers := range map[string]map[string]string{
		"sec-fetch-site": {"Sec-Fetch-Site": "cross-site"},
		"origin":         {"Origin": "https://evil.example"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("email=a%40b.c&password=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
}

// Settings

func TestSettingsRequiresLogin(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	for _, path := range []string{"/settings/timezone", "/settings/password"} {
		if r := b.post(path, url.Values{"timezone": {"UTC"}}); r.url.Path != "/login" {
			t.Errorf("POST %s logged out ended at %s", path, r.url.Path)
		}
	}
}

func TestSettingsTimeZone(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")

	r := b.post("/settings/timezone", url.Values{"timezone": {"Europe/London"}})
	if r.url.Path != "/settings" || !strings.Contains(r.body, "Time zone saved") {
		t.Fatalf("save: %d at %s", r.status, r.url.Path)
	}
	if tz := ts.user(t, "dog@example.com").TimeZone; tz != "Europe/London" {
		t.Fatalf("stored %q", tz)
	}

	r = b.post("/settings/timezone", url.Values{"timezone": {"Mars/Olympus_Mons"}})
	if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "Unknown time zone") {
		t.Fatalf("invalid zone: %d", r.status)
	}
	if tz := ts.user(t, "dog@example.com").TimeZone; tz != "Europe/London" {
		t.Fatalf("invalid zone changed it to %q", tz)
	}
}

func TestSettingsSetPasswordForPasswordlessAccount(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("nopass@example.com", "")

	if r := b.post("/settings/password", url.Values{"password": {"new password"}, "confirm": {"different"}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("mismatch: %d", r.status)
	}
	if r := b.post("/settings/password", url.Values{"password": {"short"}, "confirm": {"short"}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("short: %d", r.status)
	}
	if ts.user(t, "nopass@example.com").HasPassword() {
		t.Fatal("invalid attempts set a password")
	}

	b.post("/settings/password", url.Values{"password": {"new password"}, "confirm": {"new password"}})
	other := ts.browser(t)
	other.login("nopass@example.com", "new password")
	if !other.loggedInAs("nopass@example.com") {
		t.Fatal("can't log in with the new password")
	}
}

func TestChangingPasswordLogsOutOtherDevices(t *testing.T) {
	ts := newTestServer(t)
	phone, laptop := ts.browser(t), ts.browser(t)
	phone.signup("dog@example.com", "old password")
	laptop.login("dog@example.com", "old password")

	r := phone.post("/settings/password", url.Values{"password": {"new password"}, "confirm": {"new password"}})
	if !strings.Contains(r.body, "Password saved") {
		t.Fatalf("change: %d at %s", r.status, r.url.Path)
	}

	if !phone.loggedInAs("dog@example.com") {
		t.Error("the device that changed the password was logged out")
	}
	if laptop.loggedInAs("dog@example.com") {
		t.Error("other device still logged in")
	}

	b := ts.browser(t)
	if r := b.login("dog@example.com", "old password"); r.status != http.StatusUnauthorized {
		t.Errorf("old password: %d", r.status)
	}
	b.login("dog@example.com", "new password")
	if !b.loggedInAs("dog@example.com") {
		t.Error("new password doesn't work")
	}
}
