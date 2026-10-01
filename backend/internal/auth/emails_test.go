package auth

import (
	"context"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"vocatogether/backend/internal/email"
)

const testRecipient = "ana@example.com"

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// requireSendable passes msg through the email package's own validation.
func requireSendable(t *testing.T, msg email.Message) {
	t.Helper()
	var r email.Recorder
	if err := r.Send(context.Background(), msg); err != nil {
		t.Fatalf("message fails email validation: %v", err)
	}
}

// linkIn extracts the single URL on its own line in the body.
func linkIn(t *testing.T, text string) *url.URL {
	t.Helper()
	var links []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			links = append(links, line)
		}
	}
	if len(links) != 1 {
		t.Fatalf("found %d links in body, want exactly 1", len(links))
	}
	return mustParseURL(t, links[0])
}

func TestTokenEmailLinks(t *testing.T) {
	base := mustParseURL(t, "https://api.example.com")
	token := NewToken("")

	tests := []struct {
		name     string
		msg      email.Message
		wantLink string
		wantTTL  string
	}{
		{
			name:     "verification",
			msg:      verificationEmail(base, testRecipient, token.Raw, 24*time.Hour),
			wantLink: "https://api.example.com/verify-email?token=" + token.Raw,
			wantTTL:  "24 hours",
		},
		{
			name:     "password reset",
			msg:      passwordResetEmail(base, testRecipient, token.Raw, 30*time.Minute),
			wantLink: "https://api.example.com/reset-password?token=" + token.Raw,
			wantTTL:  "30 minutes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.msg.To != testRecipient {
				t.Errorf("To = %q, want %q", tt.msg.To, testRecipient)
			}
			if got := linkIn(t, tt.msg.Text).String(); got != tt.wantLink {
				t.Errorf("link = %q, want %q", got, tt.wantLink)
			}
			if !strings.Contains(tt.msg.Text, tt.wantTTL) {
				t.Errorf("body does not mention the expiry %q", tt.wantTTL)
			}
			requireSendable(t, tt.msg)
		})
	}
}

// Real tokens are base64url, but the link builder must not depend on that:
// any token must round-trip as exactly one query parameter, with no way to
// add parameters, change the path, or append a fragment.
func TestTokenEmailLinksEscapeToken(t *testing.T) {
	base := mustParseURL(t, "https://api.example.com")
	hostile := "a+b/c=d&admin=1#frag ?x=%41ñ"

	builders := map[string]func() email.Message{
		"verification":   func() email.Message { return verificationEmail(base, testRecipient, hostile, time.Hour) },
		"password reset": func() email.Message { return passwordResetEmail(base, testRecipient, hostile, time.Hour) },
	}
	wantPaths := map[string]string{"verification": "/verify-email", "password reset": "/reset-password"}

	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			link := linkIn(t, build().Text)
			if link.Path != wantPaths[name] {
				t.Errorf("path = %q, want %q", link.Path, wantPaths[name])
			}
			if link.Fragment != "" {
				t.Errorf("token introduced a fragment: %q", link.Fragment)
			}
			q := link.Query()
			if len(q) != 1 || len(q["token"]) != 1 {
				t.Fatalf("query = %v, want exactly one token parameter", q)
			}
			if q.Get("token") != hostile {
				t.Errorf("token did not round-trip: got %q", q.Get("token"))
			}
		})
	}
}

// APP_BASE_URL may include a path prefix (e.g. behind a reverse proxy); links
// must keep it, and any query or fragment on the base must not leak through.
func TestTokenEmailLinksRespectBaseURL(t *testing.T) {
	base := mustParseURL(t, "https://example.com/api?stale=1#old")
	link := linkIn(t, verificationEmail(base, testRecipient, "tok", time.Hour).Text)

	if got, want := link.String(), "https://example.com/api/verify-email?token=tok"; got != want {
		t.Errorf("link = %q, want %q", got, want)
	}
	if base.String() != "https://example.com/api?stale=1#old" {
		t.Errorf("builder mutated the shared base URL: %q", base.String())
	}
}

func TestNotificationEmailsContainNoLinkOrToken(t *testing.T) {
	tests := map[string]email.Message{
		"account exists":       accountExistsEmail(testRecipient),
		"password changed":     passwordChangedEmail(testRecipient),
		"passwordless account": passwordlessAccountEmail(testRecipient),
	}
	for name, msg := range tests {
		t.Run(name, func(t *testing.T) {
			if msg.To != testRecipient {
				t.Errorf("To = %q, want %q", msg.To, testRecipient)
			}
			for _, forbidden := range []string{"http://", "https://", "token", "?"} {
				if strings.Contains(strings.ToLower(msg.Text), forbidden) {
					t.Errorf("body contains %q; these emails must carry no link or token", forbidden)
				}
			}
			requireSendable(t, msg)
		})
	}
}

func TestAuthEmailsHaveDistinctSubjects(t *testing.T) {
	base := mustParseURL(t, "https://api.example.com")
	subjects := []string{
		verificationEmail(base, testRecipient, "tok", time.Hour).Subject,
		passwordResetEmail(base, testRecipient, "tok", time.Hour).Subject,
		accountExistsEmail(testRecipient).Subject,
		passwordChangedEmail(testRecipient).Subject,
		passwordlessAccountEmail(testRecipient).Subject,
	}
	seen := map[string]bool{}
	for _, s := range subjects {
		if seen[s] {
			t.Errorf("duplicate subject %q", s)
		}
		seen[s] = true
	}
}

func TestHumanDuration(t *testing.T) {
	tests := map[time.Duration]string{
		24 * time.Hour:   "24 hours",
		time.Hour:        "1 hour",
		30 * time.Minute: "30 minutes",
		time.Minute:      "1 minute",
		90 * time.Minute: "90 minutes",
	}
	for d, want := range tests {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

var (
	hrefAttr    = regexp.MustCompile(`(?is)\bhref\s*=`)
	ctaAnchor   = regexp.MustCompile(`(?s)<a href="([^"]*)"[^>]*>([^<]*)</a>`)
	fallbackURL = regexp.MustCompile(`(?s)copy this address into your browser:</p>\s*<p[^>]*>([^<]*)</p>`)
	anyURL      = regexp.MustCompile(`(?i)https?://[^\s"<]*`)
	eventAttr   = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	htmlTag     = regexp.MustCompile(`<[^>]*>`)
)

// checkSafeHTML fails if body could run code or load anything: scripts,
// event handlers, images, frames, forms, stylesheets or other fetched URLs.
// Attributes are checked inside tags only: escaped text (such as the visible
// fallback URL) may spell anything and stays inert.
func checkSafeHTML(t *testing.T, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	for _, bad := range []string{"<script", "<img", "<iframe", "<object", "<embed", "<form", "<link", "<style",
		"<svg", "<video", "<audio", "<base", "<meta http-equiv"} {
		if strings.Contains(lower, bad) {
			t.Errorf("HTML contains %q", bad)
		}
	}
	for _, tag := range htmlTag.FindAllString(lower, -1) {
		for _, bad := range []string{"javascript:", "src=", "srcset=", "background=", "url(", "@import"} {
			if strings.Contains(tag, bad) {
				t.Errorf("tag %q contains %q", tag, bad)
			}
		}
		if eventAttr.MatchString(tag) {
			t.Errorf("tag %q has an event handler attribute", tag)
		}
	}
}

func TestTokenEmailHTML(t *testing.T) {
	base := mustParseURL(t, "https://api.example.com/app")
	token := NewToken("")
	tests := []struct {
		name    string
		msg     email.Message
		link    string
		button  string
		wantTTL string
	}{
		{"verification", verificationEmail(base, testRecipient, token.Raw, 24*time.Hour),
			tokenLink(base, verifyEmailPath, token.Raw), "Verify my email", "expires in 24 hours"},
		{"password reset", passwordResetEmail(base, testRecipient, token.Raw, 30*time.Minute),
			tokenLink(base, resetPasswordPath, token.Raw), "Change password", "expires in 30 minutes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.msg.HTML
			if body == "" {
				t.Fatal("no HTML body")
			}
			if n := len(hrefAttr.FindAllString(body, -1)); n != 1 {
				t.Fatalf("found %d href attributes, want exactly 1", n)
			}
			cta := ctaAnchor.FindStringSubmatch(body)
			if cta == nil {
				t.Fatal("no CTA anchor")
			}
			if got := html.UnescapeString(cta[1]); got != tt.link {
				t.Errorf("CTA href = %q, want tokenLink %q", got, tt.link)
			}
			if cta[2] != tt.button {
				t.Errorf("CTA label = %q, want %q", cta[2], tt.button)
			}
			fallback := fallbackURL.FindStringSubmatch(body)
			if fallback == nil {
				t.Fatal("no visible fallback URL")
			}
			if got := html.UnescapeString(fallback[1]); got != tt.link {
				t.Errorf("fallback URL = %q, want tokenLink %q", got, tt.link)
			}
			// The link, twice, is the only URL in the message.
			urls := anyURL.FindAllString(body, -1)
			if len(urls) != 2 {
				t.Errorf("found %d URLs, want 2 (button and fallback): %q", len(urls), urls)
			}
			for _, u := range urls {
				if html.UnescapeString(u) != tt.link {
					t.Errorf("unexpected URL %q", u)
				}
			}
			if !strings.Contains(body, tt.wantTTL) {
				t.Errorf("HTML does not mention the expiry %q", tt.wantTTL)
			}
			if !strings.Contains(body, "<title>"+html.EscapeString(tt.msg.Subject)+"</title>") {
				t.Error("HTML title is not the subject")
			}
			checkSafeHTML(t, body)
			// Plain text stays the canonical body and carries the same link.
			if got := linkIn(t, tt.msg.Text).String(); got != tt.link {
				t.Errorf("text link = %q, want %q", got, tt.link)
			}
			requireSendable(t, tt.msg)
		})
	}
}

// A token is never HTML: whatever it contains reaches the HTML only as part
// of tokenLink's escaped query, and round-trips through both the button and
// the fallback text.
func TestTokenEmailHTMLEscapesHostileToken(t *testing.T) {
	base := mustParseURL(t, "https://api.example.com")
	hostile := `"><script>alert(1)</script><img src=x onerror=alert(1)>&amp;'` + "`javascript:alert(1)//"
	for name, msg := range map[string]email.Message{
		"verification":   verificationEmail(base, testRecipient, hostile, time.Hour),
		"password reset": passwordResetEmail(base, testRecipient, hostile, time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			checkSafeHTML(t, msg.HTML)
			if n := len(hrefAttr.FindAllString(msg.HTML, -1)); n != 1 {
				t.Fatalf("found %d href attributes, want exactly 1", n)
			}
			for _, raw := range []string{
				ctaAnchor.FindStringSubmatch(msg.HTML)[1],
				fallbackURL.FindStringSubmatch(msg.HTML)[1],
			} {
				link := mustParseURL(t, html.UnescapeString(raw))
				if got := link.Query().Get("token"); got != hostile {
					t.Errorf("token did not round-trip: got %q", got)
				}
			}
		})
	}
}

// The template escapes Link on its own too, should it ever receive a value
// that didn't come from tokenLink.
func TestActionEmailTemplateEscapesLink(t *testing.T) {
	for _, link := range []string{
		`javascript:alert(1)`,
		`https://x.example/"><script>alert(1)</script>`,
		`https://x.example/' onmouseover='alert(1)`,
	} {
		body := actionEmail{Subject: "s", Heading: "<b>h</b>", Button: "b", Link: link, Expiry: "e"}.html()
		if body == "" {
			t.Fatalf("%q: render failed", link)
		}
		checkSafeHTML(t, body)
		if strings.Contains(body, "<b>") {
			t.Error("heading markup was not escaped")
		}
		href := html.UnescapeString(ctaAnchor.FindStringSubmatch(body)[1])
		if strings.HasPrefix(strings.ToLower(href), "javascript:") {
			t.Errorf("%q: href kept a javascript: URL: %q", link, href)
		}
	}
}

func TestNotificationEmailsAreTextOnly(t *testing.T) {
	for name, msg := range map[string]email.Message{
		"account exists":       accountExistsEmail(testRecipient),
		"password changed":     passwordChangedEmail(testRecipient),
		"passwordless account": passwordlessAccountEmail(testRecipient),
	} {
		if msg.HTML != "" {
			t.Errorf("%s: has an HTML body", name)
		}
	}
}

// An account may have been created with Google, so the account-exists email
// can't send everyone to "Forgot password" (decision 020).
func TestAccountExistsEmailMentionsGoogle(t *testing.T) {
	if text := accountExistsEmail(testRecipient).Text; !strings.Contains(text, `"Continue with Google"`) {
		t.Errorf("account-exists email doesn't mention Google sign-in: %q", text)
	}
}

func TestPasswordlessAccountEmailOffersNoReset(t *testing.T) {
	text := passwordlessAccountEmail(testRecipient).Text
	if !strings.Contains(text, `"Continue with Google"`) {
		t.Errorf("passwordless email doesn't point to Google sign-in: %q", text)
	}
	if strings.Contains(text, "Forgot password") {
		t.Errorf("passwordless email points to a reset that can't work: %q", text)
	}
}

func TestVerificationEmailHeading(t *testing.T) {
	msg := verificationEmail(mustParseURL(t, "https://api.example.com"), testRecipient, "tok", time.Hour)
	h1 := regexp.MustCompile(`(?s)<h1[^>]*>([^<]*)</h1>`).FindAllStringSubmatch(msg.HTML, -1)
	if len(h1) != 1 || h1[0][1] != "Confirm your email address" {
		t.Errorf("HTML headings = %q, want one \"Confirm your email address\"", h1)
	}
	// The text body keeps its greeting.
	if !strings.HasPrefix(msg.Text, "Welcome to VocaTogether!\n") {
		t.Errorf("text body changed: %q", strings.SplitN(msg.Text, "\n", 2)[0])
	}
}
