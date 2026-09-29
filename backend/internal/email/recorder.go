package email

import (
	"context"
	"sync"
)

// Recorder is a Sender for tests: it stores messages in memory and never
// touches the network. The zero value is ready to use, and it is safe for
// concurrent use because auth sends email from background goroutines.
//
// Like a real provider, it rejects cancelled contexts, so tests catch code
// that sends with a request context that has already ended.
type Recorder struct {
	mu       sync.Mutex
	err      error
	messages []Message
}

// Send records msg, or returns an error without recording it: the context's
// error, ErrInvalidMessage, or the error set with SetError.
func (r *Recorder) Send(ctx context.Context, msg Message) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := msg.validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.messages = append(r.messages, msg)
	return nil
}

// SetError makes subsequent sends fail with err, simulating a provider
// failure. SetError(nil) restores normal behavior.
func (r *Recorder) SetError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// Messages returns a copy of the recorded messages, oldest first.
func (r *Recorder) Messages() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Message, len(r.messages))
	copy(out, r.messages)
	return out
}
