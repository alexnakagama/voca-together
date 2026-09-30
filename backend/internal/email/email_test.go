package email

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const (
	secretBody = "Open https://example.com/verify-email?token=SECRET-TOKEN to continue."
	secretHTML = `<a href="https://example.com/verify-email?token=SECRET-HTML-TOKEN">Verify</a>`
)

func validMessage() Message {
	return Message{To: "ana@example.com", Subject: "Hello", Text: secretBody}
}

func TestValidateAcceptsValidMessage(t *testing.T) {
	if err := validMessage().validate(); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}
}

func TestValidateAllowsNewlinesInBody(t *testing.T) {
	msg := validMessage()
	msg.Text = "line one\nline two\r\n\r\nline four\n"
	if err := msg.validate(); err != nil {
		t.Fatalf("multi-line body rejected: %v", err)
	}
}

func TestValidateAcceptsHTMLAlternative(t *testing.T) {
	msg := validMessage()
	msg.HTML = secretHTML
	if err := msg.validate(); err != nil {
		t.Fatalf("message with HTML rejected: %v", err)
	}
}

func TestValidateRejectsInvalidMessages(t *testing.T) {
	tests := map[string]func(*Message){
		"empty recipient":               func(m *Message) { m.To = "" },
		"empty subject":                 func(m *Message) { m.Subject = "" },
		"empty body":                    func(m *Message) { m.Text = "" },
		"HTML without text":             func(m *Message) { m.Text = ""; m.HTML = secretHTML },
		"LF in recipient":               func(m *Message) { m.To = "ana@example.com\nBcc: evil@example.com" },
		"CR in recipient":               func(m *Message) { m.To = "ana@example.com\rBcc: evil@example.com" },
		"CRLF in recipient":             func(m *Message) { m.To = "ana@example.com\r\nBcc: evil@example.com" },
		"LF in subject":                 func(m *Message) { m.Subject = "Hello\nBcc: evil@example.com" },
		"CR in subject":                 func(m *Message) { m.Subject = "Hello\rBcc: evil@example.com" },
		"CRLF in subject":               func(m *Message) { m.Subject = "Hello\r\nBcc: evil@example.com" },
		"trailing newline in recipient": func(m *Message) { m.To = "ana@example.com\n" },
		"trailing newline in subject":   func(m *Message) { m.Subject = "Hello\n" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			msg := validMessage()
			mutate(&msg)
			err := msg.validate()
			if !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("err = %v, want ErrInvalidMessage", err)
			}
			// Errors may be logged: they must not echo message contents.
			if strings.Contains(err.Error(), "evil@example.com") || strings.Contains(err.Error(), "SECRET-") {
				t.Errorf("error leaks message contents: %q", err)
			}
		})
	}
}

// Both bodies contain live tokens; printing or logging a whole Message by
// accident must reveal neither, nor the recipient.
func TestMessageIsRedactedWhenFormattedOrLogged(t *testing.T) {
	msg := validMessage()
	msg.HTML = secretHTML
	leaks := func(s string) bool {
		return strings.Contains(s, "SECRET-") || strings.Contains(s, msg.To)
	}

	formatted := fmt.Sprintf("%v | %+v | %s | %#v", msg, msg, msg, msg)
	if leaks(formatted) {
		t.Errorf("formatted message leaks body or recipient: %q", formatted)
	}
	if !strings.Contains(formatted, msg.Subject) {
		t.Errorf("formatted message should still show the subject for debugging: %q", formatted)
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("sending", "msg", msg)
	if leaks(buf.String()) {
		t.Errorf("slog output leaks body or recipient: %s", buf.String())
	}
	var text bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("sending", "msg", msg)
	if leaks(text.String()) {
		t.Errorf("slog text output leaks body or recipient: %s", text.String())
	}
}

// Redaction must not depend on which bodies are set: the placeholder shows
// only whether an HTML part exists.
func TestMessageStringMarksHTMLPresence(t *testing.T) {
	msg := validMessage()
	if strings.Contains(msg.String(), "HTML") {
		t.Errorf("text-only message mentions HTML: %q", msg.String())
	}
	msg.HTML = secretHTML
	if !strings.Contains(msg.String(), "HTML: [REDACTED]") {
		t.Errorf("message with HTML does not mark it: %q", msg.String())
	}
}
