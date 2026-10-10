package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"vocatogether/backend/internal/profile"
	"vocatogether/backend/internal/safety"
)

// blockedMemberResponse is one member the caller has blocked, in the list of
// GET /v1/me/blocks (decision 033). It is a type of its own, like
// memberProfileResponse, and holds only what names the member: no account
// id, no email, no time of the block and no picture.
type blockedMemberResponse struct {
	ID          string `json:"id"` // the profile's public id
	DisplayName string `json:"display_name"`
}

// blocksResponse is the caller's list of blocked members. Blocks is always an
// array, never null.
type blocksResponse struct {
	Blocks []blockedMemberResponse `json:"blocks"`
}

// writeBlocks answers 200 with the list. Unlike writeJSON it does not write
// &, < and > as six-byte \u escapes, which protect JSON placed inside HTML:
// this answer is application/json with nosniff, and a list of the most
// members with the longest names must stay under the 64 KiB a client reads
// (decision 033). Without the escapes no character of a name takes more than
// four bytes.
func writeBlocks(w http.ResponseWriter, blocks []blockedMemberResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(blocksResponse{Blocks: blocks})
}

// targetUserID resolves the public id a block or report route carries to the
// internal id of the user it names. found is false for everything that is not
// the id of a saved profile: unknown, malformed (answered by profile.Public
// without a query), an account id, a member with no profile.
//
// The caller answers found=false exactly as it answers success, so these
// routes never say what an id names (decision 033).
func targetUserID(ctx context.Context, profiles *profile.Service, publicID string) (userID string, found bool, err error) {
	p, err := profiles.Public(ctx, publicID)
	switch {
	case errors.Is(err, profile.ErrNotFound):
		return "", false, nil
	case err != nil:
		return "", false, err
	}
	return p.UserID, true, nil
}

// handlePutBlock makes the authenticated user block the member the path's id
// names, and answers 204 with no body: when the block was stored, when it
// already existed, and when the id names no profile, in which case nothing
// is stored. The three are one answer on purpose: a member whose read of a
// profile says 404 must not be able to learn from a write whether that
// profile exists (decision 033). Blocking oneself is 422 member: self, and
// one member more than safety.MaxBlocks is 422 blocks: too_many.
//
// It must run behind requireAccessToken, which also sets no-store, and the
// block-write limit. Who blocks comes only from the session; the path names
// whom, and the target's internal user id never leaves the handler. The body
// is never read, and the handler logs nothing: not the id, not whom it names.
func handlePutBlock(logger *slog.Logger, profiles *profile.Service, safetySvc *safety.Service) http.HandlerFunc {
	return handleBlockWrite(logger, profiles, safetySvc.Block)
}

// handleDeleteBlock removes the authenticated user's block of the member the
// path's id names, and answers 204 with no body: also when there was no
// block, and when the id names no profile. It never removes a block the
// other member made. Naming oneself is 422 member: self.
//
// It must run behind requireAccessToken and the block-write limit, which it
// shares with handlePutBlock. The body is never read and the handler logs
// nothing.
func handleDeleteBlock(logger *slog.Logger, profiles *profile.Service, safetySvc *safety.Service) http.HandlerFunc {
	return handleBlockWrite(logger, profiles, safetySvc.Unblock)
}

// handleBlockWrite is the handler of both block routes; write is
// safety.Service.Block or Unblock. The two differ in nothing else, so that
// their answers for an id that names nobody cannot drift apart.
func handleBlockWrite(logger *slog.Logger, profiles *profile.Service,
	write func(ctx context.Context, blockerID, blockedID string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		target, found, err := targetUserID(r.Context(), profiles, r.PathValue("id"))
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		if found {
			if err := write(r.Context(), id.UserID, target); err != nil {
				writeServiceError(w, r, logger, err)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleGetBlocks answers 200 with the members the authenticated user has
// blocked, most recently blocked first, each by their profile's public id
// and current name. It never holds who has blocked the caller, a member the
// caller has blocked too included (safety.Service.ListBlocked).
//
// safety knows the blocked users and profile what names them, and neither
// imports the other: this handler composes the two. A blocked member who has
// no profile (they deleted it, or it was taken down) has nothing to be named
// by and is left out; the block still holds. The two reads are not one
// snapshot: a name saved in between is a name a moment newer.
//
// It must run behind requireAccessToken. It is the caller's own data, so it
// is not limited. The body is never read and nothing is logged.
func handleGetBlocks(logger *slog.Logger, profiles *profile.Service, safetySvc *safety.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		blocked, err := safetySvc.ListBlocked(r.Context(), id.UserID)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		found, err := profiles.PublicByUsers(r.Context(), blocked)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		byUser := make(map[string]profile.PublicProfile, len(found))
		for _, p := range found {
			byUser[p.UserID] = p
		}
		// The order is the list's, newest block first.
		blocks := make([]blockedMemberResponse, 0, len(found))
		for _, userID := range blocked {
			if p, ok := byUser[userID]; ok {
				blocks = append(blocks, blockedMemberResponse{ID: p.PublicID, DisplayName: p.DisplayName})
			}
		}
		writeBlocks(w, blocks)
	}
}

// maxReportBodyBytes is deliberately far above the longest valid report
// (under 13 KiB even with every character of the details \u-escaped as a
// surrogate pair), for the reason maxProfileBodyBytes is: details that are
// too long must be answered by the field's own rule, 422 too_long, and a
// limit close to it would refuse a long paste as a malformed request instead.
const maxReportBodyBytes = 64 << 10

// reportRequest lists everything a report may say. Anything else in the
// body, a reporter or a reported member included, is an unknown field and
// the request is refused (decodeJSON): who reports is the session and whom
// is the path. An absent or null field is the empty string.
type reportRequest struct {
	Reason  string `json:"reason"`
	Details string `json:"details"`
}

// handlePutReport stores the authenticated user's report of the member the
// path's id names, replacing the one they made of that member before, and
// answers 204 with no body: when the report was stored, when the same one
// was stored already, and when the id names no profile, in which case
// nothing is stored. As for a block, the three are one answer on purpose
// (decision 033). There is no body because there is nothing to give back:
// no route ever returns a report.
//
// The body is validated before the id is resolved, so what is wrong with a
// report (400 for its shape, 422 for its reason and details) is answered the
// same whatever the id names. Reporting oneself is 422 member: self. A block
// between the two members, in either direction, changes nothing here:
// blocking first is no way to escape a report.
//
// It must run behind requireAccessToken, which also sets no-store, and the
// report-write limit. Who reports comes only from the session, and the
// target's internal user id never leaves the handler. The handler logs
// nothing: not the id, not whom it names, not the reason and not the details.
func handlePutReport(logger *slog.Logger, profiles *profile.Service, safetySvc *safety.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		var req reportRequest
		if err := decodeJSON(w, r, &req, maxReportBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		content, err := safety.ParseReport(req.Reason, req.Details)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		target, found, err := targetUserID(r.Context(), profiles, r.PathValue("id"))
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		if found {
			if err := safetySvc.Report(r.Context(), id.UserID, target, content); err != nil {
				writeServiceError(w, r, logger, err)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
