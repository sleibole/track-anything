package main

import (
	"bytes"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func setAnonymousCSRF(t *testing.T, req *http.Request) {
	t.Helper()
	token, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: token, Path: "/"})
	req.Header.Set(csrfHeader, token)
}

func sessionSecret(t *testing.T, b *browser) string {
	t.Helper()
	raw := cookieValue(b, sessionCookie)
	if raw == "" {
		t.Fatal("no session cookie")
	}
	var s Session
	if err := b.ts.app.db.Take(&s, "id = ?", hashToken(raw)).Error; err != nil {
		t.Fatal(err)
	}
	if !validCSRFToken(s.CSRFSecret) {
		t.Fatalf("secret %q", s.CSRFSecret)
	}
	return s.CSRFSecret
}

func postRaw(t *testing.T, rawURL, body, contentType string, cookies []*http.Cookie, header map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(got)
}

func TestCSRFRejectsMissingInvalidAndMalformedTokens(t *testing.T) {
	h := newTestApp(t).routes()
	form := "email=new%40example.com&timezone=UTC&" + turnstileField + "=test-token"
	valid, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	other, err := newToken()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		cookie string
		header string
		body   string
	}{
		{"missing", "", "", form},
		{"wrong", valid, other, form},
		{"malformed", valid, "%%%", form},
		{"short", valid, "abc", form},
		{"empty header with cookie", valid, "", form},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: csrfCookie, Value: tc.cookie})
			}
			if tc.header != "" {
				req.Header.Set(csrfHeader, tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden || strings.TrimSpace(rec.Body.String()) != "Forbidden" {
				t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), tc.header) && tc.header != "" {
				t.Fatalf("body echoed the token: %s", rec.Body.String())
			}
		})
	}
}

func TestCSRFRejectionDoesNotRunTheHandler(t *testing.T) {
	ts := newTestServer(t)
	code, body := postRaw(t, ts.srv.URL+"/signup", "email=skip%40example.com&timezone=UTC&"+turnstileField+"=test-token", "application/x-www-form-urlencoded", nil, nil)
	if code != http.StatusForbidden || strings.TrimSpace(body) != "Forbidden" {
		t.Fatalf("status %d body %q", code, body)
	}
	if userCount(t, ts) != 0 {
		t.Fatal("signup ran")
	}
	if len(ts.mailer.messages()) != 0 {
		t.Fatal("mailer ran")
	}
}

func TestCSRFAcceptsTheFieldOrTheHeader(t *testing.T) {
	h := newTestApp(t).routes()
	for _, useHeader := range []bool{false, true} {
		token, err := newToken()
		if err != nil {
			t.Fatal(err)
		}
		email := "header@example.com"
		if !useHeader {
			email = "field@example.com"
		}
		body := "email=" + url.QueryEscape(email) + "&timezone=UTC&" + turnstileField + "=test-token"
		if !useHeader {
			body += "&" + csrfFieldName + "=" + token
		}
		req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: csrfCookie, Value: token})
		if useHeader {
			req.Header.Set(csrfHeader, token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("header %v: status %d body %s", useHeader, rec.Code, rec.Body.String())
		}
	}
}

func TestCSRFSafeMethodsDoNotNeedAToken(t *testing.T) {
	h := newTestApp(t).routes()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "/login", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("%s was forbidden", method)
		}
	}
}

func TestCSRFUnsafeMethodsRequireAToken(t *testing.T) {
	h := newTestApp(t).routes()
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete, "TRACE"} {
		req := httptest.NewRequest(method, "/login", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s without a token: %d", method, rec.Code)
		}
		token, err := newToken()
		if err != nil {
			t.Fatal(err)
		}
		ok := httptest.NewRequest(method, "/login", nil)
		ok.AddCookie(&http.Cookie{Name: csrfCookie, Value: token})
		ok.Header.Set(csrfHeader, token)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, ok)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("%s with a token was still forbidden", method)
		}
	}
}

func TestCSRFTokenDoesNotBypassTheOriginCheck(t *testing.T) {
	a := newTestApp(t)
	h := a.routes()
	token, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, headers := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "cross-site"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader("email=evil%40example.com&timezone=UTC&"+turnstileField+"=test-token"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: csrfCookie, Value: token})
		req.Header.Set(csrfHeader, token)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%v: %d", headers, rec.Code)
		}
	}
	var n int64
	if err := a.db.Model(&User{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("cross-origin signup created an account")
	}
}

func TestCSRFSameOriginStillRequiresAToken(t *testing.T) {
	h := newTestApp(t).routes()
	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader("email=same%40example.com&timezone=UTC&"+turnstileField+"=test-token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("same-origin without a token: %d", rec.Code)
	}
}

func TestCSRFJSONDoesNotBypassTheCheck(t *testing.T) {
	ts := newTestServer(t)
	b := ts.confirmAndSetPassword(t, "json@example.com", "longenough")
	secret := sessionSecret(t, b)
	raw := cookieValue(b, sessionCookie)
	cookie := &http.Cookie{Name: sessionCookie, Value: raw}
	code, body := postRaw(t, ts.srv.URL+"/settings/timezone", `{"timezone":"UTC"}`, "application/json", []*http.Cookie{cookie}, nil)
	if code != http.StatusForbidden || strings.Contains(body, "Time zone") {
		t.Fatalf("json without a token: %d %s", code, body)
	}
	code, body = postRaw(t, ts.srv.URL+"/settings/timezone", `{"timezone":"UTC"}`, "application/json", []*http.Cookie{cookie}, map[string]string{csrfHeader: secret})
	if code == http.StatusForbidden {
		t.Fatalf("json with a token was rejected as csrf: %s", body)
	}
	if tz := ts.user(t, "json@example.com").TimeZone; tz == "UTC" {
		t.Fatal("json body was applied")
	}
}

func TestCSRFRejectsATokenFromAnotherSession(t *testing.T) {
	ts := newTestServer(t)
	a := ts.confirmAndSetPassword(t, "a@example.com", "longenough")
	b := ts.confirmAndSetPassword(t, "b@example.com", "longenough")
	code, _ := postRaw(t, ts.srv.URL+"/settings/timezone", "timezone=Europe/London", "application/x-www-form-urlencoded",
		[]*http.Cookie{{Name: sessionCookie, Value: cookieValue(b, sessionCookie)}},
		map[string]string{csrfHeader: sessionSecret(t, a)})
	if code != http.StatusForbidden {
		t.Fatalf("status %d", code)
	}
	if tz := ts.user(t, "b@example.com").TimeZone; tz == "Europe/London" {
		t.Fatal("cross-session token changed the zone")
	}
}

func TestCSRFTokenIsStableAcrossRepeats(t *testing.T) {
	ts := newTestServer(t)
	b := ts.confirmAndSetPassword(t, "tabs@example.com", "longenough")
	h := ts.personalHousehold(t, "tabs@example.com")
	tr := b.createTracker(h, "Meals", nil)
	secret := sessionSecret(t, b)
	cookie := &http.Cookie{Name: sessionCookie, Value: cookieValue(b, sessionCookie)}
	quick := ts.srv.URL + "/trackers/" + strconv.FormatUint(uint64(tr.ID), 10) + "/quick"
	for i := range 2 {
		code, body := postRaw(t, quick, "", "application/x-www-form-urlencoded", []*http.Cookie{cookie}, map[string]string{csrfHeader: secret})
		if code != http.StatusSeeOther && code != http.StatusOK {
			t.Fatalf("post %d: %d %s", i+1, code, body)
		}
	}
	if got := sessionSecret(t, b); got != secret {
		t.Fatal("token was consumed or rotated")
	}
	if len(ts.entries(t, tr.ID)) != 2 {
		t.Fatalf("entries %d", len(ts.entries(t, tr.ID)))
	}
}

func TestCSRFSessionLifecycle(t *testing.T) {
	ts := newTestServer(t)
	b := ts.confirmAndSetPassword(t, "life@example.com", "longenough")
	oldSecret := sessionSecret(t, b)
	oldCookie := cookieValue(b, sessionCookie)
	if r := b.post("/logout", nil); r.url.Path != "/login" && !strings.Contains(r.body, "Log in") {
		t.Fatalf("logout: %d %s", r.status, r.url)
	}
	code, _ := postRaw(t, ts.srv.URL+"/settings/timezone", "timezone=UTC", "application/x-www-form-urlencoded",
		[]*http.Cookie{{Name: sessionCookie, Value: oldCookie}},
		map[string]string{csrfHeader: oldSecret})
	if code != http.StatusForbidden {
		t.Fatalf("logged-out session token: %d", code)
	}

	page := b.get("/login")
	meta := csrfMeta(t, page.body)
	cookie := cookieValue(b, csrfCookie)
	if meta == "" || meta != cookie {
		t.Fatalf("login form token %q cookie %q", meta, cookie)
	}
	if meta == oldSecret {
		t.Fatal("logged-out page reused the session secret")
	}
	code, _ = postRaw(t, ts.srv.URL+"/login", "email=life@example.com&password=longenough&"+turnstileField+"=test-token&"+csrfFieldName+"="+meta,
		"application/x-www-form-urlencoded", []*http.Cookie{{Name: csrfCookie, Value: cookie}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("login: %d", code)
	}
	// The test browser did not follow that raw login, so sign in through the client
	// and show that the new secret is not the anonymous one.
	if r := b.login("life@example.com", "longenough"); !strings.Contains(r.body, "Time zone") && r.url.Path == "/login" {
		t.Fatalf("browser login: %d %s", r.status, r.url)
	}
	if sessionSecret(t, b) == meta {
		t.Fatal("login kept the anonymous token")
	}

	secret := sessionSecret(t, b)
	raw := cookieValue(b, sessionCookie)
	ts.clock.advance(sessionLifetime + time.Hour)
	code, _ = postRaw(t, ts.srv.URL+"/settings/timezone", "timezone=Europe/London", "application/x-www-form-urlencoded",
		[]*http.Cookie{{Name: sessionCookie, Value: raw}},
		map[string]string{csrfHeader: secret})
	if code != http.StatusForbidden {
		t.Fatalf("expired session: %d", code)
	}
	if tz := ts.user(t, "life@example.com").TimeZone; tz == "Europe/London" {
		t.Fatal("expired session changed the zone")
	}
}

func TestCSRFSharePostsRequireAToken(t *testing.T) {
	ts, _, tr, link := shareLink(t)
	code, _ := postRaw(t, ts.srv.URL+link+"/quick", "", "application/x-www-form-urlencoded", nil, nil)
	if code != http.StatusForbidden || len(ts.entries(t, tr.ID)) != 0 {
		t.Fatalf("share without a token: %d entries %d", code, len(ts.entries(t, tr.ID)))
	}
	token, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	code, _ = postRaw(t, ts.srv.URL+link+"/quick", "", "application/x-www-form-urlencoded",
		[]*http.Cookie{{Name: csrfCookie, Value: token}}, map[string]string{csrfHeader: token})
	if code != http.StatusSeeOther && code != http.StatusOK {
		t.Fatalf("share with a token: %d", code)
	}
	if len(ts.entries(t, tr.ID)) != 1 {
		t.Fatalf("entries %d", len(ts.entries(t, tr.ID)))
	}
}

func TestCSRFFieldEscapesAndReadsTheToken(t *testing.T) {
	got := string(csrfField(`"><script>`))
	if strings.Contains(got, "<script>") || strings.Contains(got, `value=""><script>`) {
		t.Fatalf("unescaped: %s", got)
	}
	if !strings.Contains(got, "&#34;") || !strings.Contains(got, "&lt;script&gt;") {
		t.Fatalf("escaped form: %s", got)
	}
	if !strings.Contains(got, `name="csrf_token"`) {
		t.Fatalf("field: %s", got)
	}
	page := string(csrfField(view{CSRFToken: "abc"}))
	if !strings.Contains(page, `value="abc"`) {
		t.Fatalf("view: %s", page)
	}
	card := string(csrfField(trackerCard{CSRFToken: "card-token"}))
	if !strings.Contains(card, `value="card-token"`) {
		t.Fatalf("card: %s", card)
	}
}

func TestPostFormsUseTheSharedCSRFTemplate(t *testing.T) {
	var files []string
	for _, pattern := range []string{"templates/*.html", "templates/partials/*.html"} {
		matches, err := fs.Glob(embedded, pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no templates")
	}
	var forms int
	for _, name := range files {
		body, err := fs.ReadFile(embedded, name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `name="csrf_token"`) {
			t.Errorf("%s writes the input by hand", name)
		}
		rest := string(body)
		for {
			i := strings.Index(strings.ToLower(rest), "<form")
			if i < 0 {
				break
			}
			rest = rest[i:]
			end := strings.Index(rest, ">")
			closeAt := strings.Index(strings.ToLower(rest), "</form>")
			if end < 0 || closeAt < 0 {
				t.Fatalf("%s: unclosed form", name)
			}
			open := strings.ToLower(rest[:end])
			if strings.Contains(open, `method="post"`) || strings.Contains(open, `method='post'`) {
				forms++
				if !strings.Contains(rest[:closeAt], "csrf-field") {
					t.Errorf("%s missing csrf-field:\n%s", name, rest[:end])
				}
			}
			rest = rest[closeAt+len("</form>"):]
		}
	}
	if forms < 10 {
		t.Fatalf("only found %d post forms", forms)
	}
}

func TestCSRFPagesRenderOneToken(t *testing.T) {
	ts := newTestServer(t)
	anon := ts.browser(t)
	home := anon.get("/")
	meta := csrfMeta(t, home.body)
	fields := csrfFields(t, home.body)
	if meta == "" || len(fields) == 0 || fields[0] != meta {
		t.Fatalf("home meta %q fields %v", meta, fields)
	}
	if cookieValue(anon, csrfCookie) != meta {
		t.Fatal("anonymous cookie does not match the page")
	}
	login := anon.get("/login")
	if csrfMeta(t, login.body) != meta {
		t.Fatal("a second page rotated the anonymous token")
	}

	owner := ts.confirmAndSetPassword(t, "page@example.com", "longenough")
	secret := sessionSecret(t, owner)
	h := ts.personalHousehold(t, "page@example.com")
	tr := owner.createTracker(h, "Meals", nil)
	id := strconv.FormatUint(uint64(tr.ID), 10)
	owner.post("/trackers/"+id+"/quick", nil)
	if r := owner.post("/trackers/"+id+"/share", nil); r.status != http.StatusOK && r.status != http.StatusSeeOther {
		t.Fatalf("share on: %d %s", r.status, r.body)
	}
	page := owner.get("/trackers/" + id)
	if csrfMeta(t, page.body) != secret {
		t.Fatal("tracker page meta is not the session secret")
	}
	if !strings.Contains(page.body, `name="csrf_token" value="`+secret+`"`) {
		t.Fatal("nested entry form lost the token")
	}
	partial := ownerGetHX(t, owner, "/trackers/"+id)
	if strings.Contains(partial, `<meta name="csrf-token"`) {
		t.Fatal("partial replaced the head token")
	}
	if !strings.Contains(partial, `name="csrf_token" value="`+secret+`"`) {
		t.Fatal("partial form lost the token")
	}

	share := anon.get("/s/" + shareToken(t, ts, tr))
	shareMeta := csrfMeta(t, share.body)
	if shareMeta == "" || shareMeta == secret {
		t.Fatalf("share token %q", shareMeta)
	}
	if !strings.Contains(share.body, `name="csrf_token" value="`+shareMeta+`"`) {
		t.Fatal("share form lost the token")
	}
}

func TestCSRFRejectionDoesNotLogTheToken(t *testing.T) {
	var buf bytes.Buffer
	a := newTestApp(t)
	a.logger = slog.New(slog.NewTextHandler(&buf, nil))
	token, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	other, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(csrfFieldName+"="+other))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: token})
	req.Header.Set(csrfHeader, other)
	a.routes().ServeHTTP(httptest.NewRecorder(), req)
	logged := buf.String()
	if strings.Contains(logged, token) || strings.Contains(logged, other) {
		t.Fatalf("log contained a token: %s", logged)
	}
	if !strings.Contains(logged, "csrf rejected") {
		t.Fatalf("log: %s", logged)
	}
}

func TestCSRFCookieIsHostOnly(t *testing.T) {
	h := newTestApp(t).routes()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	var c *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == csrfCookie {
			c = cookie
		}
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure || c.Path != "/" || c.Domain != "" {
		t.Fatalf("cookie %+v", c)
	}
	if !validCSRFToken(c.Value) {
		t.Fatalf("value %q", c.Value)
	}

	a := newTestApp(t)
	a.cfg.Env = "prod"
	rec = httptest.NewRecorder()
	token, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	a.setCSRFCookie(rec, token)
	c = rec.Result().Cookies()[0]
	if !c.Secure || !c.HttpOnly || c.Domain != "" || c.Path != "/" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("prod cookie %+v", c)
	}
}

func TestCSRFScriptStaysOnThisOrigin(t *testing.T) {
	h := newTestApp(t).routes()
	for _, path := range []string{"/static/app.js", "/static/reorder.js"} {
		code, body := get(t, h, path, nil)
		if code != http.StatusOK {
			t.Fatalf("%s: %d", path, code)
		}
		if !strings.Contains(body, "trackCSRF") || !strings.Contains(body, "sameOrigin") {
			t.Fatalf("%s does not use the shared CSRF helper", path)
		}
	}
	_, app := get(t, h, "/static/app.js", nil)
	if !strings.Contains(app, "htmx:configRequest") || !strings.Contains(app, "X-CSRF-Token") {
		t.Fatal("app.js does not attach the header for HTMX")
	}
	_, reorder := get(t, h, "/static/reorder.js", nil)
	if !strings.Contains(reorder, `credentials: "same-origin"`) {
		t.Fatal("reorder fetch does not keep credentials on this origin")
	}
}

func csrfMeta(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no csrf meta")
	}
	return m[1]
}

func csrfFields(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`name="csrf_token" value="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func ownerGetHX(t *testing.T, b *browser, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, b.ts.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	return b.do(req).body
}

func shareToken(t *testing.T, ts *testServer, tr Tracker) string {
	t.Helper()
	var fresh Tracker
	if err := ts.app.db.Take(&fresh, tr.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fresh.ShareToken == nil {
		t.Fatal("no share token")
	}
	return *fresh.ShareToken
}

func TestStampCSRFReachesNestedFields(t *testing.T) {
	page := sharePage{Card: trackerCard{}, Entries: []entryView{{}}}
	stamped := stampCSRF(page, "secret").(sharePage)
	if stamped.Card.CSRFToken != "secret" || len(stamped.Entries) != 1 || stamped.Entries[0].CSRFToken != "secret" {
		t.Fatalf("card %q entries %+v", stamped.Card.CSRFToken, stamped.Entries)
	}
}
