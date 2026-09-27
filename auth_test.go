package main

import (
	"context"
	"errors"
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

	"golang.org/x/crypto/bcrypt"
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

// confirmAndSetPassword signs up, confirms the mailbox from that browser, and sets a password.
func (ts *testServer) confirmAndSetPassword(t *testing.T, email, password string) *browser {
	t.Helper()
	b := ts.browser(t)
	b.signup(email, "")
	if r := b.post(ts.lastVerifyLink(t), nil); r.url.Path != "/verify/done" {
		t.Fatalf("verify: %d at %s", r.status, r.url)
	}
	if !b.loggedInAs(email) {
		t.Fatal("confirming from the signup browser logged it out")
	}
	r := b.post("/settings/password", url.Values{"password": {password}, "confirm": {password}})
	if !strings.Contains(r.body, "Password saved") {
		t.Fatalf("set password: %d", r.status)
	}
	return b
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
	return strings.Contains(b.get("/settings").body, "Logged in as "+email)
}

var (
	linkRE   = regexp.MustCompile(`http://example\.test(/login/link/[A-Za-z0-9_-]+(?:\?next=\S+)?)`)
	verifyRE = regexp.MustCompile(`http://example\.test(/verify/[A-Za-z0-9_-]+)`)
)

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

// lastVerifyLink returns the path of the most recent emailed verification link.
func (ts *testServer) lastVerifyLink(t *testing.T) string {
	t.Helper()
	msgs := ts.mailer.messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := verifyRE.FindStringSubmatch(msgs[i].Body); m != nil {
			return m[1]
		}
	}
	t.Fatal("no verification link in email")
	return ""
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

func TestAuthPagesLeadWithEmail(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)

	signup := b.get("/signup")
	for _, want := range []string{"<h1>Sign up</h1>", ">Continue</button>", "after you confirm your email", "Already have an account?"} {
		if !strings.Contains(signup.body, want) {
			t.Errorf("signup missing %q", want)
		}
	}
	if strings.Contains(signup.body, "auth-password") {
		t.Error("signup still offers a password before the email is confirmed")
	}

	login := b.get("/login")
	for _, want := range []string{"<h1>Log in</h1>", `action="/login/link"`, ">Continue</button>", "Log in with a password instead", "Don't have an account?"} {
		if !strings.Contains(login.body, want) {
			t.Errorf("login missing %q", want)
		}
	}

	bad := b.post("/login", url.Values{"email": {"nobody@example.com"}, "password": {"longenough"}})
	if bad.status != http.StatusUnauthorized || !strings.Contains(bad.body, `auth-password-toggle" checked`) {
		t.Fatalf("password login error did not keep the password form open: %d", bad.status)
	}
}

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
	if u.HasPassword() {
		t.Error("signup stored a password before the email was verified")
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
		{"empty email", "", "", "valid email"},
		{"blank email", "   ", "", "valid email"},
		{"display name", "Dog <dog@example.com>", "", "valid email"},
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
	b := ts.confirmAndSetPassword(t, "dog@example.com", "correct horse")

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
	ts.confirmAndSetPassword(t, "dog@example.com", "correct horse").post("/logout", nil)

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
	for _, tc := range []struct{ in, want string }{
		{"", "/"},
		{"/settings", "/settings"},
		{"/trackers/1?x=2", "/trackers/1?x=2"},
		{"https://evil.io", "/"},
		{"http://evil.io/path", "/"},
		{"//evil.io", "/"},
		{"///evil.io", "/"},
		{"/\\evil.io", "/"},
		{"/foo\\bar", "/"},
		{"\\\\evil.io", "/"},
		{"settings", "/"},
		{"javascript:alert(1)", "/"},
		{"/\t/evil.io", "/"},
		{"/\n/evil.io", "/"},
		{"/\r/evil.io", "/"},
		{"/foo\tbar", "/"},
		{"/ok\x7fpath", "/"},
	} {
		if got := safeNext(tc.in); got != tc.want {
			t.Errorf("safeNext(%q) = %q, want %q", tc.in, got, tc.want)
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
	last := msgs[len(msgs)-1]
	if last.To != "dog@example.com" || !strings.Contains(last.Body, "/login/link/") {
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
		a.setSessionCookie(rec, "raw-token", time.Now().Add(time.Hour))
		c := rec.Result().Cookies()[0]
		if c.Value != "raw-token" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure != wantSecure || c.Path != "/" {
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
	n := 0
	for _, msg := range ts.mailer.messages() {
		if strings.Contains(msg.Body, "/login/link/") {
			n++
		}
	}
	// Signup already sent one verification email, and the recipient cap is 3.
	if n != 2 {
		t.Fatalf("%d login emails sent, want 2", n)
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
		trust   string
		headers map[string]string
		want    string
	}{
		{"remote addr", "", nil, "192.0.2.1"},
		{"spoofed cloudflare ignored", "", map[string]string{"CF-Connecting-IP": "203.0.113.5", "X-Forwarded-For": "198.51.100.1"}, "192.0.2.1"},
		{"spoofed forwarded-for ignored", "", map[string]string{"X-Forwarded-For": "198.51.100.1, 10.0.0.1"}, "192.0.2.1"},
		{"cloudflare when configured", "CF-Connecting-IP", map[string]string{"CF-Connecting-IP": "203.0.113.5", "X-Forwarded-For": "198.51.100.1"}, "203.0.113.5"},
		{"forwarded-for ignored unless it is the trusted header", "CF-Connecting-IP", map[string]string{"X-Forwarded-For": "198.51.100.1"}, "192.0.2.1"},
		{"forwarded-for when configured", "X-Forwarded-For", map[string]string{"X-Forwarded-For": "198.51.100.1, 10.0.0.1", "CF-Connecting-IP": "203.0.113.5"}, "198.51.100.1"},
		{"invalid trusted value falls back", "CF-Connecting-IP", map[string]string{"CF-Connecting-IP": "not-an-ip"}, "192.0.2.1"},
		{"embedded control characters in a trusted header fall back", "CF-Connecting-IP", map[string]string{"CF-Connecting-IP": "203.0.113.5\r\nX-Evil: 1"}, "192.0.2.1"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil) // RemoteAddr 192.0.2.1:1234
		for k, v := range tc.headers {
			r.Header.Set(k, v)
		}
		if got := clientIP(r, tc.trust); got != tc.want {
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

	for _, tz := range []string{"Mars/Olympus_Mons", "America/Boise", ""} {
		r = b.post("/settings/timezone", url.Values{"timezone": {tz}})
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "Pick a time zone from the list") {
			t.Fatalf("%q: %d", tz, r.status)
		}
		if got := ts.user(t, "dog@example.com").TimeZone; got != "Europe/London" {
			t.Fatalf("%q changed it to %q", tz, got)
		}
	}
}

func TestSettingsTimeZoneIsAFriendlyList(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")

	r := b.get("/settings")
	if strings.Contains(r.body, `<input type="text" name="timezone"`) {
		t.Error("settings still has a raw time zone text field")
	}
	for _, want := range []string{`<select name="timezone"`, `<summary>Pacific Time (US &amp; Canada)</summary>`, `value="America/Los_Angeles" selected`} {
		if !strings.Contains(r.body, want) {
			t.Errorf("settings missing %q", want)
		}
	}
}

func TestSettingsKeepsUnlistedSignupZone(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.post("/signup", url.Values{"email": {"boise@example.com"}, "timezone": {"America/Boise"}})

	r := b.get("/settings")
	if !strings.Contains(r.body, `value="America/Boise" selected`) || !strings.Contains(r.body, "<summary>Boise</summary>") {
		t.Fatal("unlisted signup zone not shown as current")
	}
	r = b.post("/settings/timezone", url.Values{"timezone": {"America/Boise"}})
	if r.url.Path != "/settings" || !strings.Contains(r.body, "Time zone saved") {
		t.Fatalf("re-save: %d at %s", r.status, r.url.Path)
	}
	if tz := ts.user(t, "boise@example.com").TimeZone; tz != "America/Boise" {
		t.Fatalf("stored %q", tz)
	}
}

func TestSettingsSetPasswordForPasswordlessAccount(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("nopass@example.com", "")

	if r := b.post("/settings/password", url.Values{"password": {"new password"}, "confirm": {"new password"}}); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "Confirm your email") {
		t.Fatalf("password before verification: %d", r.status)
	}
	if ts.user(t, "nopass@example.com").HasPassword() {
		t.Fatal("unverified account stored a password")
	}

	if r := b.post(ts.lastVerifyLink(t), nil); r.url.Path != "/verify/done" {
		t.Fatalf("verify: %s", r.url)
	}

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
	phone := ts.confirmAndSetPassword(t, "dog@example.com", "old password")
	laptop := ts.browser(t)
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

func TestUnverifiedSignupCannotPreHijack(t *testing.T) {
	ts := newTestServer(t)
	attacker := ts.browser(t)
	attacker.signup("victim@example.com", "attacker-password")
	if !attacker.loggedInAs("victim@example.com") {
		t.Fatal("signup session missing")
	}
	if ts.user(t, "victim@example.com").HasPassword() {
		t.Fatal("signup stored a password for an unverified address")
	}
	msgs := ts.mailer.messages()
	if len(msgs) != 1 || strings.Contains(strings.ToLower(msgs[0].Body), "ignore") {
		t.Fatalf("verification email should not tell the recipient to ignore it: %+v", msgs)
	}
	if !strings.Contains(msgs[0].Body, "open the link anyway") {
		t.Fatal("verification email does not tell the mailbox owner to open the link")
	}

	// A password planted before confirmation must not survive it, and must not log in.
	hash, err := bcrypt.GenerateFromPassword([]byte("attacker-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.app.db.Model(&User{}).Where("email = ?", "victim@example.com").Update("password_hash", string(hash)).Error; err != nil {
		t.Fatal(err)
	}
	if r := ts.browser(t).login("victim@example.com", "attacker-password"); r.status != http.StatusUnauthorized {
		t.Fatalf("unverified password login: %d", r.status)
	}

	victim := ts.browser(t)
	if r := victim.post(ts.lastVerifyLink(t), nil); r.url.Path != "/verify/done" {
		t.Fatalf("verify: %s", r.url)
	}
	if victim.loggedInAs("victim@example.com") {
		t.Fatal("verification from another browser took over the session")
	}
	if attacker.loggedInAs("victim@example.com") {
		t.Fatal("attacker session survived proof of the mailbox")
	}
	u := ts.user(t, "victim@example.com")
	if u.EmailVerifiedAt == nil || u.HasPassword() {
		t.Fatalf("after verify: verified=%v password=%v", u.EmailVerifiedAt != nil, u.HasPassword())
	}
	if r := ts.browser(t).login("victim@example.com", "attacker-password"); r.status != http.StatusUnauthorized {
		t.Fatalf("old password still works: %d", r.status)
	}

	victim.post("/login/link", url.Values{"email": {"victim@example.com"}})
	if r := victim.post(ts.lastLoginLink(t), nil); !victim.loggedInAs("victim@example.com") {
		t.Fatalf("mailbox owner could not log in: %d", r.status)
	}
	if attacker.loggedInAs("victim@example.com") {
		t.Fatal("attacker regained the account")
	}
}

func TestLoginLinkClaimsAnUnverifiedAccount(t *testing.T) {
	ts := newTestServer(t)
	attacker := ts.browser(t)
	attacker.signup("victim@example.com", "attacker-password")
	hash, err := bcrypt.GenerateFromPassword([]byte("attacker-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.app.db.Model(&User{}).Where("email = ?", "victim@example.com").Update("password_hash", string(hash)).Error; err != nil {
		t.Fatal(err)
	}

	owner := ts.browser(t)
	owner.post("/login/link", url.Values{"email": {"victim@example.com"}})
	body := ts.mailer.messages()[len(ts.mailer.messages())-1].Body
	if strings.Contains(strings.ToLower(body), "ignore") || !strings.Contains(body, "open the link anyway") {
		t.Fatalf("unverified login email: %s", body)
	}
	if r := owner.post(ts.lastLoginLink(t), nil); !owner.loggedInAs("victim@example.com") {
		t.Fatalf("login link: %d", r.status)
	}
	if attacker.loggedInAs("victim@example.com") {
		t.Fatal("pre-confirmation session survived the login link")
	}
	u := ts.user(t, "victim@example.com")
	if u.EmailVerifiedAt == nil || u.HasPassword() {
		t.Fatal("login link left the pre-confirmation password in place")
	}

	// A later login link for an already-confirmed account does not clear the password.
	owner.post("/settings/password", url.Values{"password": {"correct horse"}, "confirm": {"correct horse"}})
	other := ts.browser(t)
	other.post("/login/link", url.Values{"email": {"victim@example.com"}})
	later := ts.mailer.messages()[len(ts.mailer.messages())-1].Body
	if !strings.Contains(later, "you can ignore this email") {
		t.Fatal("verified login email lost the ignore wording")
	}
	other.post(ts.lastLoginLink(t), nil)
	if !ts.user(t, "victim@example.com").HasPassword() {
		t.Fatal("login link cleared a password set after confirmation")
	}
	if !owner.loggedInAs("victim@example.com") || !other.loggedInAs("victim@example.com") {
		t.Fatal("confirming an already-verified address signed someone out")
	}
}

func TestSessionIDIsStoredHashed(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")

	u, err := url.Parse(ts.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	for _, c := range b.c.Jar.Cookies(u) {
		if c.Name == sessionCookie {
			raw = c.Value
		}
	}
	if raw == "" {
		t.Fatal("no session cookie")
	}
	var s Session
	if err := ts.app.db.Take(&s).Error; err != nil {
		t.Fatal(err)
	}
	if s.ID == raw || s.ID != hashToken(raw) || strings.Contains(s.ID, raw) {
		t.Fatalf("stored %q for cookie %q", s.ID, raw)
	}
	if !b.loggedInAs("dog@example.com") {
		t.Fatal("hashed session did not authenticate")
	}
}

func TestNewTokenFailsClosed(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = orig })

	token, err := newToken()
	if err == nil || token != "" {
		t.Fatalf("newToken() = %q, %v", token, err)
	}
}

func TestRateLimiterCapsKeys(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	l := newRateLimiter(2, time.Minute, clock.now)
	l.maxKeys = 2
	l.sweepEvery = 1000
	if !l.allow("a") || !l.allow("b") {
		t.Fatal("keys under the cap should be allowed")
	}
	if l.allow("c") {
		t.Fatal("new key admitted past the cap")
	}
	if !l.allow("a") {
		t.Fatal("an existing key should still count inside the window")
	}
	if l.allow("a") {
		t.Fatal("existing key exceeded its limit")
	}
	clock.advance(time.Minute)
	l.sweepEvery = 1
	l.calls = 0
	if !l.allow("c") {
		t.Fatal("expired keys were not dropped, so a new key stayed refused")
	}
}

func TestSpoofedForwardingHeadersDoNotResetLoginLimits(t *testing.T) {
	ts := newTestServer(t)
	for i := range 10 {
		req, err := http.NewRequest(http.MethodPost, ts.srv.URL+"/login", strings.NewReader("email=dog@example.com&password=guess"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("CF-Connecting-IP", "203.0.113."+string(rune('1'+i)))
		req.Header.Set("X-Forwarded-For", "198.51.100."+string(rune('1'+i)))
		resp, err := ts.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, resp.StatusCode)
		}
	}
	req, err := http.NewRequest(http.MethodPost, ts.srv.URL+"/login", strings.NewReader("email=dog@example.com&password=guess"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("CF-Connecting-IP", "203.0.113.200")
	req.Header.Set("X-Forwarded-For", "198.51.100.200")
	resp, err := ts.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("spoofed headers reset the limit: %d", resp.StatusCode)
	}
}

func TestRecipientMailLimitHidesWhetherTheAccountExists(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	ts.app.linkLimiter = newRateLimiter(100, 15*time.Minute, func() time.Time { return ts.app.now() })
	b := ts.browser(t)
	var last response
	for range 5 {
		last = b.post("/login/link", url.Values{"email": {"dog@example.com"}})
	}
	if last.status != http.StatusOK || !strings.Contains(last.body, "Check your email") {
		t.Fatalf("throttled known address: %d", last.status)
	}
	n := 0
	for _, msg := range ts.mailer.messages() {
		if strings.Contains(msg.Body, "/login/link/") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d login emails, want 2 after the signup verification", n)
	}
	unknown := b.post("/login/link", url.Values{"email": {"nobody@example.com"}})
	if unknown.status != http.StatusOK || !strings.Contains(unknown.body, "Check your email") {
		t.Fatalf("unknown address: %d", unknown.status)
	}
}

func TestExpiredAuthRecordsAreDeleted(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")
	b.post("/login/link", url.Values{"email": {"dog@example.com"}})
	b.post(ts.lastLoginLink(t), nil)

	ts.app.deleteExpiredAuth()
	var loginTokens int64
	ts.app.db.Model(&LoginToken{}).Count(&loginTokens)
	if loginTokens != 0 {
		t.Fatal("used login token was kept")
	}
	var sessions int64
	ts.app.db.Model(&Session{}).Count(&sessions)
	if sessions == 0 {
		t.Fatal("live session was deleted")
	}

	ts.clock.advance(sessionLifetime + time.Hour)
	ts.app.deleteExpiredAuth()
	ts.app.db.Model(&Session{}).Count(&sessions)
	var verifyTokens int64
	ts.app.db.Model(&VerificationToken{}).Count(&verifyTokens)
	if sessions != 0 || verifyTokens != 0 {
		t.Fatalf("sessions %d verification tokens %d", sessions, verifyTokens)
	}
}

func TestCleanupAuthStopsWhenCancelled(t *testing.T) {
	ts := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ts.app.cleanupAuth(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not stop")
	}
}

func TestPasswordLoginButtonSubmitsThePasswordForm(t *testing.T) {
	ts := newTestServer(t)
	page := ts.browser(t).get("/login").body
	link := strings.Index(page, `class="auth-link-submit"`)
	password := strings.Index(page, `class="auth-password-submit"`)
	if link < 0 || password < 0 || password < link {
		t.Fatal("email mode should submit the magic-link button first")
	}
	bad := ts.browser(t).post("/login", url.Values{"email": {"nobody@example.com"}, "password": {"longenough"}}).body
	if !strings.Contains(bad, `action="/login"`) {
		t.Fatal("password mode form does not post to /login")
	}
	link = strings.Index(bad, `class="auth-link-submit"`)
	password = strings.Index(bad, `class="auth-password-submit"`)
	if link < 0 || password < 0 || password > link {
		t.Fatal("password mode should submit the password button first")
	}
	script := ts.browser(t).get("/static/app.js").body
	if !strings.Contains(script, `form.action = passwordMode ? "/login" : "/login/link"`) {
		t.Fatal("app.js does not switch the login form action with the password toggle")
	}
	if !strings.Contains(script, "htmx:responseError") || !strings.Contains(script, "That wasn't saved") {
		t.Fatal("app.js does not restore a failed optimistic log")
	}
}
