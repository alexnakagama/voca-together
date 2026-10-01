package auth

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/url"
	"time"

	"vocatogether/backend/internal/email"
)

// Paths of the backend-served pages that emailed links open.
const (
	verifyEmailPath   = "/verify-email"
	resetPasswordPath = "/reset-password"
)

//go:embed templates/action_email.html
var emailTemplates embed.FS

// actionEmailHTML renders an email built around one link: a heading, intro
// paragraphs, one button, the expiry, the link again as visible text, and
// closing paragraphs. Table layout with inline styles only; no scripts,
// images or other external resources.
var actionEmailHTML = template.Must(template.ParseFS(emailTemplates, "templates/action_email.html"))

// actionEmail is the data for actionEmailHTML. Every field is fixed text from
// this package except Link, which comes from tokenLink; html/template escapes
// all of them for their context, so none can add markup.
type actionEmail struct {
	Subject string
	Heading string
	Intro   []string
	Button  string
	Link    string
	Expiry  string
	Outro   []string
}

// html renders e. The text body is the canonical one and HTML is optional, so
// if rendering ever failed the message would go out as text only; the error
// is dropped rather than logged, since the data holds a live token.
func (e actionEmail) html() string {
	var buf bytes.Buffer
	if err := actionEmailHTML.Execute(&buf, e); err != nil {
		return ""
	}
	return buf.String()
}

// The builders below take `to` already normalized by NormalizeEmail and a raw
// one-time token. Messages carrying a token are secrets: never log them.

func verificationEmail(baseURL *url.URL, to, rawToken string, ttl time.Duration) email.Message {
	const subject = "Verify your VocaTogether email address"
	link := tokenLink(baseURL, verifyEmailPath, rawToken)
	return email.Message{
		To:      to,
		Subject: subject,
		Text: fmt.Sprintf(`Welcome to VocaTogether!

Confirm your email address by opening this link:

%s

The link expires in %s and can be used only once.

If you didn't create a VocaTogether account, you can ignore this email.
`, link, humanDuration(ttl)),
		HTML: actionEmail{
			Subject: subject,
			Heading: "Confirm your email address",
			Intro:   []string{"Confirm your email address to finish creating your account."},
			Button:  "Verify my email",
			Link:    link,
			Expiry:  fmt.Sprintf("The link expires in %s and can be used only once.", humanDuration(ttl)),
			Outro:   []string{"If you didn't create a VocaTogether account, you can ignore this email."},
		}.html(),
	}
}

func passwordResetEmail(baseURL *url.URL, to, rawToken string, ttl time.Duration) email.Message {
	const subject = "Reset your VocaTogether password"
	link := tokenLink(baseURL, resetPasswordPath, rawToken)
	return email.Message{
		To:      to,
		Subject: subject,
		Text: fmt.Sprintf(`We received a request to reset the password of your VocaTogether account.

Choose a new password by opening this link:

%s

The link expires in %s and can be used only once. After the reset you will be
signed out on all devices.

If you didn't ask to reset your password, you can ignore this email. Your
password will not change.
`, link, humanDuration(ttl)),
		HTML: actionEmail{
			Subject: subject,
			Heading: "Reset your password",
			Intro: []string{
				"We received a request to reset the password of your VocaTogether account.",
				"Choose a new password with the button below.",
			},
			Button: "Change password",
			Link:   link,
			Expiry: fmt.Sprintf("The link expires in %s and can be used only once. "+
				"After the reset you will be signed out on all devices.", humanDuration(ttl)),
			Outro: []string{"If you didn't ask to reset your password, you can ignore this email. " +
				"Your password will not change."},
		}.html(),
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

If it was you, open the app and log in: with your password, or with
"Continue with Google" if you signed up with Google. If you don't remember
your password, use "Forgot password" on the login screen.

If it wasn't you, you can ignore this email. Your account has not changed.
`,
	}
}

// passwordlessAccountEmail answers a password reset request for an account
// created with Google, which has no password and can't be recovered through
// its mailbox (decision 020). It carries no link: the account is entered
// only with its Google identity.
func passwordlessAccountEmail(to string) email.Message {
	return email.Message{
		To:      to,
		Subject: "Your VocaTogether account signs in with Google",
		Text: `Someone, hopefully you, asked to reset the password of the VocaTogether
account for this email address.

This account has no password: it signs in with Google. Open the app and use
"Continue with Google" with the Google account you signed up with.

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
