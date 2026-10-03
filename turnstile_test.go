package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func turnstileResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// installTestTurnstile stubs Siteverify. Tests must not call Cloudflare.
func installTestTurnstile(a *app) {
	a.turnstile.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return turnstileResponse(http.StatusOK, `{"success":true}`), nil
	})
}

func TestProductionTurnstileConfig(t *testing.T) {
	ok := config{
		Env:                "prod",
		BaseURL:            "https://trackanything.io",
		baseURLSet:         true,
		TurnstileSiteKey:   "site-key",
		TurnstileSecretKey: "secret-key",
	}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}

	missingSite := ok
	missingSite.TurnstileSiteKey = ""
	err := missingSite.validate()
	if err == nil || !strings.Contains(err.Error(), "TURNSTILE_SITE_KEY") {
		t.Fatalf("missing site key: %v", err)
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("error included the secret: %v", err)
	}

	missingSecret := ok
	missingSecret.TurnstileSecretKey = ""
	err = missingSecret.validate()
	if err == nil || !strings.Contains(err.Error(), "TURNSTILE_SECRET_KEY") {
		t.Fatalf("missing secret key: %v", err)
	}
	if strings.Contains(err.Error(), "site-key") {
		t.Fatalf("error included the site key: %v", err)
	}
}

func TestDevTurnstileDefaults(t *testing.T) {
	t.Setenv("ENV", "dev")
	t.Setenv("BASE_URL", "")
	t.Setenv("TRUSTED_IP_HEADER", "")
	t.Setenv("TURNSTILE_SITE_KEY", "")
	t.Setenv("TURNSTILE_SECRET_KEY", "")
	cfg := loadConfig()
	if cfg.TurnstileSiteKey != turnstileTestSiteKey || cfg.TurnstileSecretKey != turnstileTestSecretKey {
		t.Fatal("development did not use the test keys")
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TURNSTILE_SITE_KEY", "custom-site")
	t.Setenv("TURNSTILE_SECRET_KEY", "custom-secret")
	cfg = loadConfig()
	if cfg.TurnstileSiteKey != "custom-site" || cfg.TurnstileSecretKey != "custom-secret" {
		t.Fatal("explicit development keys were replaced")
	}

	onlySite := config{Env: "dev", TurnstileSiteKey: "custom-site"}
	if err := onlySite.validate(); err == nil {
		t.Fatal("development accepted a site key without a secret")
	}

	t.Setenv("ENV", "prod")
	t.Setenv("BASE_URL", "https://trackanything.io")
	t.Setenv("TURNSTILE_SITE_KEY", "")
	t.Setenv("TURNSTILE_SECRET_KEY", "")
	cfg = loadConfig()
	if cfg.TurnstileSiteKey != "" || cfg.TurnstileSecretKey != "" {
		t.Fatal("production substituted test keys")
	}
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "TURNSTILE_SITE_KEY") {
		t.Fatalf("production missing keys: %v", err)
	}
}

func TestTurnstileVerify(t *testing.T) {
	var calls atomic.Int32
	v := newTurnstileVerifier("site-secret")
	v.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != turnstileSiteverifyURL || r.Method != http.MethodPost {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content-type %q", ct)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Errorf("parse body: %v", err)
		}
		if form.Get("secret") != "site-secret" || form.Get("response") != "widget-token" {
			t.Errorf("form secret/response = %q / %q", form.Get("secret"), form.Get("response"))
		}
		if form.Get("remoteip") != "203.0.113.9" {
			t.Errorf("remoteip %q", form.Get("remoteip"))
		}
		return turnstileResponse(http.StatusOK, `{"success":true,"error-codes":[]}`), nil
	})
	if err := v.verify(context.Background(), "widget-token", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls %d", calls.Load())
	}

	v.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Error("siteverify was called for a missing token")
		return nil, errors.New("should not be called")
	})
	for _, token := range []string{"", "  "} {
		err := v.verify(context.Background(), token, "203.0.113.9")
		if err == nil || !strings.Contains(err.Error(), "missing token") {
			t.Fatalf("%q: %v", token, err)
		}
		if strings.Contains(err.Error(), "site-secret") {
			t.Fatalf("error included the secret: %v", err)
		}
	}

	v.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if _, ok := form["remoteip"]; ok {
			t.Error("empty remoteip was sent")
		}
		return turnstileResponse(http.StatusOK, `{"success":true}`), nil
	})
	if err := v.verify(context.Background(), "widget-token", ""); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		status int
		body   string
		netErr bool
		want   string
	}{
		{name: "success false", status: http.StatusOK, body: `{"success":false,"error-codes":["invalid-input-response"]}`, want: "invalid-input-response"},
		{name: "success false without codes", status: http.StatusOK, body: `{"success":false}`, want: "rejected"},
		{name: "malformed", status: http.StatusOK, body: `{"success":`, want: "malformed"},
		{name: "http error", status: http.StatusBadGateway, body: `{"success":true}`, want: "status 502"},
		{name: "network", netErr: true, want: "connection refused"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if tc.netErr {
					return nil, errors.New("dial tcp: connection refused")
				}
				return turnstileResponse(tc.status, tc.body), nil
			})
			err := v.verify(context.Background(), "widget-token", "203.0.113.9")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
			if strings.Contains(err.Error(), "widget-token") || strings.Contains(err.Error(), "site-secret") {
				t.Fatalf("error leaked a secret or token: %v", err)
			}
		})
	}
}

func TestTurnstileVerifyTimeout(t *testing.T) {
	v := newTurnstileVerifier("site-secret")
	v.client.Timeout = 30 * time.Millisecond
	v.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	start := time.Now()
	err := v.verify(context.Background(), "widget-token", "")
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if time.Since(start) > time.Second {
		t.Fatal("verification hung")
	}
	if strings.Contains(err.Error(), "site-secret") || strings.Contains(err.Error(), "widget-token") {
		t.Fatalf("error leaked a secret or token: %v", err)
	}
}

func TestSuccessfulTurnstileVerificationPermitsProtectedActions(t *testing.T) {
	var calls atomic.Int32
	ts := newTestServer(t)
	ts.app.turnstile.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != turnstileSiteverifyURL || r.Method != http.MethodPost {
			return nil, errors.New("unexpected siteverify request")
		}
		return turnstileResponse(http.StatusOK, `{"success":true}`), nil
	})

	b := ts.browser(t)
	if r := b.signup("dog@example.com", ""); r.url.Path != "/" || !b.loggedInAs("dog@example.com") {
		t.Fatalf("signup: %d at %s", r.status, r.url)
	}
	if r := b.post("/login/link", url.Values{"email": {"dog@example.com"}}); r.status != http.StatusOK {
		t.Fatalf("magic link: %d", r.status)
	}
	if r := b.post(ts.lastVerifyLink(t), nil); r.url.Path != "/verify/done" {
		t.Fatalf("verify: %s", r.url)
	}
	if r := b.post("/settings/password", url.Values{"password": {"correct horse"}, "confirm": {"correct horse"}}); !strings.Contains(r.body, "Password saved") {
		t.Fatalf("password: %d", r.status)
	}
	other := ts.browser(t)
	other.login("dog@example.com", "correct horse")
	if !other.loggedInAs("dog@example.com") {
		t.Fatal("password login did not start a session")
	}
	if calls.Load() != 4 {
		t.Fatalf("siteverify calls: %d", calls.Load())
	}
}

func TestProtectedActionsRejectFailedTurnstile(t *testing.T) {
	cases := []struct {
		name   string
		token  string
		status int
		body   string
		netErr bool
	}{
		{name: "missing token"},
		{name: "success false", token: "widget-token", status: http.StatusOK, body: `{"success":false,"error-codes":["invalid-input-response"]}`},
		{name: "network", token: "widget-token", netErr: true},
		{name: "malformed", token: "widget-token", status: http.StatusOK, body: `{"success":`},
		{name: "http error", token: "widget-token", status: http.StatusBadGateway, body: `{"success":true}`},
	}
	for _, tc := range cases {
		t.Run("signup/"+tc.name, func(t *testing.T) {
			ts := newTestServer(t)
			calls := stubTurnstileFailure(ts, tc.netErr, tc.status, tc.body)
			r := ts.browser(t).post("/signup", url.Values{
				"email":        {"dog@example.com"},
				"timezone":     {"UTC"},
				turnstileField: {tc.token},
			})
			assertTurnstileRejected(t, ts, r)
			if userCount(t, ts) != 0 {
				t.Fatal("signup created an account")
			}
			if tc.token == "" && calls.Load() != 0 {
				t.Fatal("missing token called siteverify")
			}
			if tc.token != "" && calls.Load() != 1 {
				t.Fatalf("siteverify calls: %d", calls.Load())
			}
		})
		t.Run("login/"+tc.name, func(t *testing.T) {
			ts := newTestServer(t)
			ts.confirmAndSetPassword(t, "dog@example.com", "correct horse")
			stubTurnstileFailure(ts, tc.netErr, tc.status, tc.body)
			b := ts.browser(t)
			r := b.post("/login", url.Values{
				"email":        {"dog@example.com"},
				"password":     {"correct horse"},
				turnstileField: {tc.token},
			})
			assertTurnstileRejected(t, ts, r)
			if b.loggedInAs("dog@example.com") {
				t.Fatal("password login started a session")
			}
		})
		t.Run("magic-link/"+tc.name, func(t *testing.T) {
			ts := newTestServer(t)
			ts.browser(t).signup("dog@example.com", "")
			before := loginEmailCount(ts)
			stubTurnstileFailure(ts, tc.netErr, tc.status, tc.body)
			r := ts.browser(t).post("/login/link", url.Values{
				"email":        {"dog@example.com"},
				turnstileField: {tc.token},
			})
			assertTurnstileRejected(t, ts, r)
			if loginEmailCount(ts) != before {
				t.Fatal("magic link was sent")
			}
		})
		t.Run("password/"+tc.name, func(t *testing.T) {
			ts := newTestServer(t)
			b := ts.confirmAndSetPassword(t, "dog@example.com", "original password")
			before := ts.user(t, "dog@example.com").PasswordHash
			stubTurnstileFailure(ts, tc.netErr, tc.status, tc.body)
			r := b.post("/settings/password", url.Values{
				"password":     {"new password"},
				"confirm":      {"new password"},
				turnstileField: {tc.token},
			})
			assertTurnstileRejected(t, ts, r)
			if ts.user(t, "dog@example.com").PasswordHash != before {
				t.Fatal("password changed")
			}
			if !strings.Contains(r.body, `class="cf-turnstile"`) {
				t.Fatal("password form did not render a fresh widget")
			}
		})
	}
}

func TestTurnstileFailureStillRateLimits(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	form := url.Values{"email": {"dog@example.com"}, "password": {"guess"}, turnstileField: {""}}
	for i := range 10 {
		if r := b.post("/login", form); r.status != http.StatusForbidden {
			t.Fatalf("attempt %d: %d", i+1, r.status)
		}
	}
	if r := b.post("/login", form); r.status != http.StatusTooManyRequests {
		t.Fatalf("11th attempt: %d body %s", r.status, r.body)
	}
}

func TestTurnstileDoesNotBypassCSRF(t *testing.T) {
	var calls atomic.Int32
	ts := newTestServer(t)
	ts.app.turnstile.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return turnstileResponse(http.StatusOK, `{"success":true}`), nil
	})
	form := url.Values{
		"email":        {"csrf@example.com"},
		"timezone":     {"UTC"},
		turnstileField: {"test-token"},
	}
	req, err := http.NewRequest(http.MethodPost, ts.srv.URL+"/signup", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if strings.Contains(string(body), turnstileRejectedMessage) {
		t.Fatalf("CSRF rejection was handled as turnstile: %s", body)
	}
	if calls.Load() != 0 {
		t.Fatal("cross-origin signup called siteverify")
	}
	if userCount(t, ts) != 0 {
		t.Fatal("cross-origin signup created an account")
	}
}

func TestTurnstileSendsDetectedClientIP(t *testing.T) {
	var got url.Values
	ts := newTestServer(t)
	ts.app.cfg.TrustedIPHeader = "Fly-Client-IP"
	ts.app.turnstile.secret = "site-secret"
	ts.app.turnstile.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		got, err = url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		return turnstileResponse(http.StatusOK, `{"success":true}`), nil
	})
	form := url.Values{
		"email":        {"ip@example.com"},
		"timezone":     {"UTC"},
		turnstileField: {"widget-token"},
	}
	req, err := http.NewRequest(http.MethodPost, ts.srv.URL+"/signup", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Fly-Client-IP", "203.0.113.9")
	req.Header.Set("CF-Connecting-IP", "198.51.100.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got.Get("remoteip") != "203.0.113.9" {
		t.Fatalf("remoteip %q", got.Get("remoteip"))
	}
	if got.Get("secret") != "site-secret" || got.Get("response") != "widget-token" {
		t.Fatalf("form = %v", got)
	}
}

func TestTurnstileLogsOmitSecrets(t *testing.T) {
	var buf bytes.Buffer
	ts := newTestServer(t)
	ts.app.logger = slog.New(slog.NewTextHandler(&buf, nil))
	ts.app.turnstile.secret = "super-secret-turnstile"
	ts.app.turnstile.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return turnstileResponse(http.StatusOK, `{"success":false,"error-codes":["invalid-input-response"]}`), nil
	})
	ts.browser(t).post("/signup", url.Values{
		"email":        {"dog@example.com"},
		"timezone":     {"UTC"},
		turnstileField: {"super-token-value"},
	})
	logged := buf.String()
	if !strings.Contains(logged, "turnstile verification failed") || !strings.Contains(logged, "invalid-input-response") {
		t.Fatalf("log: %s", logged)
	}
	if strings.Contains(logged, "super-secret-turnstile") || strings.Contains(logged, "super-token-value") {
		t.Fatalf("log leaked a secret or token: %s", logged)
	}
}

func TestTurnstileWidgetPages(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	script := "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit"
	home := b.get("/")
	if strings.Contains(home.body, "challenges.cloudflare.com") || strings.Contains(home.body, "cf-turnstile") {
		t.Fatal("home loaded turnstile")
	}
	for _, path := range []string{"/signup", "/login"} {
		page := b.get(path)
		if !strings.Contains(page.body, script) || !strings.Contains(page.body, `class="cf-turnstile"`) {
			t.Fatalf("%s missing the widget", path)
		}
		if !strings.Contains(page.body, `data-sitekey="`+ts.app.cfg.TurnstileSiteKey+`"`) {
			t.Fatalf("%s missing the site key", path)
		}
		if strings.Contains(page.body, ts.app.turnstile.secret) {
			t.Fatalf("%s contained the secret", path)
		}
		if !strings.Contains(page.body, `method="post"`) {
			t.Fatalf("%s dropped the normal form", path)
		}
	}
	signup := b.get("/signup")
	if !strings.Contains(signup.body, `method="post"`) || !strings.Contains(signup.body, `action="/signup"`) {
		t.Fatal("signup form is not a normal post")
	}
	login := b.get("/login")
	if !strings.Contains(login.body, `action="/login/link"`) || !strings.Contains(login.body, `formaction="/login"`) {
		t.Fatal("login form dropped an action")
	}

	b.signup("dog@example.com", "")
	unverified := b.get("/settings")
	if strings.Contains(unverified.body, "challenges.cloudflare.com") || strings.Contains(unverified.body, "cf-turnstile") {
		t.Fatal("settings loaded turnstile before the password form exists")
	}
	if r := b.post(ts.lastVerifyLink(t), nil); r.url.Path != "/verify/done" {
		t.Fatalf("verify: %s", r.url)
	}
	settings := b.get("/settings")
	if !strings.Contains(settings.body, script) || !strings.Contains(settings.body, `action="/settings/password"`) {
		t.Fatal("verified settings missing the password widget")
	}
	if strings.Contains(settings.body, ts.app.turnstile.secret) {
		t.Fatal("settings contained the secret")
	}

	js := b.get("/static/app.js").body
	if !strings.Contains(js, "htmx:afterSettle") || !strings.Contains(js, "turnstile.render") || !strings.Contains(js, "onTurnstileLoad") {
		t.Fatal("app.js does not render a fresh widget after load or an HTMX swap")
	}
}

func TestTurnstileHTMXErrorRendersAFreshWidget(t *testing.T) {
	ts := newTestServer(t)
	form := url.Values{
		"email":        {"dog@example.com"},
		"timezone":     {"UTC"},
		turnstileField: {"spent-token"},
	}
	req, err := http.NewRequest(http.MethodPost, ts.srv.URL+"/signup", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	ts.app.turnstile.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return turnstileResponse(http.StatusOK, `{"success":false,"error-codes":["timeout-or-duplicate"]}`), nil
	})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(page, turnstileRejectedMessage) {
		t.Fatalf("status %d body %s", resp.StatusCode, page)
	}
	if strings.Contains(page, "<!doctype") {
		t.Fatal("HTMX error returned a full page")
	}
	if !strings.Contains(page, `class="cf-turnstile"`) || !strings.Contains(page, `method="post"`) {
		t.Fatal("HTMX error did not include a fresh form widget")
	}
	if strings.Contains(page, "spent-token") || strings.Contains(page, "timeout-or-duplicate") || strings.Contains(page, ts.app.turnstile.secret) {
		t.Fatalf("response leaked details: %s", page)
	}
	if userCount(t, ts) != 0 {
		t.Fatal("signup created an account")
	}
}

func stubTurnstileFailure(ts *testServer, netErr bool, status int, body string) *atomic.Int32 {
	var calls atomic.Int32
	ts.app.turnstile.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if netErr {
			return nil, errors.New("dial tcp: connection refused")
		}
		return turnstileResponse(status, body), nil
	})
	return &calls
}

func assertTurnstileRejected(t *testing.T, ts *testServer, r response) {
	t.Helper()
	if r.status != http.StatusForbidden || !strings.Contains(r.body, turnstileRejectedMessage) {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	for _, leaked := range []string{"siteverify", "invalid-input-response", "dial tcp", "malformed", "widget-token", ts.app.turnstile.secret} {
		if leaked != "" && strings.Contains(r.body, leaked) {
			t.Fatalf("response contained %q", leaked)
		}
	}
}

func userCount(t *testing.T, ts *testServer) int64 {
	t.Helper()
	var n int64
	if err := ts.app.db.Model(&User{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func loginEmailCount(ts *testServer) int {
	n := 0
	for _, msg := range ts.mailer.messages() {
		if strings.Contains(msg.Body, "/login/link/") {
			n++
		}
	}
	return n
}
