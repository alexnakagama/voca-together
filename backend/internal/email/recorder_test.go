package email

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

var _ Sender = (*Recorder)(nil)

func TestRecorderRecordsInOrder(t *testing.T) {
	var r Recorder
	ctx := context.Background()
	first, second := validMessage(), validMessage()
	second.Subject = "Second"

	for _, m := range []Message{first, second} {
		if err := r.Send(ctx, m); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}

	got := r.Messages()
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("Messages() = %v, want [first second]", got)
	}
}

func TestRecorderMessagesReturnsCopy(t *testing.T) {
	var r Recorder
	if err := r.Send(context.Background(), validMessage()); err != nil {
		t.Fatal(err)
	}

	got := r.Messages()
	got[0].Subject = "tampered"
	_ = append(got, validMessage())

	again := r.Messages()
	if len(again) != 1 || again[0].Subject != validMessage().Subject {
		t.Fatalf("mutating the returned slice changed recorder state: %v", again)
	}
}

func TestRecorderEmpty(t *testing.T) {
	var r Recorder
	if got := r.Messages(); len(got) != 0 {
		t.Fatalf("new recorder has %d messages", len(got))
	}
}

func TestRecorderHonorsCancelledContext(t *testing.T) {
	var r Recorder
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Send(ctx, validMessage())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := len(r.Messages()); n != 0 {
		t.Fatalf("cancelled send recorded %d messages", n)
	}
}

func TestRecorderHonorsExpiredDeadline(t *testing.T) {
	var r Recorder
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := r.Send(ctx, validMessage())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if n := len(r.Messages()); n != 0 {
		t.Fatalf("expired send recorded %d messages", n)
	}
}

func TestRecorderRejectsInvalidMessage(t *testing.T) {
	var r Recorder
	msg := validMessage()
	msg.Subject = "Hi\r\nBcc: evil@example.com"

	if err := r.Send(context.Background(), msg); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("err = %v, want ErrInvalidMessage", err)
	}
	if n := len(r.Messages()); n != 0 {
		t.Fatalf("invalid message was recorded")
	}
}

func TestRecorderSimulatesProviderFailure(t *testing.T) {
	var r Recorder
	providerErr := errors.New("provider unavailable")
	r.SetError(providerErr)

	if err := r.Send(context.Background(), validMessage()); !errors.Is(err, providerErr) {
		t.Fatalf("err = %v, want the configured provider error", err)
	}
	if n := len(r.Messages()); n != 0 {
		t.Fatalf("failed send recorded %d messages", n)
	}

	r.SetError(nil)
	if err := r.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("after clearing the error, Send = %v", err)
	}
	if n := len(r.Messages()); n != 1 {
		t.Fatalf("recorded %d messages, want 1", n)
	}
}

// Auth sends email from background goroutines; run with -race.
func TestRecorderIsSafeForConcurrentUse(t *testing.T) {
	var r Recorder
	const senders = 100

	var wg sync.WaitGroup
	for i := range senders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg := validMessage()
			msg.Subject = fmt.Sprintf("message %d", i)
			if err := r.Send(context.Background(), msg); err != nil {
				t.Errorf("Send: %v", err)
			}
			_ = r.Messages()
			if i%10 == 0 {
				r.SetError(nil)
			}
		}()
	}
	wg.Wait()

	got := r.Messages()
	if len(got) != senders {
		t.Fatalf("recorded %d messages, want %d", len(got), senders)
	}
	seen := make(map[string]bool, senders)
	for _, m := range got {
		seen[m.Subject] = true
	}
	if len(seen) != senders {
		t.Fatalf("recorded %d distinct messages, want %d", len(seen), senders)
	}
}
