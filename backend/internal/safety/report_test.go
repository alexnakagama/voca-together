package safety

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseReportReason(t *testing.T) {
	for _, reason := range []string{"harassment", "inappropriate_content", "spam", "impersonation", "other"} {
		t.Run(reason, func(t *testing.T) {
			got, err := ParseReport(reason, "")
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.reason != reason || got.details != "" {
				t.Errorf("parsed = %q, %q; want %q and no details", got.reason, got.details, reason)
			}
		})
	}
	if len(Reasons) != 5 {
		t.Errorf("Reasons = %v, want the five above", Reasons)
	}

	refused := []struct {
		name, in string
		want     *FieldError
	}{
		{"empty", "", ErrReasonRequired},
		{"upper case first", "Spam", ErrReasonInvalid},
		{"upper case", "SPAM", ErrReasonInvalid},
		{"space before", " spam", ErrReasonInvalid},
		{"space after", "spam ", ErrReasonInvalid},
		{"only a space", " ", ErrReasonInvalid},
		{"line break after", "spam\n", ErrReasonInvalid},
		{"unknown word", "rude", ErrReasonInvalid},
		{"two reasons", "spam,other", ErrReasonInvalid},
		{"a hyphen for the underscore", "inappropriate-content", ErrReasonInvalid},
		{"a NUL after", "spam\x00", ErrReasonInvalid},
		{"invalid UTF-8", "spam\xff", ErrReasonInvalid},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseReport(tt.in, "")
			if fields := fieldsOf(t, err); !slices.Equal(fields, []string{tt.want.Error()}) {
				t.Errorf("fields = %v, want %v", fields, tt.want)
			}
			if got != (ReportContent{}) {
				t.Error("a refused report returned content")
			}
		})
	}
}

func TestParseReportDetails(t *testing.T) {
	tests := []struct {
		name, in, want string
		wantErr        *FieldError
	}{
		{name: "absent or empty", in: "", want: ""},
		{name: "only spaces", in: "   ", want: ""},
		{name: "only spaces, tabs and line breaks", in: " \t\r\n 　\n", want: ""},
		{name: "plain", in: "They keep writing to me.", want: "They keep writing to me."},
		{name: "trimmed", in: " \n\t text \t\r\n", want: "text"},
		{name: "spaces inside kept", in: "a  b", want: "a  b"},
		{name: "blank lines inside kept", in: "a\n\n\nb", want: "a\n\n\nb"},
		{name: "CRLF", in: "one\r\ntwo\r\nthree", want: "one\ntwo\nthree"},
		{name: "CR", in: "one\rtwo", want: "one\ntwo"},
		{name: "mixed line breaks", in: "one\r\n\rtwo\n\rthree", want: "one\n\ntwo\n\nthree"},
		{name: "tab", in: "one\ttwo", want: "one two"},
		{name: "CRLF with a tab", in: "one\r\n\ttwo\r\nthree", want: "one\n two\nthree"},
		{name: "composed to NFC", in: "José", want: "José"},
		{name: "other scripts", in: "嫌がらせ: مرحبا 🙂", want: "嫌がらせ: مرحبا 🙂"},
		// Kept as written: nobody is shown this text in the app.
		{name: "bidirectional and zero-width characters kept", in: "a‮b​c", want: "a‮b​c"},
		{name: "1000 characters", in: strings.Repeat("a", 1000), want: strings.Repeat("a", 1000)},
		{name: "1000 characters of three bytes", in: strings.Repeat("語", 1000), want: strings.Repeat("語", 1000)},
		{name: "1000 astral characters", in: strings.Repeat("𠀀", 1000), want: strings.Repeat("𠀀", 1000)},
		{name: "1000 after normalization", in: strings.Repeat("é", 1000), want: strings.Repeat("é", 1000)},
		{name: "1000 after trimming", in: " " + strings.Repeat("a", 1000) + "\r\n", want: strings.Repeat("a", 1000)},
		{name: "1000 with CRLF counted as one", in: strings.Repeat("a\r\n", 499) + "ab", want: strings.Repeat("a\n", 499) + "ab"},

		{name: "1001 characters", in: strings.Repeat("a", 1001), wantErr: ErrDetailsTooLong},
		{name: "1001 characters of three bytes", in: strings.Repeat("語", 1001), wantErr: ErrDetailsTooLong},
		{name: "several thousand characters", in: strings.Repeat("a", 5000), wantErr: ErrDetailsTooLong},
		{name: "NUL", in: "a\x00b", wantErr: ErrDetailsInvalid},
		{name: "escape", in: "a\x1bb", wantErr: ErrDetailsInvalid},
		{name: "vertical tab", in: "a\vb", wantErr: ErrDetailsInvalid},
		{name: "form feed at the end is not trimmed", in: "a\f", wantErr: ErrDetailsInvalid},
		{name: "delete", in: "a\x7fb", wantErr: ErrDetailsInvalid},
		{name: "a C1 control (NEL)", in: "a\u0085b", wantErr: ErrDetailsInvalid},
		{name: "only a control character", in: "\x00", wantErr: ErrDetailsInvalid},
		{name: "invalid UTF-8", in: "a\xffb", wantErr: ErrDetailsInvalid},
		{name: "a truncated sequence", in: "a\xe8\xaa", wantErr: ErrDetailsInvalid},
		{name: "what a JSON decoder leaves of invalid UTF-8", in: "a�b", wantErr: ErrDetailsInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseReport("spam", tt.in)
			if tt.wantErr != nil {
				if fields := fieldsOf(t, err); !slices.Equal(fields, []string{tt.wantErr.Error()}) {
					t.Fatalf("fields = %v, want %v", fields, tt.wantErr)
				}
				if got != (ReportContent{}) {
					t.Error("a refused report returned content")
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.details != tt.want {
				t.Fatalf("details = %q, want %q", got.details, tt.want)
			}
			if n := utf8.RuneCountInString(got.details); n > DetailsMaxLength {
				t.Errorf("stored details have %d characters", n)
			}

			// What is stored parses to itself.
			again, err := ParseReport(got.reason, got.details)
			if err != nil {
				t.Fatalf("parsing its own output: %v", err)
			}
			if again != got {
				t.Errorf("parsing its own output = %q, want %q", again.details, got.details)
			}
		})
	}
}

// Every failing field is named, the reason first.
func TestParseReportReportsBothFields(t *testing.T) {
	tests := []struct {
		name, reason, details string
		want                  []string
	}{
		{"unknown and too long", "rude", strings.Repeat("a", 1001), []string{"reason: invalid", "details: too_long"}},
		{"missing and invalid", "", "a\x00b", []string{"reason: required", "details: invalid"}},
		{"missing and too long", "", strings.Repeat("a", 1001), []string{"reason: required", "details: too_long"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseReport(tt.reason, tt.details)
			if fields := fieldsOf(t, err); !slices.Equal(fields, tt.want) {
				t.Errorf("fields = %v, want %v", fields, tt.want)
			}
		})
	}
}

// An error says which field and which rule, and never what was sent: it may
// be logged.
func TestParseReportErrorsHoldNoneOfTheInput(t *testing.T) {
	const reasonMark, detailsMark = "REASONMARK", "DETAILSMARK"
	for _, tt := range []struct{ reason, details string }{
		{reasonMark, detailsMark + strings.Repeat("a", 1001)},
		{reasonMark, detailsMark + "\x00"},
		{reasonMark, detailsMark + "\xff"},
		{reasonMark, detailsMark},
		{"spam", detailsMark + "\x00"},
	} {
		_, err := ParseReport(tt.reason, tt.details)
		if err == nil {
			t.Fatalf("ParseReport(%q, …) succeeded", tt.reason)
		}
		printed := fmt.Sprintf("%v %+v %s %q", err, err, err.Error(), err)
		if strings.Contains(printed, reasonMark) || strings.Contains(printed, detailsMark) {
			t.Errorf("the error holds the input: %s", printed)
		}
	}
}

// A parsed report prints as a placeholder, whatever prints it.
func TestReportContentIsNeverPrinted(t *testing.T) {
	const detailsMark = "DETAILSMARK"
	c, err := ParseReport("impersonation", detailsMark)
	if err != nil {
		t.Fatal(err)
	}
	var logs strings.Builder
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("x", "report", c)
	slog.New(slog.NewTextHandler(&logs, nil)).Info("x", "report", c)
	printed := fmt.Sprintf("%v %+v %s", c, c, c) + logs.String()
	if strings.Contains(printed, detailsMark) || strings.Contains(printed, "impersonation") {
		t.Errorf("a report was printed: %s", printed)
	}
}
