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
	"net/mail"
	"regexp"
	"strings"
	"time"
)

const (
	resendEndpoint  = "https://api.resend.com/emails"
	resendUserAgent = "vocatogether-backend" // Resend rejects requests without a User-Agent
	// resendTimeout caps each request whatever the caller's context allows.
	resendTimeout = 10 * time.Second
	// maxResendResponseBytes bounds how much of a response is read. Success
	// bodies are a few bytes; error bodies only need their "name". Normal
	// responses are read to completion and can reuse the connection;
	// oversized responses are truncated and their connection is not reused.
	maxResendResponseBytes = 64 << 10
)

// resendErrorName matches the provider's documented error names
// ("validation_error", "rate_limit_exceeded", ...). Anything else is dropped
// rather than copied into our errors and logs.
var resendErrorName = regexp.MustCompile(`^[a-z_]{1,64}$`)

// ResendSender delivers messages through the Resend HTTP API
// (POST /emails), using only the standard library.
//
// It never logs, and its errors carry only the HTTP status and the provider's
// error name: never the API key, the recipient, the bodies, or the provider's
// free-text message. It doesn't retry: delivery is best effort (decision 018).
type ResendSender struct {
	apiKey   string // secret: never log it
	from     string
	endpoint string
	client   *http.Client
}

// NewResendSender returns a sender that authenticates with apiKey and sends
// from `from`, which must be on a domain verified in Resend. from must be in
// canonical form, "no-reply@mail.example.com" or
// "Name <no-reply@mail.example.com>" (see canonicalSender); surrounding space
// is ignored. Errors never echo apiKey.
func NewResendSender(apiKey, from string) (*ResendSender, error) {
	if !validResendAPIKey(apiKey) {
		return nil, errors.New("email: resend: invalid API key: want printable ASCII starting with re_")
	}
	sender, ok := canonicalSender(from)
	if !ok {
		return nil, errors.New(`email: resend: invalid sender address: want "local@domain" or "Name <local@domain>"`)
	}
	return &ResendSender{
		apiKey:   apiKey,
		from:     sender,
		endpoint: resendEndpoint,
		client:   newResendClient(),
	}, nil
}

func newResendClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = resendTimeout
	return &http.Client{
		Transport: transport,
		Timeout:   resendTimeout,
		// A redirect would re-send the message, and possibly the key, to
		// another URL. The API doesn't redirect; a 3xx is an error.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// validResendAPIKey checks the key's shape, not its validity. Printable ASCII
// without spaces also keeps it safe to place in a header.
func validResendAPIKey(key string) bool {
	if !strings.HasPrefix(key, "re_") || len(key) == len("re_") {
		return false
	}
	for i := 0; i < len(key); i++ {
		if c := key[i]; c <= ' ' || c > '~' {
			return false
		}
	}
	return true
}

// canonicalSender rebuilds from out of its parsed parts, as the bare
// "local@domain" or "Name <local@domain>" (the form Resend documents), and
// accepts it only if the trimmed input already is exactly that. So every
// other spelling net/mail accepts is rejected: comments, quoted display names
// or local parts, "<local@domain>" alone, repeated spaces. The display name may
// hold only ASCII letters, digits and single spaces; the domain must be a
// domain name (no IP literal, at least one dot).
func canonicalSender(from string) (string, bool) {
	from = strings.TrimSpace(from)
	for i := 0; i < len(from); i++ {
		if c := from[i]; c < ' ' || c > '~' {
			return "", false
		}
	}
	addr, err := mail.ParseAddress(from)
	if err != nil {
		return "", false
	}
	_, domain, _ := strings.Cut(addr.Address, "@")
	if !validSenderDomain(domain) {
		return "", false
	}

	canonical := addr.Address
	if addr.Name != "" {
		if !plainDisplayName(addr.Name) {
			return "", false
		}
		canonical = addr.Name + " <" + addr.Address + ">"
	}
	if from != canonical {
		return "", false
	}
	return canonical, true
}

func validSenderDomain(domain string) bool {
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	for i := 0; i < len(domain); i++ {
		c := domain[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

// plainDisplayName reports whether name needs no quoting: ASCII letters,
// digits and spaces only.
func plainDisplayName(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ' ') {
			return false
		}
	}
	return true
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html,omitempty"`
}

// Send delivers msg. The context bounds the whole request; a cancelled or
// expired context returns an error matching context.Canceled or
// context.DeadlineExceeded.
func (s *ResendSender) Send(ctx context.Context, msg Message) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := msg.validate(); err != nil {
		return err
	}

	body, err := json.Marshal(resendRequest{
		From:    s.from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		Text:    msg.Text,
		HTML:    msg.HTML,
	})
	if err != nil {
		return fmt.Errorf("email: resend: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("email: resend: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", resendUserAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		// Report the caller's cancellation or deadline as such.
		if ctxErr := checkContext(ctx); ctxErr != nil {
			return ctxErr
		}
		// *url.Error names only the fixed endpoint URL and the cause.
		return fmt.Errorf("email: resend: send: %w", err)
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResendResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if name := resendErrorNameOf(respBody); name != "" {
			return fmt.Errorf("email: resend: status %d (%s)", resp.StatusCode, name)
		}
		return fmt.Errorf("email: resend: status %d", resp.StatusCode)
	}
	// The status is what counts; a failed read of a success body (whose id we
	// don't use) doesn't make the send fail, but a cancellation still shows.
	if readErr != nil {
		if ctxErr := checkContext(ctx); ctxErr != nil {
			return ctxErr
		}
	}
	return nil
}

// resendErrorNameOf returns the "name" of a Resend error body if it has the
// documented shape, or "". The free-text "message" is never used: it may
// repeat request data.
func resendErrorNameOf(body []byte) string {
	var e struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(body, &e) != nil || !resendErrorName.MatchString(e.Name) {
		return ""
	}
	return e.Name
}

// String, GoString and LogValue never show the API key. Value receivers, so
// a copied ResendSender is redacted too.
func (s ResendSender) String() string {
	return fmt.Sprintf("email.ResendSender{From: %q, APIKey: [REDACTED]}", s.from)
}

func (s ResendSender) GoString() string { return s.String() }

func (s ResendSender) LogValue() slog.Value {
	return slog.GroupValue(slog.String("provider", "resend"), slog.String("from", s.from))
}
