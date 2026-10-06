package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/avatar"
	"vocatogether/backend/internal/profile"
)

const (
	avatarPath     = "/v1/me/avatar"
	avatarNotFound = `{"error":{"code":"avatar_not_found"}}`
)

var (
	avatarRed   = color.NRGBA{R: 230, G: 20, B: 20, A: 255}
	avatarGreen = color.NRGBA{R: 20, G: 180, B: 20, A: 255}
	avatarBlue  = color.NRGBA{R: 20, G: 20, B: 230, A: 255}
)

// The uploads are generated: no image file is checked in.

func plainImage(w, h int, c color.Color) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

func pngUpload(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, plainImage(w, h, c)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegUpload(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, plainImage(w, h, c), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pngChunkOf is one PNG chunk: its length, type, data and checksum.
func pngChunkOf(typ string, data []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	chunk = append(chunk, typ...)
	chunk = append(chunk, data...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}

// pngDeclaring is a 33-byte PNG that declares a w by h image and holds no
// pixels: a small file with any dimensions.
func pngDeclaring(w, h int) []byte {
	ihdr := binary.BigEndian.AppendUint32(nil, uint32(w))
	ihdr = binary.BigEndian.AppendUint32(ihdr, uint32(h))
	ihdr = append(ihdr, 8, 2, 0, 0, 0)
	return append([]byte("\x89PNG\r\n\x1a\n"), pngChunkOf("IHDR", ihdr)...)
}

// avatarWith sends method to target (a path plus any query) with body, the
// declared content type (none if empty) and the given Authorization header
// values (none if empty).
func (a testAPI) avatarWith(method, target string, body []byte, contentType string, authorization ...string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, v := range authorization {
		req.Header.Add("Authorization", v)
	}
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a testAPI) putAvatar(accessToken string, raw []byte) *httptest.ResponseRecorder {
	return a.avatarWith(http.MethodPut, avatarPath, raw, "application/octet-stream", "Bearer "+accessToken)
}

func (a testAPI) getAvatar(accessToken string) *httptest.ResponseRecorder {
	return a.avatarWith(http.MethodGet, avatarPath, nil, "", "Bearer "+accessToken)
}

func (a testAPI) deleteAvatar(accessToken string) *httptest.ResponseRecorder {
	return a.avatarWith(http.MethodDelete, avatarPath, nil, "", "Bearer "+accessToken)
}

func (a testAPI) getMemberAvatar(accessToken, id string) *httptest.ResponseRecorder {
	return a.avatarWith(http.MethodGet, membersPath+id+"/avatar", nil, "", "Bearer "+accessToken)
}

func (a testAPI) avatarService() *avatar.Service {
	return avatar.NewService(a.pool, slog.New(slog.DiscardHandler))
}

func (a testAPI) avatarCount(t *testing.T) int {
	t.Helper()
	return a.count(t, "avatars")
}

// avatarRowVersion identifies the stored row version of addr's picture: it
// changes whenever the row is rewritten, even with the same values.
func (a testAPI) avatarRowVersion(t *testing.T, addr string) string {
	t.Helper()
	var v string
	err := a.pool.QueryRow(context.Background(),
		`SELECT a.xmin::text || '/' || a.ctid::text FROM avatars a JOIN users u ON u.id = a.user_id WHERE u.email = $1`,
		addr).Scan(&v)
	if err != nil {
		t.Fatalf("avatar of %s: %v", addr, err)
	}
	return v
}

// requireImage checks an image response and returns the picture: 200, a JPEG
// declared as one, never cached, never sniffed, and a stored picture's size.
func requireImage(t *testing.T, rec *httptest.ResponseRecorder) []byte {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %.200s", rec.Code, rec.Body)
	}
	body := rec.Body.Bytes()
	for name, want := range map[string]string{
		"Content-Type":           "image/jpeg",
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"Content-Length":         fmt.Sprint(len(body)),
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if !bytes.HasPrefix(body, []byte{0xFF, 0xD8, 0xFF}) {
		t.Fatalf("the body is not a JPEG: % x", body[:min(len(body), 8)])
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("the body does not decode: %v", err)
	}
	if cfg.Width != avatar.Size || cfg.Height != avatar.Size {
		t.Errorf("the picture is %d by %d, want %d by %d", cfg.Width, cfg.Height, avatar.Size, avatar.Size)
	}
	return body
}

// requireShows checks that picture's centre is about the colour want.
func requireShows(t *testing.T, picture []byte, want color.NRGBA) {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(picture))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(avatar.Size/2, avatar.Size/2).RGBA()
	got := [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
	for i, w := range [3]int{int(want.R), int(want.G), int(want.B)} {
		if d := got[i] - w; d < -40 || d > 40 {
			t.Errorf("the picture's centre is %v, want about %v", got, want)
			return
		}
	}
}

// requireAvatarRemoved checks the answer to a removal: 204, no body, and no
// header beyond the ones every protected response has.
func requireAvatarRemoved(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	requireLoggedOut(t, rec)
}

func requireAvatarRefused(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	requireLoginResponse(t, rec, http.StatusUnprocessableEntity, invalidField("avatar", code))
}

// hasAvatar reads has_avatar from a member profile response.
func hasAvatar(t *testing.T, rec *httptest.ResponseRecorder) bool {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var body struct {
		HasAvatar *bool `json:"has_avatar"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.HasAvatar == nil {
		t.Fatalf("no has_avatar in %s (%v)", rec.Body, err)
	}
	return *body.HasAvatar
}

// ---- Setting and replacing one's own picture ----

func TestGetAvatarBeforeSettingOne(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLoginResponse(t, api.getAvatar(tokens.AccessToken), http.StatusNotFound, avatarNotFound)
}

// The first picture, by a member who has saved no profile: the answer is the
// picture as stored, and reading it returns the same bytes.
func TestPutAvatarSetsItAndGetReturnsIt(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	raw := jpegUpload(t, 300, 200, avatarRed)

	stored := requireImage(t, api.putAvatar(tokens.AccessToken, raw))
	requireShows(t, stored, avatarRed)
	if bytes.Equal(stored, raw) {
		t.Error("the upload itself was returned")
	}
	if got := requireImage(t, api.getAvatar(tokens.AccessToken)); !bytes.Equal(got, stored) {
		t.Error("GET returned other bytes than the PUT")
	}
	if n := api.profileCount(t); n != 0 {
		t.Errorf("profiles = %d: setting a picture needs none and creates none", n)
	}
	if n := api.avatarCount(t); n != 1 {
		t.Errorf("avatars = %d, want 1", n)
	}
}

// A different upload replaces the picture, and the earlier one is returned
// by no request any more.
func TestPutAvatarReplacesThePicture(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	first := requireImage(t, api.putAvatar(ana.AccessToken, jpegUpload(t, 64, 64, avatarRed)))

	second := requireImage(t, api.putAvatar(ana.AccessToken, pngUpload(t, 64, 64, avatarBlue)))
	requireShows(t, second, avatarBlue)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"own":    api.getAvatar(ana.AccessToken),
		"member": api.getMemberAvatar(ana.AccessToken, anaID),
	} {
		if got := requireImage(t, rec); !bytes.Equal(got, second) || bytes.Equal(got, first) {
			t.Errorf("%s read: not the new picture", name)
		}
	}
	if n := api.avatarCount(t); n != 1 {
		t.Errorf("avatars = %d, want 1", n)
	}
}

// What is stored is made by the server from the pixels: the centre square at
// the stored size, as a JPEG, with nothing else of the upload in it.
func TestPutAvatarStoresANormalizedPictureWithoutTheUploadsData(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	const marker = "MARKERLOCATION"

	// A landscape PNG: green in the centre square, red in the margins a
	// centre crop drops, a text chunk and bytes after the end of the image.
	img := image.NewNRGBA(image.Rect(0, 0, 3000, 2000))
	draw.Draw(img, img.Bounds(), image.NewUniform(avatarRed), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(500, 0, 2500, 2000), image.NewUniform(avatarGreen), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	file := buf.Bytes()
	const afterHeader = 8 + 25 // the signature and the IHDR chunk
	raw := append([]byte{}, file[:afterHeader]...)
	raw = append(raw, pngChunkOf("tEXt", []byte("Comment\x00"+marker))...)
	raw = append(raw, file[afterHeader:]...)
	raw = append(raw, marker...)

	stored := requireImage(t, api.putAvatar(tokens.AccessToken, raw))
	if bytes.Contains(stored, []byte(marker)) {
		t.Error("the stored picture holds data of the upload")
	}
	picture, err := jpeg.Decode(bytes.NewReader(stored))
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []image.Point{{3, 3}, {avatar.Size - 4, avatar.Size - 4}, {avatar.Size / 2, avatar.Size / 2}} {
		r, g, _, _ := picture.At(at.X, at.Y).RGBA()
		if r>>8 > 80 || g>>8 < 140 {
			t.Errorf("pixel %v is not the centre square's green: r %d, g %d", at, r>>8, g>>8)
		}
	}
	if got := requireImage(t, api.getAvatar(tokens.AccessToken)); !bytes.Equal(got, stored) {
		t.Error("GET returned other bytes than the PUT")
	}
}

// ---- Accepted input ----

func TestPutAvatarRefusesWhatIsNotAnAcceptablePicture(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	current := requireImage(t, api.putAvatar(tokens.AccessToken, jpegUpload(t, 64, 64, avatarRed)))
	version := api.avatarRowVersion(t, "ana@example.com")
	jpegFile, pngFile := jpegUpload(t, 64, 64, avatarBlue), pngUpload(t, 64, 64, avatarBlue)

	for name, tc := range map[string]struct {
		body        []byte
		contentType string
		code        string
	}{
		"no body":                           {nil, "image/jpeg", "required"},
		"one byte over the limit":           {make([]byte, avatar.MaxUploadBytes+1), "image/jpeg", "too_large"},
		"twice the limit":                   {make([]byte, 2*avatar.MaxUploadBytes), "image/png", "too_large"},
		"exactly the limit, not an image":   {make([]byte, avatar.MaxUploadBytes), "image/jpeg", "unsupported_type"},
		"a GIF":                             {[]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"), "image/gif", "unsupported_type"},
		"a GIF declared as a JPEG":          {[]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"), "image/jpeg", "unsupported_type"},
		"a WebP":                            {[]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), "image/webp", "unsupported_type"},
		"a WebP declared as a PNG":          {[]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), "image/png", "unsupported_type"},
		"a PDF":                             {[]byte("%PDF-1.7\n1 0 obj\n"), "application/pdf", "unsupported_type"},
		"text":                              {[]byte("this is not a picture"), "text/plain", "unsupported_type"},
		"text declared as a JPEG":           {[]byte("this is not a picture"), "image/jpeg", "unsupported_type"},
		"JSON":                              {[]byte(`{"avatar":"x"}`), "application/json", "unsupported_type"},
		"a form":                            {[]byte("--b\r\nContent-Disposition: form-data; name=\"avatar\"\r\n\r\nx\r\n--b--"), "multipart/form-data; boundary=b", "unsupported_type"},
		"a truncated JPEG":                  {jpegFile[:len(jpegFile)/2], "image/jpeg", "invalid_image"},
		"a JPEG signature and nothing else": {[]byte{0xFF, 0xD8, 0xFF}, "image/jpeg", "invalid_image"},
		"a truncated PNG":                   {pngFile[:len(pngFile)-30], "image/png", "invalid_image"},
		"too wide":                          {pngDeclaring(avatar.MaxDimension+1, 10), "image/png", "dimensions_too_large"},
		"too tall":                          {pngDeclaring(10, avatar.MaxDimension+1), "image/png", "dimensions_too_large"},
		"a small file declaring a huge one": {pngDeclaring(20000, 20000), "image/png", "dimensions_too_large"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := api.avatarWith(http.MethodPut, avatarPath, tc.body, tc.contentType, "Bearer "+tokens.AccessToken)
			requireAvatarRefused(t, rec, tc.code)
		})
	}

	// Every refusal left the picture the member had.
	if got := requireImage(t, api.getAvatar(tokens.AccessToken)); !bytes.Equal(got, current) {
		t.Error("a refused upload changed the picture")
	}
	if got := api.avatarRowVersion(t, "ana@example.com"); got != version {
		t.Errorf("a refused upload rewrote the row: %s → %s", version, got)
	}
}

// What an upload is is decided by its content: the declared type, right,
// wrong or missing, changes nothing.
func TestPutAvatarIgnoresTheDeclaredType(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	uploads := map[color.NRGBA][]byte{avatarRed: jpegUpload(t, 64, 64, avatarRed), avatarBlue: pngUpload(t, 64, 64, avatarBlue)}
	stored := map[color.NRGBA][]byte{}

	for _, contentType := range []string{
		"application/octet-stream", "image/jpeg", "image/png", "", "text/plain", "application/json",
		"image/gif", "multipart/form-data; boundary=b", "not a media type",
	} {
		for c, raw := range uploads {
			got := requireImage(t, api.avatarWith(http.MethodPut, avatarPath, raw, contentType, "Bearer "+tokens.AccessToken))
			requireShows(t, got, c)
			if stored[c] == nil {
				stored[c] = got
			}
			if !bytes.Equal(got, stored[c]) {
				t.Errorf("declared as %q, the same upload gave another picture", contentType)
			}
		}
	}
}

// counted counts the bytes read from the reader it wraps.
type counted struct {
	r io.Reader
	n int
}

func (c *counted) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// A body over the limit is refused as too large without being read past the
// limit: however long it is, the server takes in no more than the largest
// accepted upload and the little it needs to see there is more.
func TestPutAvatarDoesNotReadPastTheLimit(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	for _, size := range []int{avatar.MaxUploadBytes + 1, avatar.MaxUploadBytes + 4096, 20 * avatar.MaxUploadBytes} {
		// The start of a real PNG, so only its length can refuse it.
		body := &counted{r: io.LimitReader(io.MultiReader(bytes.NewReader(pngDeclaring(64, 64)), zeroes{}), int64(size))}
		req := httptest.NewRequest(http.MethodPut, avatarPath, body)
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		rec := httptest.NewRecorder()
		api.handler.ServeHTTP(rec, req)

		requireAvatarRefused(t, rec, "too_large")
		if body.n > avatar.MaxUploadBytes+2 {
			t.Errorf("a body of %d bytes: %d were read, want no more than the limit of %d and two", size, body.n, avatar.MaxUploadBytes)
		}
	}
	if n := api.avatarCount(t); n != 0 {
		t.Errorf("avatars = %d, want 0", n)
	}
}

// zeroes is an endless reader of zero bytes.
type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// A body that can't be read whole is not an upload to judge.
func TestPutAvatarWithABodyThatFailsToArrive(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	raw := pngUpload(t, 64, 64, avatarRed)

	req := httptest.NewRequest(http.MethodPut, avatarPath, io.MultiReader(bytes.NewReader(raw[:40]), failingReader{}))
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	rec := httptest.NewRecorder()
	api.handler.ServeHTTP(rec, req)

	requireLoginResponse(t, rec, http.StatusBadRequest, invalidRequest)
	if n := api.avatarCount(t); n != 0 {
		t.Errorf("avatars = %d, want 0", n)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// ---- Uploading is idempotent ----

// The same upload twice (a lost answer, the resend after a 401, the retry
// after a 503): identical answers, and nothing rewritten or logged again.
func TestPutAvatarIsIdempotent(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	raw := jpegUpload(t, 120, 80, avatarGreen)

	first := api.putAvatar(tokens.AccessToken, raw)
	stored := requireImage(t, first)
	version := api.avatarRowVersion(t, "ana@example.com")
	for range 3 {
		again := api.putAvatar(tokens.AccessToken, raw)
		if !bytes.Equal(requireImage(t, again), stored) || fmt.Sprint(again.Header()) != fmt.Sprint(first.Header()) {
			t.Errorf("the replay answered differently:\n%v\nvs\n%v", again.Header(), first.Header())
		}
	}
	if got := api.avatarRowVersion(t, "ana@example.com"); got != version {
		t.Errorf("the same upload rewrote the row: %s → %s", version, got)
	}
	api.svc.Wait()
	if n := strings.Count(api.logs.String(), "avatar: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1: the replays changed nothing", n)
	}
}

// ---- Removing one's own picture ----

func TestDeleteAvatar(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben := api.loggedIn(t, "ben@example.com")
	bens := requireImage(t, api.putAvatar(ben.AccessToken, pngUpload(t, 64, 64, avatarBlue)))

	// Removing nothing is 204 too.
	requireAvatarRemoved(t, api.deleteAvatar(ana.AccessToken))

	requireImage(t, api.putAvatar(ana.AccessToken, jpegUpload(t, 64, 64, avatarRed)))
	requireImage(t, api.getMemberAvatar(ben.AccessToken, anaID))
	requireAvatarRemoved(t, api.deleteAvatar(ana.AccessToken))

	// No later request returns it: not the owner's, not another member's.
	requireLoginResponse(t, api.getAvatar(ana.AccessToken), http.StatusNotFound, avatarNotFound)
	requireLoginResponse(t, api.getMemberAvatar(ben.AccessToken, anaID), http.StatusNotFound, avatarNotFound)
	requireLoginResponse(t, api.getMemberAvatar(ana.AccessToken, anaID), http.StatusNotFound, avatarNotFound)
	requireAvatarRemoved(t, api.deleteAvatar(ana.AccessToken))

	// A body and a query change nothing about a removal.
	requireImage(t, api.putAvatar(ana.AccessToken, jpegUpload(t, 64, 64, avatarRed)))
	requireAvatarRemoved(t, api.avatarWith(http.MethodDelete, avatarPath+"?keep=1", []byte(`{"keep":true}`), "application/json", "Bearer "+ana.AccessToken))
	requireLoginResponse(t, api.getAvatar(ana.AccessToken), http.StatusNotFound, avatarNotFound)

	// Only Ana's went, and her profile is untouched.
	if got := requireImage(t, api.getAvatar(ben.AccessToken)); !bytes.Equal(got, bens) {
		t.Error("ben's picture changed")
	}
	if n := api.avatarCount(t); n != 1 {
		t.Errorf("avatars = %d, want 1", n)
	}
	if name, _ := api.storedProfile(t, "ana@example.com"); name != "Ana" {
		t.Errorf("ana's profile name = %q", name)
	}
}

// ---- Reading a member's picture ----

func TestGetMemberAvatar(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	bens := requireImage(t, api.putAvatar(ben.AccessToken, pngUpload(t, 64, 64, avatarBlue)))

	// Another member's picture, one's own by the public id, and a query
	// string that names somebody else: always the stored picture of the
	// member the path names.
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"another member": api.getMemberAvatar(ana.AccessToken, benID),
		"the owner":      api.getMemberAvatar(ben.AccessToken, benID),
		"with a query": api.avatarWith(http.MethodGet, membersPath+benID+"/avatar?id="+anaID+"&user_id="+api.userID(t, "ana@example.com"),
			nil, "", "Bearer "+ana.AccessToken),
	} {
		if got := requireImage(t, rec); !bytes.Equal(got, bens) {
			t.Errorf("%s: not the stored picture", name)
		}
	}

	// A member with a profile and no picture.
	requireLoginResponse(t, api.getMemberAvatar(ben.AccessToken, anaID), http.StatusNotFound, avatarNotFound)
	requireLoginResponse(t, api.getMemberAvatar(ana.AccessToken, anaID), http.StatusNotFound, avatarNotFound)

	// Reading changed nothing.
	if n := api.avatarCount(t); n != 1 {
		t.Errorf("avatars = %d, want 1", n)
	}
}

// Everything that is not the public id of a saved profile is the one 404 of
// the member profile route, whether or not a picture is behind it.
func TestGetMemberAvatarNotFoundIsTheSameForEveryMiss(t *testing.T) {
	api := newTestAPI(t)
	ana, _ := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	requireImage(t, api.putAvatar(ben.AccessToken, pngUpload(t, 64, 64, avatarBlue)))

	unknown := api.getMemberAvatar(ana.AccessToken, unknownMemberID)
	requireMemberNotFound(t, unknown)
	profileMiss := api.getMember(ana.AccessToken, unknownMemberID)

	for name, id := range map[string]string{
		"not an id":               "not-an-id",
		"upper case":              strings.ToUpper(benID),
		"no hyphens":              strings.ReplaceAll(benID, "-", ""),
		"one character short":     benID[:35],
		"one character long":      benID + "0",
		"trailing space":          benID + "%20",
		"the owner's account id":  api.userID(t, "ben@example.com"),
		"the reader's account id": api.userID(t, "ana@example.com"),
		"an email address":        "ben%40example.com",
		"me":                      "me",
	} {
		t.Run(name, func(t *testing.T) {
			rec := api.getMemberAvatar(ana.AccessToken, id)
			requireMemberNotFound(t, rec)
			for route, miss := range map[string]*httptest.ResponseRecorder{"the picture route": unknown, "the profile route": profileMiss} {
				if rec.Body.String() != miss.Body.String() || fmt.Sprint(rec.Header()) != fmt.Sprint(miss.Header()) {
					t.Errorf("response differs from an unknown id's on %s:\n%v %s\nvs\n%v %s",
						route, rec.Header(), rec.Body, miss.Header(), miss.Body)
				}
			}
		})
	}
}

// A picture can be set before a profile is saved, and then nobody else can
// reach it: no profile, no public picture. Saving a profile is what makes it
// public, and deleting the account takes both away.
func TestAPictureWithoutAProfileIsNotPublic(t *testing.T) {
	api := newTestAPI(t)
	ana, _ := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	cho := api.loggedIn(t, "cho@example.com")
	chos := requireImage(t, api.putAvatar(cho.AccessToken, pngUpload(t, 64, 64, avatarGreen)))
	choAccount := api.userID(t, "cho@example.com")

	// Cho has no public id; nothing anyone can send names her picture.
	for _, id := range []string{choAccount, strings.ToUpper(choAccount), "cho%40example.com", unknownMemberID} {
		for _, reader := range []loginTokens{ana, cho} {
			requireMemberNotFound(t, api.getMemberAvatar(reader.AccessToken, id))
		}
	}
	// She still reads her own.
	if got := requireImage(t, api.getAvatar(cho.AccessToken)); !bytes.Equal(got, chos) {
		t.Error("cho did not get her own picture")
	}

	// With a profile, the same picture is public under the profile's id.
	choID := requireProfile(t, api.putProfile(cho.AccessToken, profileJSON(t, "Cho", ""))).ID
	if got := requireImage(t, api.getMemberAvatar(ana.AccessToken, choID)); !bytes.Equal(got, chos) {
		t.Error("ana did not get cho's picture once cho had a profile")
	}
	if !hasAvatar(t, api.getMember(ana.AccessToken, choID)) {
		t.Error("has_avatar is false for a member with a profile and a picture")
	}

	// The account deleted: the picture is gone with it.
	api.exec(t, `DELETE FROM users WHERE email = 'cho@example.com'`)
	requireMemberNotFound(t, api.getMemberAvatar(ana.AccessToken, choID))
	if n := api.avatarCount(t); n != 0 {
		t.Errorf("avatars = %d after the account was deleted, want 0", n)
	}
}

// has_avatar in the member profile follows the picture: an upload, a
// replacement and a removal.
func TestMemberProfileSaysWhetherThereIsAPicture(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "Hi")
	want := memberJSON(t, benID, "Ben", "Hi", noLanguages)
	withPicture := strings.Replace(want, `"has_avatar":false`, `"has_avatar":true`, 1)

	requireMember(t, api.getMember(ana.AccessToken, benID), want)

	requireImage(t, api.putAvatar(ben.AccessToken, pngUpload(t, 64, 64, avatarBlue)))
	requireMember(t, api.getMember(ana.AccessToken, benID), withPicture)
	requireMember(t, api.getMember(ben.AccessToken, benID), withPicture)

	requireImage(t, api.putAvatar(ben.AccessToken, jpegUpload(t, 64, 64, avatarRed)))
	requireMember(t, api.getMember(ana.AccessToken, benID), withPicture)

	requireAvatarRemoved(t, api.deleteAvatar(ben.AccessToken))
	requireMember(t, api.getMember(ana.AccessToken, benID), want)

	// The reader's own picture says nothing about Ben's.
	requireImage(t, api.putAvatar(ana.AccessToken, pngUpload(t, 64, 64, avatarGreen)))
	requireMember(t, api.getMember(ana.AccessToken, benID), want)
}

// The member picture route is read-only, like the member profile route.
func TestMemberAvatarRouteIsReadOnly(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	anas := requireImage(t, api.putAvatar(ana.AccessToken, pngUpload(t, 64, 64, avatarRed)))
	bens := requireImage(t, api.putAvatar(ben.AccessToken, pngUpload(t, 64, 64, avatarBlue)))
	upload := pngUpload(t, 64, 64, avatarGreen)

	for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodPatch, http.MethodDelete} {
		for owner, id := range map[string]string{"another member's id": benID, "one's own id": anaID, "an unknown id": unknownMemberID} {
			for _, body := range [][]byte{nil, upload} {
				rec := api.avatarWith(method, membersPath+id+"/avatar", body, "image/png", "Bearer "+ana.AccessToken)
				if rec.Code != http.StatusMethodNotAllowed {
					t.Errorf("%s with %s: status = %d, want 405", method, owner, rec.Code)
				}
			}
		}
	}
	// Without a token the method is refused the same way: nothing to learn.
	if rec := api.avatarWith(http.MethodDelete, membersPath+benID+"/avatar", nil, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE without a token: status = %d, want 405", rec.Code)
	}

	if got := requireImage(t, api.getAvatar(ana.AccessToken)); !bytes.Equal(got, anas) {
		t.Error("ana's picture changed")
	}
	if got := requireImage(t, api.getAvatar(ben.AccessToken)); !bytes.Equal(got, bens) {
		t.Error("ben's picture changed")
	}
	if n := api.avatarCount(t); n != 2 {
		t.Errorf("avatars = %d, want 2", n)
	}
}

// The mux answers a wrong method before authentication runs, with no service.
func TestAvatarRoutesAllowOnlyTheirMethods(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, nil, Options{})
	for path, methods := range map[string][]string{
		avatarPath: {http.MethodPost, http.MethodPatch},
		membersPath + unknownMemberID + "/avatar": {http.MethodPut, http.MethodPost, http.MethodPatch, http.MethodDelete},
	} {
		for _, method := range methods {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status = %d, want 405", method, path, rec.Code)
			}
		}
	}
}

// ---- A picture is changed only by its owner ----

// Without a usable access token every avatar route answers the same 401,
// stores nothing, removes nothing and returns nothing, and the member route
// answers the same whether or not the id names a profile.
func TestAvatarRoutesRejectMissingOrUnusableCredentials(t *testing.T) {
	api := newTestAPI(t)
	tokens, id := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	current := requireImage(t, api.putAvatar(tokens.AccessToken, pngUpload(t, 64, 64, avatarRed)))
	version := api.avatarRowVersion(t, "ana@example.com")
	revoked := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	requireLoggedOut(t, api.logout(revoked.AccessToken))
	expired := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	api.exec(t, `UPDATE sessions SET access_expires_at = now() - interval '1 second' WHERE access_token_hash = $1`,
		auth.HashToken(expired.AccessToken))
	upload := pngUpload(t, 64, 64, avatarBlue)

	for name, tc := range map[string]struct {
		query  string
		header []string
	}{
		"no header":           {},
		"basic scheme":        {header: []string{"Basic " + tokens.AccessToken}},
		"no scheme":           {header: []string{tokens.AccessToken}},
		"refresh token":       {header: []string{"Bearer " + tokens.RefreshToken}},
		"malformed":           {header: []string{"Bearer " + auth.AccessTokenPrefix + "garbage"}},
		"unknown":             {header: []string{"Bearer " + auth.NewToken(auth.AccessTokenPrefix).Raw}},
		"revoked":             {header: []string{"Bearer " + revoked.AccessToken}},
		"expired":             {header: []string{"Bearer " + expired.AccessToken}},
		"two headers":         {header: []string{"Bearer " + tokens.AccessToken, "Bearer " + tokens.AccessToken}},
		"token only in query": {query: "?access_token=" + tokens.AccessToken},
	} {
		t.Run(name, func(t *testing.T) {
			requireUnauthorized(t, api.avatarWith(http.MethodGet, avatarPath+tc.query, nil, "", tc.header...))
			requireUnauthorized(t, api.avatarWith(http.MethodPut, avatarPath+tc.query, upload, "image/png", tc.header...))
			requireUnauthorized(t, api.avatarWith(http.MethodDelete, avatarPath+tc.query, nil, "", tc.header...))

			existing := api.avatarWith(http.MethodGet, membersPath+id+"/avatar"+tc.query, nil, "", tc.header...)
			requireUnauthorized(t, existing)
			for _, other := range []string{unknownMemberID, "not-an-id", strings.ToUpper(id), api.userID(t, "ana@example.com")} {
				rec := api.avatarWith(http.MethodGet, membersPath+other+"/avatar"+tc.query, nil, "", tc.header...)
				requireUnauthorized(t, rec)
				if rec.Body.String() != existing.Body.String() || fmt.Sprint(rec.Header()) != fmt.Sprint(existing.Header()) {
					t.Errorf("%s answers differently from an existing id:\n%v %s\nvs\n%v %s",
						other, rec.Header(), rec.Body, existing.Header(), existing.Body)
				}
			}
		})
	}

	if got := requireImage(t, api.getAvatar(tokens.AccessToken)); !bytes.Equal(got, current) {
		t.Error("an unauthenticated request changed the picture")
	}
	if got := api.avatarRowVersion(t, "ana@example.com"); got != version {
		t.Errorf("an unauthenticated request rewrote the row: %s → %s", version, got)
	}
	if n := api.avatarCount(t); n != 1 {
		t.Errorf("avatars = %d, want 1", n)
	}
}

// The caller's session is the only thing that selects a picture to write:
// another member's identifiers in the query string point at nobody.
func TestAvatarBelongsToTheCallerOnly(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	bens := requireImage(t, api.putAvatar(ben.AccessToken, pngUpload(t, 64, 64, avatarBlue)))
	naming := "?id=" + benID + "&public_id=" + benID + "&user_id=" + api.userID(t, "ben@example.com")
	bearer := "Bearer " + ana.AccessToken

	// An upload with Ben's ids in the query is Ana's upload.
	anas := requireImage(t, api.avatarWith(http.MethodPut, avatarPath+naming, pngUpload(t, 64, 64, avatarRed), "image/png", bearer))
	requireShows(t, anas, avatarRed)
	if got := requireImage(t, api.avatarWith(http.MethodGet, avatarPath+naming, nil, "", bearer)); !bytes.Equal(got, anas) {
		t.Error("a read with ben's ids in the query did not return ana's picture")
	}
	if got := requireImage(t, api.getMemberAvatar(ana.AccessToken, benID)); !bytes.Equal(got, bens) {
		t.Error("ben's picture changed with ana's upload")
	}
	if got := requireImage(t, api.getMemberAvatar(ben.AccessToken, anaID)); !bytes.Equal(got, anas) {
		t.Error("ana's upload is not ana's picture")
	}

	// A removal with Ben's ids in the query removes Ana's.
	requireAvatarRemoved(t, api.avatarWith(http.MethodDelete, avatarPath+naming, nil, "", bearer))
	requireLoginResponse(t, api.getAvatar(ana.AccessToken), http.StatusNotFound, avatarNotFound)
	if got := requireImage(t, api.getAvatar(ben.AccessToken)); !bytes.Equal(got, bens) {
		t.Error("ben's picture was removed or changed by ana's removal")
	}
	if n := api.avatarCount(t); n != 1 {
		t.Errorf("avatars = %d, want 1", n)
	}
}

// The picture is the member's, not the session's: another session of theirs
// reads the upload, replaces it and removes it.
func TestAvatarIsSharedByAMembersSessions(t *testing.T) {
	api := newTestAPI(t)
	first := api.loggedIn(t, "ana@example.com")
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))

	stored := requireImage(t, api.putAvatar(first.AccessToken, pngUpload(t, 64, 64, avatarRed)))
	if got := requireImage(t, api.getAvatar(second.AccessToken)); !bytes.Equal(got, stored) {
		t.Error("the second session did not read the upload of the first")
	}
	requireAvatarRemoved(t, api.deleteAvatar(second.AccessToken))
	requireLoginResponse(t, api.getAvatar(first.AccessToken), http.StatusNotFound, avatarNotFound)
}

// Mounted without requireAccessToken by mistake, the handlers fail closed.
func TestAvatarHandlersWithoutMiddlewareFailClosed(t *testing.T) {
	api := newTestAPI(t)
	tokens, id := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	requireImage(t, api.putAvatar(tokens.AccessToken, pngUpload(t, 64, 64, avatarRed)))
	version := api.avatarRowVersion(t, "ana@example.com")
	logger := slog.New(slog.DiscardHandler)
	svc := api.avatarService()

	for name, tc := range map[string]struct {
		method string
		h      http.Handler
	}{
		"get":    {http.MethodGet, handleGetAvatar(logger, svc)},
		"put":    {http.MethodPut, handlePutAvatar(logger, svc)},
		"delete": {http.MethodDelete, handleDeleteAvatar(logger, svc)},
		"member": {http.MethodGet, handleGetMemberAvatar(logger, profile.NewService(api.pool, logger), svc)},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, avatarPath, bytes.NewReader(pngUpload(t, 64, 64, avatarBlue)))
		req.SetPathValue("id", id)
		tc.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError || strings.TrimSpace(rec.Body.String()) != internalError {
			t.Errorf("%s: %d %s, want the opaque 500", name, rec.Code, rec.Body)
		}
	}
	if got := api.avatarRowVersion(t, "ana@example.com"); got != version {
		t.Errorf("a handler without an identity wrote: %s → %s", version, got)
	}
}

// A user deleted after authentication but before the write: their sessions
// are gone with them, so the answer is the 401 of a dead credential (016),
// and nothing is stored.
func TestPutAvatarForAUserDeletedMeanwhile(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	id := api.userID(t, "ana@example.com")
	api.exec(t, `DELETE FROM users WHERE id = $1`, id)
	logger := slog.New(slog.DiscardHandler)
	svc := api.avatarService()

	rec := httptest.NewRecorder()
	handlePutAvatar(logger, svc).ServeHTTP(rec,
		as(httptest.NewRequest(http.MethodPut, avatarPath, bytes.NewReader(pngUpload(t, 64, 64, avatarRed))), id))
	requireResponse(t, rec, http.StatusUnauthorized, invalidAccessToken)
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
	if n := api.avatarCount(t); n != 0 {
		t.Errorf("avatars = %d, want 0", n)
	}

	// Their picture went with them: a read finds none, a removal has
	// nothing to do, and their next request is the 401 of a dead session.
	rec = httptest.NewRecorder()
	handleGetAvatar(logger, svc).ServeHTTP(rec, as(httptest.NewRequest(http.MethodGet, avatarPath, nil), id))
	requireResponse(t, rec, http.StatusNotFound, avatarNotFound)
	rec = httptest.NewRecorder()
	handleDeleteAvatar(logger, svc).ServeHTTP(rec, as(httptest.NewRequest(http.MethodDelete, avatarPath, nil), id))
	if rec.Code != http.StatusNoContent {
		t.Errorf("DELETE: status = %d, want 204", rec.Code)
	}
	for _, rec := range []*httptest.ResponseRecorder{
		api.getAvatar(tokens.AccessToken), api.putAvatar(tokens.AccessToken, pngUpload(t, 64, 64, avatarRed)), api.deleteAvatar(tokens.AccessToken),
	} {
		requireUnauthorized(t, rec)
	}
}

// ---- Avatar writes are limited ----

func TestAvatarWritesAreLimitedPerUser(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{AvatarWrite: tightUserLimiter()}})
	ana := api.loggedIn(t, "ana@example.com")
	ben := api.loggedIn(t, "ben@example.com")
	red, blue := pngUpload(t, 64, 64, avatarRed), pngUpload(t, 64, 64, avatarBlue)

	// Requests that don't authenticate never spend anyone's allowance.
	requireUnauthorized(t, api.avatarWith(http.MethodPut, avatarPath, red, "image/png"))
	requireUnauthorized(t, api.avatarWith(http.MethodDelete, avatarPath, nil, ""))
	requireUnauthorized(t, api.putAvatar(auth.NewToken(auth.AccessTokenPrefix).Raw, red))

	stored := requireImage(t, api.putAvatar(ana.AccessToken, red))
	rec := api.putAvatar(ana.AccessToken, blue)
	requireLoginResponse(t, rec, http.StatusTooManyRequests, rateLimited)
	if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
		t.Errorf("Retry-After = %q", got)
	}
	// Uploads and removals share the one bucket, and a 429 is the answer
	// whatever the body is, the same upload again included.
	for name, limited := range map[string]*httptest.ResponseRecorder{
		"a removal":       api.deleteAvatar(ana.AccessToken),
		"the same upload": api.putAvatar(ana.AccessToken, red),
		"junk":            api.putAvatar(ana.AccessToken, []byte("junk")),
		"no body":         api.putAvatar(ana.AccessToken, nil),
	} {
		requireLoginResponse(t, limited, http.StatusTooManyRequests, rateLimited)
		if limited.Body.String() != rec.Body.String() || fmt.Sprint(limited.Header()) != fmt.Sprint(rec.Header()) {
			t.Errorf("%s: limited response differs", name)
		}
	}
	// A 429 did nothing.
	if got := requireImage(t, api.getAvatar(ana.AccessToken)); !bytes.Equal(got, stored) {
		t.Error("a limited request changed the picture")
	}

	// The limit is the user's: a second session of Ana's shares it, Ben has his own.
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	requireLoginResponse(t, api.putAvatar(second.AccessToken, blue), http.StatusTooManyRequests, rateLimited)
	requireLoginResponse(t, api.deleteAvatar(second.AccessToken), http.StatusTooManyRequests, rateLimited)
	requireImage(t, api.putAvatar(ben.AccessToken, blue))

	// Reading is not limited, and the other saves have their own allowances.
	for range 5 {
		requireImage(t, api.getAvatar(ana.AccessToken))
	}
	requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "")))
	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)
}

// A removal spends the bucket for an upload too, and refused uploads cost a
// token like accepted ones: the limit is checked before the body is read.
func TestAvatarWriteLimitCountsEveryWrite(t *testing.T) {
	for name, tc := range map[string]struct {
		send func(api testAPI, token string) *httptest.ResponseRecorder
		want int
	}{
		"a removal of nothing": {func(api testAPI, token string) *httptest.ResponseRecorder { return api.deleteAvatar(token) }, http.StatusNoContent},
		"an empty upload":      {func(api testAPI, token string) *httptest.ResponseRecorder { return api.putAvatar(token, nil) }, http.StatusUnprocessableEntity},
		"not an image": {func(api testAPI, token string) *httptest.ResponseRecorder {
			return api.putAvatar(token, []byte("GIF89a"))
		}, http.StatusUnprocessableEntity},
		"a damaged image": {func(api testAPI, token string) *httptest.ResponseRecorder {
			return api.putAvatar(token, []byte("\x89PNG\r\n\x1a\n"))
		}, http.StatusUnprocessableEntity},
		"too large": {func(api testAPI, token string) *httptest.ResponseRecorder {
			return api.putAvatar(token, make([]byte, avatar.MaxUploadBytes+1))
		}, http.StatusUnprocessableEntity},
	} {
		t.Run(name, func(t *testing.T) {
			api := newTestAPIWith(t, Options{UserLimits: UserLimits{AvatarWrite: tightUserLimiter()}})
			ana := api.loggedIn(t, "ana@example.com")

			if rec := tc.send(api, ana.AccessToken); rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.want, rec.Body)
			}
			requireLoginResponse(t, api.putAvatar(ana.AccessToken, pngUpload(t, 64, 64, avatarRed)), http.StatusTooManyRequests, rateLimited)
			requireLoginResponse(t, api.deleteAvatar(ana.AccessToken), http.StatusTooManyRequests, rateLimited)
			if n := api.avatarCount(t); n != 0 {
				t.Errorf("avatars = %d, want 0", n)
			}
		})
	}
}

// The avatar bucket is its own: spending it leaves the profile's, the
// languages' and the member reads', and spending those leaves it.
func TestAvatarWriteLimitIsSeparateFromTheOthers(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{
		ProfileWrite: tightUserLimiter(), LanguagesWrite: tightUserLimiter(),
		AvatarWrite: tightUserLimiter(), MemberRead: tightUserLimiter()}})
	ana := api.loggedIn(t, "ana@example.com")

	requireImage(t, api.putAvatar(ana.AccessToken, pngUpload(t, 64, 64, avatarRed)))
	requireLoginResponse(t, api.deleteAvatar(ana.AccessToken), http.StatusTooManyRequests, rateLimited)

	id := requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", ""))).ID
	requireLoginResponse(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "")), http.StatusTooManyRequests, rateLimited)
	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)
	requireLoginResponse(t, api.putLanguages(ana.AccessToken, anasLanguages), http.StatusTooManyRequests, rateLimited)
	requireImage(t, api.getMemberAvatar(ana.AccessToken, id))
	requireLoginResponse(t, api.getMemberAvatar(ana.AccessToken, id), http.StatusTooManyRequests, rateLimited)

	// Reading one's own picture is limited by none of them.
	requireImage(t, api.getAvatar(ana.AccessToken))
}

// A member's profile and picture are one view and share the reader's
// member-read allowance: one view spends two.
func TestMemberProfileAndAvatarShareTheMemberReadLimit(t *testing.T) {
	for name, order := range map[string][2]string{
		"the profile, then the picture": {"", "/avatar"},
		"the picture, then the profile": {"/avatar", ""},
	} {
		t.Run(name, func(t *testing.T) {
			api := newTestAPIWith(t, Options{UserLimits: UserLimits{MemberRead: tightUserLimiter()}})
			ana := api.loggedIn(t, "ana@example.com")
			ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
			requireImage(t, api.putAvatar(ben.AccessToken, pngUpload(t, 64, 64, avatarBlue)))
			read := func(tokens loginTokens, suffix string) *httptest.ResponseRecorder {
				return api.avatarWith(http.MethodGet, membersPath+benID+suffix, nil, "", "Bearer "+tokens.AccessToken)
			}

			// Requests that don't authenticate never spend anyone's allowance.
			requireUnauthorized(t, api.avatarWith(http.MethodGet, membersPath+benID+"/avatar", nil, ""))

			if rec := read(ana, order[0]); rec.Code != http.StatusOK {
				t.Fatalf("first read: status = %d, body %.200s", rec.Code, rec.Body)
			}
			rec := read(ana, order[1])
			requireLoginResponse(t, rec, http.StatusTooManyRequests, rateLimited)
			if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
				t.Errorf("Retry-After = %q", got)
			}

			// The allowance is the reader's: Ben has his own, and Ana's own
			// picture routes are untouched by it.
			if rec := read(ben, "/avatar"); rec.Code != http.StatusOK {
				t.Errorf("ben's read: status = %d", rec.Code)
			}
			requireImage(t, api.putAvatar(ana.AccessToken, pngUpload(t, 64, 64, avatarRed)))
			requireImage(t, api.getAvatar(ana.AccessToken))
			requireAvatarRemoved(t, api.deleteAvatar(ana.AccessToken))
		})
	}
}

// A picture read that finds nothing costs a token too.
func TestMemberAvatarReadLimitCountsMisses(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{MemberRead: tightUserLimiter()}})
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	requireLoginResponse(t, api.getMemberAvatar(ana.AccessToken, anaID), http.StatusNotFound, avatarNotFound)
	requireLoginResponse(t, api.getMemberAvatar(ana.AccessToken, anaID), http.StatusTooManyRequests, rateLimited)
}

func TestNewUserLimitsForAvatarsMatchTheDecisionLog(t *testing.T) {
	l := NewUserLimits(slog.New(slog.DiscardHandler))
	const burst = 5
	for i := range burst {
		if ok, _ := l.AvatarWrite.Allow("user"); !ok {
			t.Fatalf("denied at %d, burst is %d", i+1, burst)
		}
	}
	ok, retryAfter := l.AvatarWrite.Allow("user")
	if ok {
		t.Errorf("allowed past burst %d", burst)
	}
	if retryAfter <= 6*time.Second || retryAfter > time.Minute {
		t.Errorf("retry after %v, want within the 1 min refill and longer than the other writes'", retryAfter)
	}
	// Its own bucket, and one per user.
	if ok, _ := l.ProfileWrite.Allow("user"); !ok {
		t.Error("spending the avatar limit spent the profile's")
	}
	if ok, _ := l.LanguagesWrite.Allow("user"); !ok {
		t.Error("spending the avatar limit spent the languages'")
	}
	if ok, _ := l.MemberRead.Allow("user"); !ok {
		t.Error("spending the avatar limit spent the member reads'")
	}
	if ok, _ := l.AvatarWrite.Allow("another user"); !ok {
		t.Error("one user's limit refused another")
	}
}

// ---- 503 ----

// When no decode slot comes free in time the service answers
// avatar.ErrOverloaded (verified with every slot held in the avatar
// package, whose slots can't be reached from here): the API turns it into
// 503 service_unavailable with Retry-After, never into a refusal of the
// photo, and logs it without anything of the upload.
func TestAvatarOverloadIsUnavailable(t *testing.T) {
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	for _, err := range []error{
		avatar.ErrOverloaded,
		fmt.Errorf("avatar: save: %w", avatar.ErrOverloaded),
		fmt.Errorf("avatar: save: %w", context.DeadlineExceeded),
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, avatarPath, nil)
		req.Pattern = "PUT " + avatarPath
		rec.Header().Set("Cache-Control", "no-store") // as requireAccessToken leaves it
		writeServiceError(rec, req, logger, err)
		requireLoginResponse(t, rec, http.StatusServiceUnavailable, serviceUnavailable)
		if got := rec.Header().Get("Retry-After"); got != "5" {
			t.Errorf("%v: Retry-After = %q, want 5", err, got)
		}
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 3 {
		t.Errorf("%d of the three 503s logged at WARN:\n%s", n, logs)
	}
}

// A request whose time is already up does no work at all and answers 503;
// the retry it invites then succeeds.
func TestAvatarRoutesWithAnExpiredDeadlineAreUnavailable(t *testing.T) {
	api := newTestAPI(t)
	tokens, publicID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	id := api.userID(t, "ana@example.com")
	logger := slog.New(slog.DiscardHandler)
	svc := api.avatarService()
	upload := pngUpload(t, 64, 64, avatarRed)

	for name, tc := range map[string]struct {
		method string
		h      http.Handler
	}{
		"get":    {http.MethodGet, handleGetAvatar(logger, svc)},
		"put":    {http.MethodPut, handlePutAvatar(logger, svc)},
		"delete": {http.MethodDelete, handleDeleteAvatar(logger, svc)},
		"member": {http.MethodGet, handleGetMemberAvatar(logger, profile.NewService(api.pool, logger), svc)},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			req := as(httptest.NewRequest(tc.method, avatarPath, bytes.NewReader(upload)).WithContext(ctx), id)
			req.SetPathValue("id", publicID)
			rec := httptest.NewRecorder()
			rec.Header().Set("Cache-Control", "no-store") // as requireAccessToken leaves it
			tc.h.ServeHTTP(rec, req)
			requireLoginResponse(t, rec, http.StatusServiceUnavailable, serviceUnavailable)
			if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
				t.Errorf("Retry-After = %q", got)
			}
		})
	}
	if n := api.avatarCount(t); n != 0 {
		t.Errorf("avatars = %d, want 0", n)
	}
	requireImage(t, api.putAvatar(tokens.AccessToken, upload))
}

// ---- Pictures are personal data ----

var avatarLogLine = regexp.MustCompile(`^time=\S+ level=INFO msg="avatar: (saved|removed)" user_id=([0-9a-f-]{36})$`)

// The logs record that a member's picture changed, with their user id and
// nothing else: nothing of the image, of the request or of who read it.
func TestAvatarLogsHoldTheUserIDOnly(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben := api.loggedIn(t, "ben@example.com")
	const marker = "MARKERCOMMENT"
	file := pngUpload(t, 300, 200, avatarRed)
	const afterHeader = 8 + 25
	raw := append([]byte{}, file[:afterHeader]...)
	raw = append(raw, pngChunkOf("tEXt", []byte("Comment\x00"+marker))...)
	raw = append(raw, file[afterHeader:]...)
	api.svc.Wait()
	before := api.logs.String()

	requireImage(t, api.putAvatar(ana.AccessToken, raw))
	requireImage(t, api.putAvatar(ana.AccessToken, raw)) // a replay: not logged
	requireImage(t, api.getAvatar(ana.AccessToken))
	requireImage(t, api.getMemberAvatar(ben.AccessToken, anaID))
	requireAvatarRefused(t, api.putAvatar(ana.AccessToken, []byte(marker)), "unsupported_type")
	requireAvatarRefused(t, api.putAvatar(ana.AccessToken, raw[:len(raw)-30]), "invalid_image")
	requireAvatarRefused(t, api.putAvatar(ana.AccessToken, pngDeclaring(8000, 8000)), "dimensions_too_large")
	requireAvatarRemoved(t, api.deleteAvatar(ana.AccessToken))
	requireAvatarRemoved(t, api.deleteAvatar(ana.AccessToken)) // nothing to remove: not logged
	requireLoginResponse(t, api.getAvatar(ana.AccessToken), http.StatusNotFound, avatarNotFound)
	requireLoginResponse(t, api.getMemberAvatar(ben.AccessToken, anaID), http.StatusNotFound, avatarNotFound)
	requireMemberNotFound(t, api.getMemberAvatar(ben.AccessToken, unknownMemberID))

	api.svc.Wait()
	added := strings.TrimPrefix(api.logs.String(), before)
	lines := strings.Split(strings.TrimSpace(added), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines logged, want a save and a removal:\n%s", len(lines), added)
	}
	account := api.userID(t, "ana@example.com")
	for i, what := range []string{"saved", "removed"} {
		m := avatarLogLine.FindStringSubmatch(lines[i])
		if m == nil || m[1] != what || m[2] != account {
			t.Errorf("line %d = %q, want only that ana's picture was %s", i+1, lines[i], what)
		}
	}
	for _, private := range []string{marker, anaID, unknownMemberID, ana.AccessToken, ben.AccessToken, "ana@example.com", "png", "jpeg", "8000"} {
		if strings.Contains(strings.ToLower(added), strings.ToLower(private)) {
			t.Errorf("logs contain %q: %s", private, added)
		}
	}
}

// A failure inside is an opaque 500, logged with the route's pattern and
// nothing of the upload, the picture or the id asked for.
func TestAvatarInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	requireImage(t, api.putAvatar(ana.AccessToken, pngUpload(t, 64, 64, avatarRed)))
	// Break every query by hiding the column each of them names.
	api.exec(t, `ALTER TABLE avatars RENAME COLUMN user_id TO test_hidden`)
	t.Cleanup(func() {
		_, _ = api.pool.Exec(context.Background(), `ALTER TABLE avatars RENAME COLUMN test_hidden TO user_id`)
	})

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"get":            api.getAvatar(ana.AccessToken),
		"put":            api.putAvatar(ana.AccessToken, pngUpload(t, 64, 64, avatarBlue)),
		"delete":         api.deleteAvatar(ana.AccessToken),
		"member picture": api.getMemberAvatar(ana.AccessToken, anaID),
		"member profile": api.getMember(ana.AccessToken, anaID),
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, rec, http.StatusInternalServerError, internalError)
			if len(rec.Header()["Retry-After"]) != 0 || len(rec.Header()["Www-Authenticate"]) != 0 {
				t.Errorf("headers = %v", rec.Header())
			}
		})
	}
	// A refused upload and a malformed id still never reach the broken queries.
	requireAvatarRefused(t, api.putAvatar(ana.AccessToken, []byte("junk")), "unsupported_type")
	requireMemberNotFound(t, api.getMemberAvatar(ana.AccessToken, "not-an-id"))

	api.svc.Wait()
	logs := api.logs.String()
	for _, route := range []string{
		"GET " + avatarPath, "PUT " + avatarPath, "DELETE " + avatarPath, "GET /v1/profiles/{id}/avatar", "GET /v1/profiles/{id}",
	} {
		if !strings.Contains(logs, `route="`+route+`"`) {
			t.Errorf("the failure of %s was not logged with its route: %s", route, logs)
		}
	}
	if n := strings.Count(logs, "request failed"); n != 5 {
		t.Errorf("%d failures logged, want 5", n)
	}
	for _, private := range []string{anaID, ana.AccessToken, "ana@example.com"} {
		if strings.Contains(logs, private) {
			t.Errorf("logs contain %q: %s", private, logs)
		}
	}
}
