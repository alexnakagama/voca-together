package email

import (
	"context"
	"log/slog"
)

// LogSender writes messages to a logger instead of delivering them, so local
// development can open verification and reset links from the log.
//
// It logs message bodies, which contain live tokens: it is for development
// and tests only. The application must refuse to start in production with it.
type LogSender struct {
	logger *slog.Logger
}

func NewLogSender(logger *slog.Logger) *LogSender {
	return &LogSender{logger: logger}
}

func (s *LogSender) Send(ctx context.Context, msg Message) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := msg.validate(); err != nil {
		return err
	}
	s.logger.LogAttrs(ctx, slog.LevelInfo, "email not delivered (development log sender)",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("text", msg.Text),
	)
	return nil
}
