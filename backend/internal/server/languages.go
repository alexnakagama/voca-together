package server

import (
	"errors"
	"log/slog"
	"net/http"

	"vocatogether/backend/internal/language"
)

// maxLanguagesBodyBytes is far above the longest valid selection (ten
// entries, under 2 KiB even with every character \u-escaped). Unlike a
// profile's text, nothing a person types can be long here: codes and levels
// are identifiers the app picks from fixed lists, so a body near the limit
// is never an honest one. It bounds what one request can make the server
// read and parse; writes are also limited per user.
const maxLanguagesBodyBytes = 8 << 10

// errIncompleteLanguages is a body that is valid JSON but doesn't say what
// replaces each list: it is null, or one of its lists is left out or null.
// It must not pass for an empty selection, which would remove a member's
// languages because a client forgot a key (decision 029).
var errIncompleteLanguages = errors.New("server: languages body does not hold both lists")

// languageEntry is one language of a member, in requests and responses.
type languageEntry struct {
	Language string `json:"language"`
	Level    string `json:"level"`
}

// languagesRequest lists everything a save may set. Anything else in the
// body or in an entry, an id, a kind or a position included, is an unknown
// field and the request is refused (decodeJSON), so nothing becomes writable
// by accident.
//
// Both lists are required: a save replaces the whole selection, so each list
// must be an array, and only an empty array clears one. The decoder leaves a
// list nil when its key is absent or null and non-nil for any array, [] too,
// which is how complete tells them apart.
type languagesRequest struct {
	Spoken   []languageEntry `json:"spoken"`
	Learning []languageEntry `json:"learning"`
}

// complete reports whether the body gave both lists as arrays.
func (r *languagesRequest) complete() bool {
	return r != nil && r.Spoken != nil && r.Learning != nil
}

// languagesResponse is the caller's own languages. It lists its fields
// explicitly: no user id, no timestamps, nothing about the account. Both
// lists are always arrays, never null.
type languagesResponse struct {
	Spoken   []languageEntry `json:"spoken"`
	Learning []languageEntry `json:"learning"`
}

// catalogResponse is every language a member can choose.
type catalogResponse struct {
	Languages []catalogLanguage `json:"languages"`
}

type catalogLanguage struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Endonym string `json:"endonym"`
}

func inputEntries(in []languageEntry) []language.InputEntry {
	out := make([]language.InputEntry, len(in))
	for i, e := range in {
		out[i] = language.InputEntry{Language: e.Language, Level: e.Level}
	}
	return out
}

func responseEntries(in []language.Entry) []languageEntry {
	out := make([]languageEntry, len(in))
	for i, e := range in {
		out[i] = languageEntry{Language: string(e.Code), Level: e.Level.String()}
	}
	return out
}

func writeLanguages(w http.ResponseWriter, s language.Selection) {
	writeJSON(w, http.StatusOK, languagesResponse{
		Spoken:   responseEntries(s.Spoken),
		Learning: responseEntries(s.Learning),
	})
}

// handleLanguageCatalog answers 200 with the catalog, ordered by name: the
// same for every member. It must run behind requireAccessToken, which also
// sets no-store. The body is never read.
func handleLanguageCatalog(logger *slog.Logger, svc *language.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := identityFrom(r.Context()); !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		catalog, err := svc.Catalog(r.Context())
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		out := make([]catalogLanguage, len(catalog))
		for i, l := range catalog {
			out[i] = catalogLanguage{Code: string(l.Code), Name: l.Name, Endonym: l.Endonym}
		}
		writeJSON(w, http.StatusOK, catalogResponse{Languages: out})
	}
}

// handleGetLanguages answers 200 with the authenticated user's languages;
// both lists are empty if they have chosen none (an empty collection is a
// value, so there is no 404 here). It must run behind requireAccessToken.
// The body is never read.
func handleGetLanguages(logger *slog.Logger, svc *language.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		s, err := svc.Get(r.Context(), id.UserID)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeLanguages(w, s)
	}
}

// handlePutLanguages replaces all the authenticated user's languages with
// the body, in the order given, and answers 200 with them as stored, whether
// they changed or were already the same. The body must hold both lists
// (languagesRequest); one that doesn't is 400 invalid_request and changes
// nothing. It must run behind requireAccessToken. Whose languages they are
// comes only from the session; the request can't name another user.
func handlePutLanguages(logger *slog.Logger, svc *language.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		// A pointer, so that a null body is told apart from an object.
		var req *languagesRequest
		err := decodeJSON(w, r, &req, maxLanguagesBodyBytes)
		if err == nil && !req.complete() {
			err = errIncompleteLanguages
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		s, err := svc.Save(r.Context(), id.UserID, language.Input{
			Spoken:   inputEntries(req.Spoken),
			Learning: inputEntries(req.Learning),
		})
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeLanguages(w, s)
	}
}
