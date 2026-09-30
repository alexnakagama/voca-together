package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var _ Sender = (*ResendSender)(nil)

const (
	testAPIKey = "re_TestKey_SECRET123"
	testFrom   = "VocaTogether <no-reply@mail.example.com>"
)

// newTestResend returns a sender whose endpoint is an httptest server running
// handler, keeping the production client (timeouts, redirect policy). Tests
// never reach api.resend.com.
func newTestResend(t *testing.T, handler http.HandlerFunc) (*ResendSender, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	s, err := NewResendSender(testAPIKey, testFrom)
	if err != nil {
		t.Fatal(err)
	}
	s.endpoint = srv.URL + "/emails"
	return s, &hits
}

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestResendSenderSendsRequest(t *testing.T) {
	var got *http.Request
	var body map[string]any
	s, _ := newTestResend(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		respond(http.StatusOK, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)(w, r)
	})
	msg := validMessage()
	msg.HTML = secretHTML

	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.Method != http.MethodPost || got.URL.Path != "/emails" {
		t.Errorf("request = %s %s, want POST /emails", got.Method, got.URL.Path)
	}
	for header, want := range map[string]string{
		"Authorization": "Bearer " + testAPIKey,
		"Content-Type":  "application/json",
		"User-Agent":    "vocatogether-backend",
	} {
		if v := got.Header.Get(header); v != want {
			t.Errorf("%s = %q, want %q", header, v, want)
		}
	}
	want := map[string]any{
		"from":    testFrom,
		"to":      []any{msg.To},
		"subject": msg.Subject,
		"text":    msg.Text,
		"html":    msg.HTML,
	}
	if fmt.Sprint(body) != fmt.Sprint(want) {
		t.Errorf("body = %v\nwant   %v", body, want)
	}
}

func TestResendSenderOmitsEmptyHTML(t *testing.T) {
	var body map[string]any
	s, _ := newTestResend(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		respond(http.StatusOK, `{"id":"x"}`)(w, r)
	})
	if err := s.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, ok := body["html"]; ok {
		t.Errorf("text-only message sent an html field: %v", body)
	}
	if body["text"] != secretBody {
		t.Errorf("text = %v, want the message text", body["text"])
	}
}

// requireSafeError checks that an error, which auth logs, carries none of the
// secrets or personal data involved in the send.
func requireSafeError(t *testing.T, err error, extra ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want an error")
	}
	for _, forbidden := range append([]string{testAPIKey, "SECRET", "ana@example.com"}, extra...) {
		if strings.Contains(err.Error(), forbidden) {
			t.Errorf("error %q contains %q", err, forbidden)
		}
	}
}

func TestResendSenderMapsProviderErrors(t *testing.T) {
	// Provider messages may repeat request data; they must never be copied.
	const providerMessage = "Invalid `to` field ana@example.com, see SECRET-TOKEN"
	tests := []struct {
		status   int
		name     string
		wantName bool
	}{
		{http.StatusBadRequest, "validation_error", true},
		{http.StatusUnauthorized, "missing_api_key", true},
		{http.StatusForbidden, "validation_error", true},
		{http.StatusUnprocessableEntity, "invalid_parameter", true},
		{http.StatusTooManyRequests, "rate_limit_exceeded", true},
		{http.StatusInternalServerError, "application_error", true},
		{http.StatusServiceUnavailable, "service_unavailable", true},
		// Unexpected names are dropped rather than logged.
		{http.StatusBadRequest, "Bearer re_TestKey_SECRET123", false},
		{http.StatusBadRequest, "ana@example.com", false},
		{http.StatusBadRequest, "<script>", false},
		{http.StatusBadRequest, strings.Repeat("a", 65), false},
		{http.StatusBadRequest, "", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d %q", tt.status, tt.name), func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"statusCode": tt.status, "name": tt.name, "message": providerMessage})
			s, _ := newTestResend(t, respond(tt.status, string(body)))

			err := s.Send(context.Background(), validMessage())
			requireSafeError(t, err, "Invalid `to` field")
			want := fmt.Sprintf("email: resend: status %d", tt.status)
			if tt.wantName {
				want += " (" + tt.name + ")"
			}
			if err.Error() != want {
				t.Errorf("err = %q, want %q", err, want)
			}
		})
	}
}

func TestResendSenderHandlesUnexpectedErrorBodies(t *testing.T) {
	tests := map[string]string{
		"not JSON": "<html>Bad Gateway ana@example.com</html>",
		"empty":    "",
		// Truncated by the read limit, so it no longer parses.
		"oversized": `{"name":"validation_error","message":"` + strings.Repeat("x", 2*maxResendResponseBytes) + `"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			s, _ := newTestResend(t, respond(http.StatusBadGateway, body))
			err := s.Send(context.Background(), validMessage())
			requireSafeError(t, err)
			if want := "email: resend: status 502"; err.Error() != want {
				t.Errorf("err = %q, want %q", err, want)
			}
		})
	}
}

func TestResendSenderAcceptsSuccessWithUnexpectedBody(t *testing.T) {
	s, _ := newTestResend(t, respond(http.StatusOK, strings.Repeat("x", 2*maxResendResponseBytes)))
	if err := s.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestResendSenderDoesNotFollowRedirects(t *testing.T) {
	var elsewhere atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
	}))
	defer target.Close()

	s, _ := newTestResend(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/emails", http.StatusTemporaryRedirect)
	})

	err := s.Send(context.Background(), validMessage())
	requireSafeError(t, err)
	if want := "email: resend: status 307"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("redirect was followed: target got %d requests", n)
	}
}

// waitForClientToGiveUp blocks until the client disconnects. The server only
// notices a disconnect once the request body has been read.
func waitForClientToGiveUp(r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	<-r.Context().Done()
}

func TestResendSenderHonorsDeadlineDuringRequest(t *testing.T) {
	s, _ := newTestResend(t, func(w http.ResponseWriter, r *http.Request) {
		waitForClientToGiveUp(r) // never answers
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := s.Send(ctx, validMessage())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	requireSafeError(t, err)
}

func TestResendSenderHonorsCancellationDuringRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, _ := newTestResend(t, func(w http.ResponseWriter, r *http.Request) {
		cancel()
		waitForClientToGiveUp(r)
	})

	if err := s.Send(ctx, validMessage()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestResendSenderSendsNothingForDeadContextOrInvalidMessage(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	invalid := validMessage()
	invalid.Subject = "Hi\r\nBcc: evil@example.com"

	tests := []struct {
		name string
		ctx  context.Context
		msg  Message
		want error
	}{
		{"cancelled", cancelled, validMessage(), context.Canceled},
		{"expired", expired, validMessage(), context.DeadlineExceeded},
		{"invalid message", context.Background(), invalid, ErrInvalidMessage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, hits := newTestResend(t, respond(http.StatusOK, `{"id":"x"}`))
			if err := s.Send(tt.ctx, tt.msg); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("server got %d requests, want 0", n)
			}
		})
	}
}

func TestResendSenderTransportErrorIsSafe(t *testing.T) {
	s, _ := newTestResend(t, respond(http.StatusOK, `{}`))
	s.endpoint = "http://127.0.0.1:1/emails" // nothing listens on port 1
	requireSafeError(t, s.Send(context.Background(), validMessage()))
}

func TestResendClientIsBounded(t *testing.T) {
	s, err := NewResendSender(testAPIKey, testFrom)
	if err != nil {
		t.Fatal(err)
	}
	if s.endpoint != "https://api.resend.com/emails" {
		t.Errorf("endpoint = %q", s.endpoint)
	}
	if s.client.Timeout != resendTimeout || s.client.Timeout <= 0 {
		t.Errorf("client timeout = %v, want %v", s.client.Timeout, resendTimeout)
	}
	tr, ok := s.client.Transport.(*http.Transport)
	if !ok || tr.TLSHandshakeTimeout <= 0 || tr.ResponseHeaderTimeout <= 0 {
		t.Errorf("transport timeouts not set: %#v", s.client.Transport)
	}
	if s.client.CheckRedirect == nil {
		t.Error("redirect policy not set")
	}
}

func TestNewResendSenderValidatesAPIKey(t *testing.T) {
	for _, key := range []string{"", "re_", "abc", "RE_abc", "re_abc def", "re_abc\n", "re_abc\r\nX-Evil: 1", "re_ñ"} {
		_, err := NewResendSender(key, testFrom)
		if err == nil {
			t.Errorf("key %q accepted", key)
			continue
		}
		// "re_" alone is part of the error's own hint.
		if len(key) > len("re_") && strings.Contains(err.Error(), key) {
			t.Errorf("error echoes the key: %q", err)
		}
	}
}

func TestNewResendSenderValidatesSender(t *testing.T) {
	valid := map[string]string{
		"no-reply@mail.example.com":                   "no-reply@mail.example.com",
		"VocaTogether <no-reply@mail.example.com>":    "VocaTogether <no-reply@mail.example.com>",
		"Voca Together 2 <no-reply@mail.example.com>": "Voca Together 2 <no-reply@mail.example.com>",
		"  no-reply@example.com  ":                    "no-reply@example.com",
		"\tVocaTogether <no-reply@example.com>\n":     "VocaTogether <no-reply@example.com>",
	}
	for from, want := range valid {
		s, err := NewResendSender(testAPIKey, from)
		if err != nil {
			t.Errorf("%q rejected: %v", from, err)
			continue
		}
		if s.from != want {
			t.Errorf("from %q stored as %q, want %q", from, s.from, want)
		}
	}

	invalid := []string{
		"",
		"   ",
		"not-an-address",
		"no-reply@localhost",
		"no-reply@[127.0.0.1]",
		"no-reply@example.com, other@example.com",
		"no-reply@example.com\r\nBcc: evil@example.com",
		"no-reply@example.com\nBcc: evil@example.com",
		"Voca <no-reply@exämple.com>",
		"Vöca <no-reply@example.com>",
		"no-reply@.example.com",
		"no-reply@example.com.",
		// Parseable, but not canonical.
		"no-reply@example.com (VocaTogether)",     // comment
		"VocaTogether <no-reply@example.com> (x)", // trailing comment
		`"VocaTogether" <no-reply@example.com>`,   // quoted display name
		`"Voca, Together" <no-reply@example.com>`, // quoted, with punctuation
		"Voca-Together <no-reply@example.com>",    // punctuation in the name
		"Voca.Together <no-reply@example.com>",    // punctuation in the name
		`"no reply"@example.com`,                  // quoted local part
		"<no-reply@example.com>",                  // angle brackets without a name
		"Voca  Together <no-reply@example.com>",   // repeated space
		"VocaTogether<no-reply@example.com>",      // missing space
		"VocaTogether <no-reply@example.com >",    // space inside the brackets
		"=?utf-8?q?Voca?= <no-reply@example.com>", // encoded word
	}
	for _, from := range invalid {
		if _, err := NewResendSender(testAPIKey, from); err == nil {
			t.Errorf("%q accepted", from)
		}
	}
}

func TestResendSenderIsRedactedWhenFormattedOrLogged(t *testing.T) {
	s, err := NewResendSender(testAPIKey, testFrom)
	if err != nil {
		t.Fatal(err)
	}

	formatted := fmt.Sprintf("%v | %+v | %s | %#v | %v | %#v", s, s, s, s, *s, *s)
	if strings.Contains(formatted, "SECRET") {
		t.Errorf("formatted sender leaks the API key: %q", formatted)
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("sender", "s", s, "v", *s)
	slog.New(slog.NewTextHandler(&buf, nil)).Info("sender", "s", s, "v", *s)
	if strings.Contains(buf.String(), "SECRET") {
		t.Errorf("slog output leaks the API key: %s", buf.String())
	}
}
