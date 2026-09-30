// Package email delivers email messages. It knows nothing about what the
// messages mean: callers (such as auth) own their content.
package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// Sender delivers one email.
//
// Implementations must honor ctx cancellation and deadlines; the caller owns
// the timeout. Errors must never include the message body, which may carry
// secrets such as one-time links, nor the recipient address (personal data):
// callers log them.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Message is a provider-independent email. The sender address is configured
// on the Sender, not chosen by callers.
//
// Text is required and is the canonical body: every message has a plain-text
// part. HTML is an optional alternative rendering of the same content for
// clients that display it; senders that can't deliver HTML send Text alone.
// Both bodies may carry secrets such as one-time links.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// ErrInvalidMessage reports a message that must not be sent: a missing field,
// or a line break in a header value (which could inject extra headers).
var ErrInvalidMessage = errors.New("email: invalid message")

// validate checks the message at the package boundary rather than trusting
// callers. Errors name the problem but never echo field values.
func (m Message) validate() error {
	switch {
	case m.To == "":
		return fmt.Errorf("%w: empty recipient", ErrInvalidMessage)
	case m.Subject == "":
		return fmt.Errorf("%w: empty subject", ErrInvalidMessage)
	case m.Text == "":
		return fmt.Errorf("%w: empty body", ErrInvalidMessage)
	case strings.ContainsAny(m.To, "\r\n"):
		return fmt.Errorf("%w: line break in recipient", ErrInvalidMessage)
	case strings.ContainsAny(m.Subject, "\r\n"):
		return fmt.Errorf("%w: line break in subject", ErrInvalidMessage)
	}
	return nil
}

// String, GoString and LogValue omit both bodies (they may contain live
// tokens) and the recipient (personal data), so a Message printed or logged by
// accident exposes neither. String shows only whether an HTML part exists.
func (m Message) String() string {
	html := ""
	if m.HTML != "" {
		html = ", HTML: [REDACTED]"
	}
	return fmt.Sprintf("email.Message{Subject: %q, To: [REDACTED], Text: [REDACTED]%s}", m.Subject, html)
}

func (m Message) GoString() string { return m.String() }

func (m Message) LogValue() slog.Value {
	return slog.GroupValue(slog.String("subject", m.Subject))
}

// checkContext returns the context's error, wrapped so that errors.Is still
// matches context.Canceled and context.DeadlineExceeded.
func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("email: send: %w", err)
	}
	return nil
}
