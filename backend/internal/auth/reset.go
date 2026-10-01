package auth

import (
	"context"
	"fmt"
	"time"
)

// passwordResetTokenTTL is short: a reset link grants control of the account.
const passwordResetTokenTTL = 30 * time.Minute

// ForgotPassword emails a password reset link if the address belongs to an
// account, verified or not, which invalidates any previous reset link. An
// account without a password (created with Google) gets no link: it can't be
// recovered through its mailbox, so its owner is told to use Google instead
// (decision 020). For unknown addresses it does nothing. All return nil, so
// the result doesn't reveal which addresses have accounts; only an invalid
// address returns a *ValidationError. No argon2 work is done in any case.
func (s *Service) ForgotPassword(ctx context.Context, emailInput string) error {
	addr, err := NormalizeEmail(emailInput)
	if err != nil {
		return validationError(err)
	}
	// Unknown addresses consume too: otherwise a 429 would mark real accounts.
	if err := allowAccount(s.limits.Mail, addr); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("auth: forgot password: %w", err)
	}

	token := NewToken("")
	userID, r, err := issuePasswordResetToken(ctx, s.pool, addr, token.Hash, passwordResetTokenTTL)
	if err != nil {
		return fmt.Errorf("auth: forgot password: %w", err)
	}
	switch r {
	case resetNoAccount:
		// No ids and no address: an unknown address isn't linked to anything.
		s.logger.InfoContext(ctx, "auth: password reset request had no effect")
	case resetPasswordless:
		s.logger.InfoContext(ctx, "auth: password reset requested for passwordless account", "user_id", userID)
		s.sendInBackground(ctx, "passwordless_account", passwordlessAccountEmail(addr))
	case resetIssued:
		s.logger.InfoContext(ctx, "auth: password reset requested", "user_id", userID)
		s.sendInBackground(ctx, "password_reset", passwordResetEmail(s.baseURL, addr, token.Raw, passwordResetTokenTTL))
	}
	return nil
}

// ResetPassword consumes a password reset token and sets its owner's new
// password (decision 017). In the same transaction it revokes all of the
// owner's sessions, so every access and refresh token issued before stops
// working, and deletes the owner's other unused one-time tokens. It does not
// log the user in. The owner is emailed that the password changed.
//
// Every unusable token (malformed, unknown, expired, used, replaced, for
// another purpose, or of an account without a password) gets the same
// *ValidationError as VerifyEmail, and a
// password that fails the policy gets its own; neither consumes the token.
// Malformed tokens and policy failures that don't need the account are
// rejected before any database access, and argon2 runs only once the token
// is known to be usable.
func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	var tokenErr error
	if !wellFormedToken(rawToken, "") {
		tokenErr = ErrTokenInvalid
	}
	// Without the email, only the same-as-email rule can't fail here.
	if err := validationError(tokenErr, ValidatePassword(newPassword, "")); err != nil {
		reason := "invalid_password"
		if tokenErr != nil {
			reason = "malformed"
		}
		s.logResetFailed(ctx, reason)
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}

	tokenHash := HashToken(rawToken)
	addr, found, err := findPasswordResetOwner(ctx, s.pool, tokenHash)
	if err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}
	if !found {
		s.logResetFailed(ctx, "invalid_token")
		return validationError(ErrTokenInvalid)
	}
	if err := ValidatePassword(newPassword, addr); err != nil {
		s.logResetFailed(ctx, "invalid_password")
		return validationError(err)
	}

	// No transaction or connection is held during the argon2 work.
	newHash, err := s.hashPassword(ctx, newPassword)
	if err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}

	r, err := resetPassword(ctx, s.pool, tokenHash, newHash)
	if err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}
	if !r.ok {
		// Used, replaced or expired since the lookup above.
		s.logResetFailed(ctx, "invalid_token")
		return validationError(ErrTokenInvalid)
	}
	s.logger.InfoContext(ctx, "auth: password reset succeeded",
		"user_id", r.userID, "sessions_revoked", r.sessionsRevoked)
	s.sendInBackground(ctx, "password_changed", passwordChangedEmail(r.email))
	return nil
}

// WellFormedResetToken reports whether raw has the shape of a password reset
// token. It only checks the public format, never the database, so the reset
// page can refuse to render a form for junk without revealing anything.
func WellFormedResetToken(raw string) bool {
	return wellFormedToken(raw, "")
}

// logResetFailed records a rejected reset. No ids: an unusable token isn't
// linked to an account. Tokens, passwords and emails are never logged.
func (s *Service) logResetFailed(ctx context.Context, reason string) {
	s.logger.InfoContext(ctx, "auth: password reset failed", "reason", reason)
}
