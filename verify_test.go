package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignupRejectsMalformedEmail(t *testing.T) {
	ts := newTestServer(t)
	for _, email := range []string{"", "   ", "not-an-email", "Dog <dog@example.com>"} {
		r := ts.browser(t).signup(email, "")
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "valid email") {
			t.Errorf("%q: status %d", email, r.status)
		}
	}
	var count int64
	ts.app.db.Model(&User{}).Count(&count)
	if count != 0 {
		t.Fatalf("%d users created", count)
	}
}

func TestSignupSucceedsWhenVerificationEmailFails(t *testing.T) {
	ts := newTestServer(t)
	ts.mailer.setErr(errors.New("smtp: secret-relay refused"))
	b := ts.browser(t)

	r := b.signup("dog@example.com", "")
	if r.status != http.StatusOK || r.url.Path != "/" {
		t.Fatalf("signup: %d at %s", r.status, r.url)
	}
	if !b.loggedInAs("dog@example.com") {
		t.Fatal("not logged in")
	}
	u := ts.user(t, "dog@example.com")
	if u.EmailVerifiedAt != nil {
		t.Fatal("failed send marked the email verified")
	}
	if strings.Contains(r.body, "secret-relay") || strings.Contains(r.body, "smtp:") {
		t.Fatalf("page exposes the mail error: %s", r.body)
	}
	if len(ts.mailer.messages()) != 0 {
		t.Fatal("failed send was recorded as delivered")
	}
}

func TestNewUserStartsUnverified(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")

	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("new user is verified")
	}
	settings := b.get("/settings")
	if !strings.Contains(settings.body, "isn't verified yet") || !strings.Contains(settings.body, "Send verification email") {
		t.Fatal("settings missing verification prompt")
	}
	link := ts.lastVerifyLink(t)
	if r := b.get(link); r.status != http.StatusOK || !strings.Contains(r.body, "Verify dog@example.com") {
		t.Fatalf("confirm page: %d", r.status)
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("opening the link verified the address")
	}
}

func TestVerificationLinkConfirmsOnce(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	link := ts.lastVerifyLink(t)

	raw := strings.TrimPrefix(link, "/verify/")
	var vt VerificationToken
	if err := ts.app.db.Take(&vt).Error; err != nil {
		t.Fatal(err)
	}
	if vt.TokenHash == raw || vt.TokenHash != hashToken(raw) {
		t.Fatalf("stored %q for token %q", vt.TokenHash, raw)
	}

	// A logged-out browser can confirm. The link does not log them in.
	b := ts.browser(t)
	r := b.post(link, nil)
	if r.status != http.StatusOK || r.url.Path != "/verify/done" || !strings.Contains(r.body, "Email verified") {
		t.Fatalf("confirm: %d at %s", r.status, r.url)
	}
	if b.get("/settings").url.Path != "/login" {
		t.Fatal("verification link logged in")
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt == nil {
		t.Fatal("email still unverified")
	}

	if r := b.post(link, nil); r.status != http.StatusGone || !strings.Contains(r.body, "Send a new link") {
		t.Fatalf("reused link: %d", r.status)
	}
	if r := b.get(link); r.status != http.StatusGone {
		t.Fatalf("reused link page: %d", r.status)
	}
}

func TestVerificationLinkExpires(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	link := ts.lastVerifyLink(t)

	ts.clock.advance(verifyLinkLifetime + time.Minute)
	r := ts.browser(t).post(link, nil)
	if r.status != http.StatusGone || !strings.Contains(r.body, "expired") {
		t.Fatalf("expired link: %d", r.status)
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("expired link verified the address")
	}
}

func TestInvalidVerificationLinkIsRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")

	r := ts.browser(t).post("/verify/not-a-real-token", nil)
	if r.status != http.StatusGone || !strings.Contains(r.body, "Send a new link") {
		t.Fatalf("invalid link: %d", r.status)
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("invalid link verified the address")
	}
}

func TestResendVerification(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")
	before := len(ts.mailer.messages())

	r := b.post("/settings/verify", nil)
	if r.status != http.StatusOK || r.url.Path != "/settings" || !strings.Contains(r.body, "Verification email sent") {
		t.Fatalf("resend: %d at %s", r.status, r.url)
	}
	if len(ts.mailer.messages()) != before+1 {
		t.Fatalf("emails %d, want %d", len(ts.mailer.messages()), before+1)
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("resend marked the email verified")
	}

	b.post(ts.lastVerifyLink(t), nil)
	if ts.user(t, "dog@example.com").EmailVerifiedAt == nil {
		t.Fatal("resent link did not verify")
	}
}

func TestResendVerificationDoesNothingWhenAlreadyVerified(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")
	b.post(ts.lastVerifyLink(t), nil)
	if ts.user(t, "dog@example.com").EmailVerifiedAt == nil {
		t.Fatal("setup")
	}

	n := len(ts.mailer.messages())
	r := b.post("/settings/verify", nil)
	if r.status != http.StatusOK || !strings.Contains(r.body, "already verified") {
		t.Fatalf("resend: %d", r.status)
	}
	if len(ts.mailer.messages()) != n {
		t.Fatal("sent a verification email for a verified address")
	}
	if strings.Contains(r.body, "Send verification email") {
		t.Fatal("verified account still offers resend")
	}
}

func TestResendVerificationFailureStaysLoggedIn(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")
	ts.mailer.setErr(errors.New("smtp: secret-relay refused"))

	r := b.post("/settings/verify", nil)
	if r.status != http.StatusInternalServerError || !strings.Contains(r.body, "couldn&#39;t send the verification email") {
		t.Fatalf("resend: %d", r.status)
	}
	if strings.Contains(r.body, "secret-relay") || strings.Contains(r.body, "smtp:") {
		t.Fatal("page exposes the mail error")
	}
	if !b.loggedInAs("dog@example.com") {
		t.Fatal("logged out after a failed resend")
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("failed resend marked the email verified")
	}
}

func TestMagicLinkVerifiesEmail(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("starts verified")
	}

	b := ts.browser(t)
	b.post("/login/link", url.Values{"email": {"dog@example.com"}})
	link := ts.lastLoginLink(t)
	if r := b.get(link); r.status != http.StatusOK {
		t.Fatalf("confirm: %d", r.status)
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("opening the login link verified the address")
	}

	b.post(link, nil)
	if !b.loggedInAs("dog@example.com") {
		t.Fatal("magic link did not log in")
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt == nil {
		t.Fatal("magic link left the email unverified")
	}
}

func TestMagicLinkKeepsExistingVerificationTime(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	past := ts.clock.now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := ts.app.db.Model(&User{}).Where("email = ?", "dog@example.com").Update("email_verified_at", past).Error; err != nil {
		t.Fatal(err)
	}
	ts.clock.advance(time.Minute)

	b := ts.browser(t)
	b.post("/login/link", url.Values{"email": {"dog@example.com"}})
	b.post(ts.lastLoginLink(t), nil)

	u := ts.user(t, "dog@example.com")
	if u.EmailVerifiedAt == nil || u.EmailVerifiedAt.Unix() != past.Unix() {
		t.Fatalf("verified at %v, want %v", u.EmailVerifiedAt, past)
	}
}

func TestVerificationTokenIsNotALoginToken(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	verify := ts.lastVerifyLink(t)

	b := ts.browser(t)
	b.post("/login/link", url.Values{"email": {"dog@example.com"}})
	login := ts.lastLoginLink(t)

	verifyToken := strings.TrimPrefix(verify, "/verify/")
	if r := b.post("/login/link/"+verifyToken, nil); r.status != http.StatusGone {
		t.Fatalf("verification token as login: %d", r.status)
	}
	if b.loggedInAs("dog@example.com") {
		t.Fatal("verification token logged in")
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("login endpoint verified the address")
	}

	loginToken := strings.TrimPrefix(login, "/login/link/")
	if r := b.post("/verify/"+loginToken, nil); r.status != http.StatusGone {
		t.Fatalf("login token as verification: %d", r.status)
	}
	if ts.user(t, "dog@example.com").EmailVerifiedAt != nil {
		t.Fatal("login token verified the address")
	}

	if r := b.post(verify, nil); r.status != http.StatusOK || ts.user(t, "dog@example.com").EmailVerifiedAt == nil {
		t.Fatal("verification token was consumed by the login endpoint")
	}
	b.post(login, nil)
	if !b.loggedInAs("dog@example.com") {
		t.Fatal("login token was consumed by the verification endpoint")
	}
}

func TestLoginLinkSendFailureIsAFriendlyPage(t *testing.T) {
	ts := newTestServer(t)
	ts.browser(t).signup("dog@example.com", "")
	ts.mailer.setErr(errors.New("smtp: secret-relay refused"))

	known := ts.browser(t).post("/login/link", url.Values{"email": {"dog@example.com"}, "next": {"/settings"}})
	if known.status != http.StatusInternalServerError {
		t.Fatalf("status %d", known.status)
	}
	for _, want := range []string{
		"We couldn&#39;t send your login link",
		"Something went wrong while sending the email. Please try again in a moment.",
		"Back to log in",
		`href="/login?next=%2Fsettings"`,
		"/static/pico.min.css",
	} {
		if !strings.Contains(known.body, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, secret := range []string{"secret-relay", "smtp:", "Internal Server Error", "dog@example.com"} {
		if strings.Contains(known.body, secret) {
			t.Errorf("page exposes %q", secret)
		}
	}

	// An address with no account still gets the same check-your-email page as a successful send.
	unknown := ts.browser(t).post("/login/link", url.Values{"email": {"nobody@example.com"}})
	if unknown.status != http.StatusOK || !strings.Contains(unknown.body, "Check your email") {
		t.Fatalf("unknown address: %d", unknown.status)
	}
	if strings.Contains(unknown.body, "couldn't send") || strings.Contains(unknown.body, "secret-relay") {
		t.Fatal("unknown address revealed a send failure")
	}
}
