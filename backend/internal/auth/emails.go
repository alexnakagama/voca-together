package auth

import (
	"fmt"
	"net/url"
	"time"

	"vocatogether/backend/internal/email"
)

// Paths of the backend-served pages that emailed links open.
const (
	verifyEmailPath   = "/verify-email"
	resetPasswordPath = "/reset-password"
)

// The builders below take `to` already normalized by NormalizeEmail and a raw
// one-time token. Messages carrying a token are secrets: never log them.

func verificationEmail(baseURL *url.URL, to, rawToken string, ttl time.Duration) email.Message {
	return email.Message{
		To:      to,
		Subject: "Verify your VocaTogether email address",
		Text: fmt.Sprintf(`Welcome to VocaTogether!

Confirm your email address by opening this link:

%s

The link expires in %s and can be used only once.

If you didn't create a VocaTogether account, you can ignore this email.
`, tokenLink(baseURL, verifyEmailPath, rawToken), humanDuration(ttl)),
	}
}

func passwordResetEmail(baseURL *url.URL, to, rawToken string, ttl time.Duration) email.Message {
	return email.Message{
		To:      to,
		Subject: "Reset your VocaTogether password",
		Text: fmt.Sprintf(`We received a request to reset the password of your VocaTogether account.

Choose a new password by opening this link:

%s

The link expires in %s and can be used only once. After the reset you will be
signed out on all devices.

If you didn't ask to reset your password, you can ignore this email. Your
password will not change.
`, tokenLink(baseURL, resetPasswordPath, rawToken), humanDuration(ttl)),
	}
}

// accountExistsEmail is sent when someone registers with an address that
// already has an account, so registration never reveals which addresses exist.
func accountExistsEmail(to string) email.Message {
	return email.Message{
		To:      to,
		Subject: "You already have a VocaTogether account",
		Text: `Someone, hopefully you, tried to create a VocaTogether account with this
email address, but an account already exists.

If it was you, open the app and log in. If you don't remember your password,
use "Forgot password" on the login screen.

If it wasn't you, you can ignore this email. Your account has not changed.
`,
	}
}

func passwordChangedEmail(to string) email.Message {
	return email.Message{
		To:      to,
		Subject: "Your VocaTogether password was changed",
		Text: `The password of your VocaTogether account was just changed, and all devices
were signed out.

If you did this, no action is needed.

If you didn't, open the app right away and use "Forgot password" on the login
screen to choose a new password.
`,
	}
}

// tokenLink returns baseURL + path with the token as the only query
// parameter. url.Values escapes the token, so its contents can't add
// parameters, change the path, or start a fragment; the base URL's own query
// and fragment are dropped. baseURL itself is not modified.
func tokenLink(baseURL *url.URL, path, rawToken string) string {
	link := baseURL.JoinPath(path)
	link.RawQuery = url.Values{"token": {rawToken}}.Encode()
	link.Fragment = ""
	link.RawFragment = ""
	return link.String()
}

// humanDuration formats token lifetimes for email text: whole hours as
// hours, anything else in minutes.
func humanDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return plural(int(d/time.Hour), "hour")
	}
	return plural(int(d/time.Minute), "minute")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
