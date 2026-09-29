package main

import (
	"log/slog"
	"testing"

	"vocatogether/backend/internal/config"
	"vocatogether/backend/internal/email"
)

func TestNewEmailSenderRefusesLogSenderInProduction(t *testing.T) {
	sender, err := newEmailSender(config.Config{Env: "production"}, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatalf("production got sender %T, want an error", sender)
	}
}

func TestNewEmailSenderUsesLogSenderOutsideProduction(t *testing.T) {
	for _, env := range []string{"development", "test"} {
		sender, err := newEmailSender(config.Config{Env: env}, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("%s: %v", env, err)
		}
		if _, ok := sender.(*email.LogSender); !ok {
			t.Errorf("%s: sender = %T, want *email.LogSender", env, sender)
		}
	}
}
