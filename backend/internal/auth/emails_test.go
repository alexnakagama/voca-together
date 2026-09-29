package auth

import (
	"context"
	"net/url"
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
		"account exists":   accountExistsEmail(testRecipient),
		"password changed": passwordChangedEmail(testRecipient),
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
