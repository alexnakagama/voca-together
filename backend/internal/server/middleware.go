package server

import (
	"context"
	"net/http"
	"time"
)

// requestTimeout bounds each request's context, below the server's 15 s
// WriteTimeout so the 503 can still be written. The http.Server timeouts
// don't cancel the request context, so without it a request could wait
// indefinitely for a database connection.
//
// The deadline can fire after a transaction has committed but before the
// response is written (decision 018). The client then gets a 503 for work
// that was done, so every operation must be safe to retry: register,
// resend, forgot (a retry sends another email, or none for a known account),
// verify-email and reset-password (a retry reports the link as used, and the
// change stands, as for an unknown COMMIT outcome in decision 017), login (a
// retry creates another session; the orphaned one expires), refresh (the
// rotated tokens are lost, and retrying with the old token is reuse: the
// session is revoked and the client logs in again, as after any lost refresh
// response, decision 014), google (a retry, with the same ID token or a new
// one, finds the account and creates another session; the orphaned one
// expires), logout (idempotent), saving a profile (idempotent: the retry
// stores the same text and changes nothing, decision 027), saving languages
// (idempotent in the same way, decision 029), setting or removing a picture
// (idempotent: the same upload again is recognised and changes nothing, and
// removing nothing is not an error, decision 031), and blocking or unblocking
// a member (idempotent: a block that exists is not stored again, and
// removing none is not an error, decision 033).
const requestTimeout = 10 * time.Second

// requestDeadline gives each request's context a deadline of d.
func requestDeadline(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// securityHeaders sets headers every response gets. The API returns JSON
// and, for profile pictures, a JPEG, so its policy allows loading and
// framing nothing; the reset page replaces the CSP with its own. HSTS is
// sent only in production: TLS ends at the proxy, and browsers ignore the
// header over plain HTTP anyway.
func securityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Referrer-Policy", "no-referrer")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}
