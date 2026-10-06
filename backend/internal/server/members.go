package server

import (
	"log/slog"
	"net/http"

	"vocatogether/backend/internal/language"
	"vocatogether/backend/internal/profile"
)

// memberProfileResponse is everything one signed-in member may read about
// another (decision 031): the definition of "public". It is a type of its
// own, not the owner's profileResponse with fields left out, so that nothing
// private can reach it by being added elsewhere: no account id, no email, no
// timestamp, nothing about how the member signs in.
type memberProfileResponse struct {
	ID          string            `json:"id"` // the profile's public id
	DisplayName string            `json:"display_name"`
	Bio         string            `json:"bio"`
	HasAvatar   bool              `json:"has_avatar"`
	Languages   languagesResponse `json:"languages"`
}

// handleGetMemberProfile answers 200 with the public profile the path's id
// names, or 404 profile_not_found. An id no profile has, one that is not
// well formed and a member who has saved no profile are the same 404: the
// handler passes the path value as it came and profile.Public alone decides
// what it names, without a query for a malformed one.
//
// It must run behind requireAccessToken, which also sets no-store, and the
// member-read limit. The reader's identity only authorizes the read; whose
// profile it is comes from the path. The owner's internal user id selects
// their languages and never leaves this function. The body is never read,
// and nothing is logged: not the id, not what the profile holds.
func handleGetMemberProfile(logger *slog.Logger, profiles *profile.Service, languages *language.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := identityFrom(r.Context()); !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		p, err := profiles.Public(r.Context(), r.PathValue("id"))
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		// A separate read, not one snapshot with the profile: a save in
		// between shows on the next load (decision 031).
		s, err := languages.Get(r.Context(), p.UserID)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, memberProfileResponse{
			ID:          p.PublicID,
			DisplayName: p.DisplayName,
			Bio:         p.Bio,
			HasAvatar:   false, // no member has a picture yet
			Languages: languagesResponse{
				Spoken:   responseEntries(s.Spoken),
				Learning: responseEntries(s.Learning),
			},
		})
	}
}
