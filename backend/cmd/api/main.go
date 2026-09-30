package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/config"
	"vocatogether/backend/internal/db"
	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	sender, err := newEmailSender(cfg, logger)
	if err != nil {
		return err
	}
	baseURL, err := url.Parse(cfg.AppBaseURL) // already validated by config.Load
	if err != nil {
		return err
	}

	startCtx, cancelStart := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStart()
	pool, err := db.Connect(startCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(startCtx, pool); err != nil {
		return err
	}

	authSvc := auth.NewService(pool, sender, baseURL, logger, auth.NewAccountLimits(logger))
	// Runs after the server has shut down: lets in-flight emails finish.
	defer authSvc.Wait()

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: server.New(logger, authSvc, server.Options{
			TrustedProxyHops: cfg.TrustedProxyHops,
			HSTS:             cfg.IsProduction(),
			IPLimits:         server.NewIPLimits(logger),
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		// Auth requests need a few hundred bytes of headers; the default is 1 MiB.
		MaxHeaderBytes: 32 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Deletes dead sessions and one-time tokens; stops with ctx, and is
	// waited for before the pool closes.
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		authSvc.RunCleanup(ctx, time.Minute, time.Hour)
	}()
	defer func() {
		stop() // also on the listen-error path, where no signal ended ctx
		<-cleanupDone
	}()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return srv.Shutdown(shutdownCtx)
}

// newEmailSender returns the email.Sender for cfg. Only the development
// LogSender exists so far. It logs live tokens, so production refuses to start
// rather than fall back to it (decision 007).
func newEmailSender(cfg config.Config, logger *slog.Logger) (email.Sender, error) {
	if cfg.IsProduction() {
		return nil, errors.New("no email sender for production: LogSender logs live tokens and is not allowed")
	}

	return email.NewLogSender(logger), nil
}
