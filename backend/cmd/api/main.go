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
	"vocatogether/backend/internal/avatar"
	"vocatogether/backend/internal/config"
	"vocatogether/backend/internal/db"
	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/googleid"
	"vocatogether/backend/internal/language"
	"vocatogether/backend/internal/profile"
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
	// Production defaults: Google's key set, the hardened client, the real clock.
	google, err := newGoogleVerifier(cfg, logger, googleid.Options{})
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

	authSvc := auth.NewService(pool, sender, baseURL, logger, auth.NewAccountLimits(logger), google)
	// Runs after the server has shut down: lets in-flight emails finish.
	defer authSvc.Wait()
	profileSvc := profile.NewService(pool, logger)
	languageSvc := language.NewService(pool, logger)
	avatarSvc := avatar.NewService(pool, logger)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.New(logger, authSvc, profileSvc, languageSvc, avatarSvc, serverOptions(cfg, logger)),
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

// serverOptions returns the router's options for cfg, with every production
// rate limit in place: the zero value of a limit disables it, so leaving one
// out here would silently run without it.
func serverOptions(cfg config.Config, logger *slog.Logger) server.Options {
	return server.Options{
		TrustedProxyHops: cfg.TrustedProxyHops,
		HSTS:             cfg.IsProduction(),
		IPLimits:         server.NewIPLimits(logger),
		UserLimits:       server.NewUserLimits(logger),
	}
}

// newEmailSender returns the email.Sender for cfg and logs which provider it
// chose, by name only (never the API key or the sender address):
//   - production: Resend, always; there is no fallback to LogSender, which
//     logs live tokens (decision 007);
//   - development: Resend when RESEND_API_KEY is set, LogSender otherwise;
//   - test: LogSender, whatever the Resend settings, so tests never send.
func newEmailSender(cfg config.Config, logger *slog.Logger) (email.Sender, error) {
	key := cfg.ResendAPIKey.Reveal()
	useResend := cfg.Env != "test" && (cfg.IsProduction() || key != "")
	if !useResend {
		logger.Info("email sender", "provider", "log")
		return email.NewLogSender(logger), nil
	}

	// config.Load already requires both in production; check again rather
	// than rely on it, since the alternative is starting without email.
	if key == "" {
		return nil, errors.New("no email sender for production: RESEND_API_KEY is required")
	}
	sender, err := email.NewResendSender(key, cfg.EmailFrom) // errors never echo the key
	if err != nil {
		return nil, err
	}
	logger.Info("email sender", "provider", "resend")
	return sender, nil
}

// newGoogleVerifier returns the Google ID-token verifier for cfg and logs
// whether Google sign-in is enabled (never the client ID):
//   - production: a verifier for GOOGLE_CLIENT_ID, always; there is no
//     fallback to disabled;
//   - development: a verifier when GOOGLE_CLIENT_ID is set, nil otherwise
//     (disabled: every Google sign-in is rejected as not configured);
//   - test: nil, whatever the setting, so a test binary never reaches Google.
//
// Disabled is an untyped nil: a nil *googleid.TokenVerifier in the interface
// would look configured to auth.Service. opts is googleid.Options{} in run;
// tests point it at a local key server. Construction does no network work:
// keys are fetched on first use, so startup doesn't depend on Google.
func newGoogleVerifier(cfg config.Config, logger *slog.Logger, opts googleid.Options) (googleid.Verifier, error) {
	id := cfg.GoogleClientID
	if cfg.Env == "test" || (id == "" && !cfg.IsProduction()) {
		logger.Info("google sign-in", "status", "disabled")
		return nil, nil
	}

	// config.Load already checks both; check again rather than rely on it,
	// since the alternative is production without Google sign-in or with an
	// audience nothing can match. Errors never echo the value.
	if id == "" {
		return nil, errors.New("no Google verifier for production: GOOGLE_CLIENT_ID is required")
	}
	if !googleid.ValidClientID(id) {
		return nil, errors.New("no Google verifier: GOOGLE_CLIENT_ID is invalid")
	}
	verifier, err := googleid.NewTokenVerifier([]string{id}, opts) // errors never echo the audience
	if err != nil {
		return nil, err
	}
	logger.Info("google sign-in", "status", "enabled")
	return verifier, nil
}
