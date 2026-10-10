package safety

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Reasons are the reasons a report may give, as the API and the database
// write them (the CHECK of migration 00010 lists the same five).
var Reasons = []string{"harassment", "inappropriate_content", "spam", "impersonation", "other"}

// DetailsMaxLength is the limit of a report's details in characters (code
// points) after normalization. The database enforces the same number with
// char_length (migration 00010).
const DetailsMaxLength = 1000

// ReportContent is what a member says in a report: a reason and, optionally,
// details. Only ParseReport makes one, so a value that reaches Report has
// passed the rules; the zero value is not a report. It prints as a
// placeholder: a report's reason and details are never logged.
type ReportContent struct {
	reason  string
	details string
}

func (ReportContent) String() string { return "[report]" }

// ParseReport returns the content of a report as it will be stored, or a
// *ValidationError naming every invalid field. It makes no query and depends
// on nothing but its arguments, so a caller can validate a report before
// knowing whom it is about.
//
// The reason must be one of Reasons exactly: no other case, no space around
// it. An empty one is "required".
//
// The details are optional. They are stored as NFC, with CRLF and CR as LF,
// tabs as spaces, and no spaces or line breaks around them; what is left may
// be empty, and is otherwise at most DetailsMaxLength characters with no
// control character but the line feed. Parsing is idempotent: the stored
// text parses to itself.
//
// These rules are deliberately fewer than the profile's (decision 033): the
// details are read by the people who run the service and never shown to a
// member, so invisible and bidirectional characters are kept as written.
func ParseReport(reason, details string) (ReportContent, error) {
	var fields []*FieldError
	if err := checkReason(reason); err != nil {
		fields = append(fields, err)
	}
	details, err := normalizeDetails(details)
	if err != nil {
		fields = append(fields, err)
	}
	if len(fields) > 0 {
		return ReportContent{}, &ValidationError{Fields: fields}
	}
	return ReportContent{reason: reason, details: details}, nil
}

func checkReason(reason string) *FieldError {
	switch {
	case reason == "":
		return ErrReasonRequired
	case !slices.Contains(Reasons, reason):
		return ErrReasonInvalid
	}
	return nil
}

// isSpaceOrLineFeed is what is removed around the details: any Unicode space
// separator and the line feed, which by then stands for every line break, as
// the space does for a tab.
func isSpaceOrLineFeed(r rune) bool {
	return r == '\n' || unicode.Is(unicode.Zs, r)
}

func normalizeDetails(s string) (string, *FieldError) {
	if !utf8.ValidString(s) {
		return "", ErrDetailsInvalid
	}
	s = norm.NFC.String(s)
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", " ").Replace(s)
	s = strings.TrimFunc(s, isSpaceOrLineFeed)
	if utf8.RuneCountInString(s) > DetailsMaxLength {
		return "", ErrDetailsTooLong
	}
	for _, r := range s {
		// U+FFFD is what a JSON decoder leaves in place of bytes that are not
		// valid UTF-8, so it is refused like them.
		if r == unicode.ReplacementChar || (unicode.IsControl(r) && r != '\n') {
			return "", ErrDetailsInvalid
		}
	}
	return s, nil
}

// Report stores the report of the user reportedID by the user reporterID,
// with the content c. Reporting oneself, or a c that ParseReport did not
// make, returns a *ValidationError and stores nothing; a reporter that no
// longer exists returns ErrUserGone.
//
// There is one report per pair, and a save is a full replace: a different
// reason or different details take the place of the earlier ones. It is
// idempotent: the content already stored writes nothing and logs nothing,
// which makes any retry safe. A reported user that no longer exists is not an
// error either: there is nobody to report, and nothing is stored.
//
// Nothing here returns a report, and the log line of one holds the reporter
// and nothing else: never the reason, the details or the reported user.
func (s *Service) Report(ctx context.Context, reporterID, reportedID string, c ReportContent) error {
	if reporterID == reportedID {
		return &ValidationError{Fields: []*FieldError{ErrMemberSelf}}
	}
	if c.reason == "" {
		return &ValidationError{Fields: []*FieldError{ErrReasonRequired}}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("safety: report: %w", err)
	}
	saved, err := upsertReport(ctx, s.pool, reporterID, reportedID, c)
	if err != nil {
		return fmt.Errorf("safety: report: %w", err)
	}
	if saved {
		// The member who reported and never whom, why or what they wrote.
		s.logger.InfoContext(ctx, "report: saved", "user_id", reporterID)
	}
	return nil
}
