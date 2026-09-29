package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"
)

var _ Sender = (*LogSender)(nil)

func newTestLogSender() (*LogSender, *bytes.Buffer) {
	var buf bytes.Buffer
	return NewLogSender(slog.New(slog.NewJSONHandler(&buf, nil))), &buf
}

// Local development relies on the log to see verification and reset links.
func TestLogSenderLogsFullMessage(t *testing.T) {
	s, buf := newTestLogSender()
	msg := validMessage()

	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not one JSON record: %v\n%s", err, buf.String())
	}
	for key, want := range map[string]string{"to": msg.To, "subject": msg.Subject, "text": msg.Text} {
		if entry[key] != want {
			t.Errorf("log %q = %v, want %q", key, entry[key], want)
		}
	}
}

func TestLogSenderHonorsCancelledContext(t *testing.T) {
	s, buf := newTestLogSender()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.Send(ctx, validMessage()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("cancelled send logged: %s", buf.String())
	}
}

func TestLogSenderHonorsExpiredDeadline(t *testing.T) {
	s, buf := newTestLogSender()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	if err := s.Send(ctx, validMessage()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expired send logged: %s", buf.String())
	}
}

func TestLogSenderRejectsInvalidMessage(t *testing.T) {
	s, buf := newTestLogSender()
	msg := validMessage()
	msg.To = ""

	if err := s.Send(context.Background(), msg); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("err = %v, want ErrInvalidMessage", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("invalid message logged: %s", buf.String())
	}
}
