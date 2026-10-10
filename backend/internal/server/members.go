package server

import (
	"context"
	"log/slog"
	"net/http"

	"vocatogether/backend/internal/avatar"
	"vocatogether/backend/internal/language"
	"vocatogether/backend/internal/profile"
	"vocatogether/backend/internal/safety"
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

// memberFor returns the public profile publicID names, for the reader
// readerID, or profile.ErrNotFound. It is the one place that decides whether
// a member may read another (decisions 031 and 033), and both member routes
// go through it.
//
// A block between the two, made by either, is profile.ErrNotFound: the very
// error of an id that names nobody, so the caller's answer cannot differ in
// status, body or headers, and a member cannot tell being blocked from a
// profile that does not exist. The question is asked after the id resolved,
// so a miss costs no second query, and never of a member reading their own
// profile.
func memberFor(ctx context.Context, profiles *profile.Service, safetySvc *safety.Service,
	readerID, publicID string) (profile.PublicProfile, error) {
	p, err := profiles.Public(ctx, publicID)
	if err != nil {
		return profile.PublicProfile{}, err
	}
	if p.UserID == readerID {
		return p, nil
	}
	blocked, err := safetySvc.Blocked(ctx, readerID, p.UserID)
	if err != nil {
		return profile.PublicProfile{}, err
	}
	if blocked {
		return profile.PublicProfile{}, profile.ErrNotFound
	}
	return p, nil
}

// handleGetMemberProfile answers 200 with the public profile the path's id
// names, or 404 profile_not_found. An id no profile has, one that is not
// well formed, a member who has saved no profile and a member with a block
// between them and the reader are the same 404: the handler passes the path
// value as it came and memberFor alone decides what the reader may read,
// without a query for a malformed id.
//
// It must run behind requireAccessToken, which also sets no-store, and the
// member-read limit. Whose profile it is comes from the path; the reader's
// identity authorizes the read and is what a block is checked against. The
// owner's internal user id selects their languages and their picture and
// never leaves this function. The body is never read, and nothing is
// logged: not the id, not what the profile holds, not a block.
func handleGetMemberProfile(logger *slog.Logger, profiles *profile.Service, languages *language.Service,
	avatars *avatar.Service, safetySvc *safety.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reader, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		p, err := memberFor(r.Context(), profiles, safetySvc, reader.UserID, r.PathValue("id"))
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		// Separate reads, not one snapshot with the profile: a save in
		// between shows on the next load (decision 031).
		s, err := languages.Get(r.Context(), p.UserID)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		hasAvatar, err := avatars.Exists(r.Context(), p.UserID)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, memberProfileResponse{
			ID:          p.PublicID,
			DisplayName: p.DisplayName,
			Bio:         p.Bio,
			HasAvatar:   hasAvatar,
			Languages: languagesResponse{
				Spoken:   responseEntries(s.Spoken),
				Learning: responseEntries(s.Learning),
			},
		})
	}
}

// handleGetMemberAvatar answers 200 with the picture of the member the
// path's id names, as image/jpeg; 404 avatar_not_found if that member has a
// profile and no picture; and 404 profile_not_found for everything that is
// not the id of a saved profile, exactly as handleGetMemberProfile does.
//
// The id is resolved through the profile first, so the picture of a member
// who has saved no profile is unreachable: no profile, no public picture
// (decision 031). A block between the reader and the member is answered
// profile_not_found too, whether or not there is a picture: avatar_not_found
// would say that the profile exists (decision 033). It must run behind
// requireAccessToken and the member-read limit; the owner's internal user id
// never leaves this function, the body is never read and nothing is logged.
func handleGetMemberAvatar(logger *slog.Logger, profiles *profile.Service, avatars *avatar.Service,
	safetySvc *safety.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reader, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		p, err := memberFor(r.Context(), profiles, safetySvc, reader.UserID, r.PathValue("id"))
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		image, err := avatars.Get(r.Context(), p.UserID)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeImage(w, image)
	}
}
