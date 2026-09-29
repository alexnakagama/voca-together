package email

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const secretBody = "Open https://example.com/verify-email?token=SECRET-TOKEN to continue."

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

func TestValidateRejectsInvalidMessages(t *testing.T) {
	tests := map[string]func(*Message){
		"empty recipient":               func(m *Message) { m.To = "" },
		"empty subject":                 func(m *Message) { m.Subject = "" },
		"empty body":                    func(m *Message) { m.Text = "" },
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
			if strings.Contains(err.Error(), "evil@example.com") || strings.Contains(err.Error(), "SECRET-TOKEN") {
				t.Errorf("error leaks message contents: %q", err)
			}
		})
	}
}

// A Message body contains live tokens; printing or logging a whole Message by
// accident must not reveal it.
func TestMessageIsRedactedWhenFormattedOrLogged(t *testing.T) {
	msg := validMessage()

	formatted := fmt.Sprintf("%v | %+v | %s | %#v", msg, msg, msg, msg)
	if strings.Contains(formatted, "SECRET-TOKEN") {
		t.Errorf("formatted message leaks body: %q", formatted)
	}
	if !strings.Contains(formatted, msg.Subject) {
		t.Errorf("formatted message should still show the subject for debugging: %q", formatted)
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("sending", "msg", msg)
	if strings.Contains(buf.String(), "SECRET-TOKEN") {
		t.Errorf("slog output leaks body: %s", buf.String())
	}
}
