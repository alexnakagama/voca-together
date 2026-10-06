package avatar

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// requireRefused checks that err is a validation error with the one reason.
func requireRefused(t *testing.T, err error, reason *FieldError) {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, want a validation error with %v", err, reason)
	}
	if len(verr.Fields) != 1 || verr.Fields[0] != reason {
		t.Fatalf("err = %v, want %v", err, reason)
	}
	if verr.Fields[0].Field != "avatar" {
		t.Errorf("field = %q, want avatar", verr.Fields[0].Field)
	}
}

func TestErrorCodes(t *testing.T) {
	for code, reason := range map[string]*FieldError{
		"required": ErrRequired, "too_large": ErrTooLarge, "unsupported_type": ErrUnsupportedType,
		"invalid_image": ErrInvalidImage, "dimensions_too_large": ErrDimensionsTooLarge,
	} {
		if reason.Field != "avatar" || reason.Code != code {
			t.Errorf("%s = %+v", code, reason)
		}
		if got, want := refused(reason).Error(), "avatar: invalid input: avatar: "+code; got != want {
			t.Errorf("message = %q, want %q", got, want)
		}
	}
}

func TestInspectAcceptsJPEGAndPNGByContent(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  []byte
		want format
	}{
		"jpeg":                     {encodeJPEG(t, solid(64, 48, red)), formatJPEG},
		"png":                      {encodePNG(t, solid(64, 48, red)), formatPNG},
		"one pixel png":            {encodePNG(t, solid(1, 1, red)), formatPNG},
		"one pixel jpeg":           {encodeJPEG(t, solid(1, 1, red)), formatJPEG},
		"jpeg with metadata":       {orientedJPEG(t, scene(64, 48), 6), formatJPEG},
		"jpeg with trailing bytes": {append(encodeJPEG(t, solid(8, 8, red)), "trailing"...), formatJPEG},
		"png with trailing bytes":  {append(encodePNG(t, solid(8, 8, red)), "trailing"...), formatPNG},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := inspect(tc.raw)
			if err != nil || got != tc.want {
				t.Errorf("inspect = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestInspectRequired(t *testing.T) {
	for _, raw := range [][]byte{nil, {}} {
		_, err := inspect(raw)
		requireRefused(t, err, ErrRequired)
	}
}

// The limit is on the bytes sent: exactly MaxUploadBytes is looked at, one
// more is refused whatever it holds.
func TestInspectTooLarge(t *testing.T) {
	valid := encodePNG(t, solid(64, 64, red))
	padded := func(size int) []byte { return append(valid, make([]byte, size-len(valid))...) }

	if f, err := inspect(padded(MaxUploadBytes)); err != nil || f != formatPNG {
		t.Errorf("an upload of exactly the limit: %v, %v; want it accepted", f, err)
	}
	_, err := inspect(padded(MaxUploadBytes + 1))
	requireRefused(t, err, ErrTooLarge)

	// Size is judged before content: junk over the limit is too_large too.
	for name, raw := range map[string][]byte{
		"text over the limit":  bytes.Repeat([]byte("a"), MaxUploadBytes+1),
		"zeros over the limit": make([]byte, MaxUploadBytes+1),
		"far over the limit":   make([]byte, 3*MaxUploadBytes),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := inspect(raw)
			requireRefused(t, err, ErrTooLarge)
		})
	}
	// And junk of exactly the limit is judged by its content.
	_, err = inspect(bytes.Repeat([]byte("a"), MaxUploadBytes))
	requireRefused(t, err, ErrUnsupportedType)
}

func TestInspectUnsupportedType(t *testing.T) {
	for name, raw := range map[string][]byte{
		"gif":                  []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\xff\xff\xff\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;"),
		"gif87":                []byte("GIF87a\x01\x00\x01\x00"),
		"webp":                 []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00"),
		"pdf":                  []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n"),
		"text":                 []byte("just some text, not an image"),
		"html":                 []byte("<!doctype html><script>alert(1)</script>"),
		"svg":                  []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"bmp":                  []byte("BM\x46\x00\x00\x00\x00\x00\x00\x00"),
		"tiff":                 []byte("II*\x00\x08\x00\x00\x00"),
		"heic":                 []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00"),
		"zip":                  []byte("PK\x03\x04\x14\x00"),
		"one byte":             {0xFF},
		"two bytes of a jpeg":  {0xFF, 0xD8},
		"seven bytes of a png": pngSignature[:7],
		"png with a wrong byte": append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\r'},
			encodePNG(t, solid(4, 4, red))[8:]...),
		"a png after one byte":   append([]byte{' '}, encodePNG(t, solid(4, 4, red))...),
		"a jpeg after a newline": append([]byte{'\n'}, encodeJPEG(t, solid(4, 4, red))...),
		"zeros":                  make([]byte, 64),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := inspect(raw)
			requireRefused(t, err, ErrUnsupportedType)
		})
	}
}

// It starts as a JPEG or a PNG and its header can't be read.
func TestInspectInvalidImage(t *testing.T) {
	jpegFile, pngFile := encodeJPEG(t, solid(64, 48, red)), encodePNG(t, solid(64, 48, red))
	corruptPNG := append([]byte{}, pngFile...)
	corruptPNG[20] ^= 0xFF // inside IHDR: its checksum no longer matches

	for name, raw := range map[string][]byte{
		"jpeg signature only":         {0xFF, 0xD8, 0xFF},
		"jpeg truncated in a segment": jpegFile[:20],
		"jpeg truncated mid-tables":   jpegFile[:100],
		"jpeg signature then text":    append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, "not really a jpeg at all"...),
		"png signature only":          pngSignature,
		"png truncated in its header": pngFile[:20],
		"png with a corrupt header":   corruptPNG,
		"png signature then text":     append(append([]byte{}, pngSignature...), "not really a png at all"...),
		"png of zero width":           pngHeaderOnly(0, 10),
		"png of zero height":          pngHeaderOnly(10, 0),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := inspect(raw)
			requireRefused(t, err, ErrInvalidImage)
		})
	}
}

// 4096 on a side is accepted and 4097 is not, on each side and in each
// format; and what counts is what the file declares, not how big it is.
func TestInspectDimensions(t *testing.T) {
	small := encodeJPEG(t, solid(16, 16, red))
	for name, tc := range map[string]struct {
		raw     []byte
		refused bool
	}{
		"png 4096 wide":            {raw: encodePNG(t, solid(MaxDimension, 1, red))},
		"png 4096 high":            {raw: encodePNG(t, solid(1, MaxDimension, red))},
		"png 4097 wide":            {raw: encodePNG(t, solid(MaxDimension+1, 1, red)), refused: true},
		"png 4097 high":            {raw: encodePNG(t, solid(1, MaxDimension+1, red)), refused: true},
		"jpeg 4096 wide":           {raw: encodeJPEG(t, solid(MaxDimension, 8, red))},
		"jpeg 4096 high":           {raw: encodeJPEG(t, solid(8, MaxDimension, red))},
		"jpeg 4097 wide":           {raw: encodeJPEG(t, solid(MaxDimension+1, 8, red)), refused: true},
		"jpeg 4097 high":           {raw: encodeJPEG(t, solid(8, MaxDimension+1, red)), refused: true},
		"png declaring 4096x4096":  {raw: pngHeaderOnly(MaxDimension, MaxDimension)},
		"png declaring 4097x4096":  {raw: pngHeaderOnly(MaxDimension+1, MaxDimension), refused: true},
		"png declaring 4096x4097":  {raw: pngHeaderOnly(MaxDimension, MaxDimension+1), refused: true},
		"png declaring 30000²":     {raw: pngHeaderOnly(30000, 30000), refused: true},
		"png declaring 1x1000000":  {raw: pngHeaderOnly(1, 1_000_000), refused: true},
		"jpeg declaring 4096x4096": {raw: withJPEGDimensions(t, small, MaxDimension, MaxDimension)},
		"jpeg declaring 4097x16":   {raw: withJPEGDimensions(t, small, MaxDimension+1, 16), refused: true},
		"jpeg declaring 16x4097":   {raw: withJPEGDimensions(t, small, 16, MaxDimension+1), refused: true},
		"jpeg declaring 65535²":    {raw: withJPEGDimensions(t, small, 65535, 65535), refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := inspect(tc.raw)
			if tc.refused {
				requireRefused(t, err, ErrDimensionsTooLarge)
			} else if err != nil {
				t.Errorf("err = %v, want it accepted", err)
			}
		})
	}

	// The files that declare the largest images here are tiny: the decision
	// is taken from the header, before any pixel is decoded.
	for name, raw := range map[string][]byte{
		"png":  pngHeaderOnly(30000, 30000),
		"jpeg": withJPEGDimensions(t, small, 65535, 65535),
	} {
		if len(raw) > 1024 {
			t.Errorf("the %s fixture is %d bytes, want a small file", name, len(raw))
		}
		// A normalizer with no free slot still refuses it at once: it never
		// asked for one.
		n := newNormalizer(1, decodeQueueTimeout)
		n.slots <- struct{}{}
		_, err := n.normalize(context.Background(), raw)
		requireRefused(t, err, ErrDimensionsTooLarge)
	}
}

// Whatever is wrong with an upload, the error names the field and the code
// and holds nothing of what was sent, nor what a decoder said about it.
func TestErrorsNeverContainTheInput(t *testing.T) {
	const marker = "MARKERINPUT"
	jpegFile := encodeJPEG(t, solid(64, 48, red))
	pngFile := encodePNG(t, solid(64, 48, red))
	n := testNormalizer()

	for name, raw := range map[string][]byte{
		"text":                 []byte(marker + " is not an image"),
		"too large":            bytes.Repeat([]byte(marker), MaxUploadBytes/len(marker)+1),
		"jpeg cut short":       append(append([]byte{}, jpegFile[:20]...), marker...),
		"jpeg with a bad body": append(append([]byte{}, jpegFile[:len(jpegFile)/2]...), strings.Repeat(marker, 50)...),
		"png cut short":        append(append([]byte{}, pngFile[:20]...), marker...),
		"png with a bad body":  append(append([]byte{}, pngFile[:len(pngFile)-20]...), marker...),
		"png with a bad chunk": withPNGChunks(pngFile, []byte("\x00\x00\x00\x0btEXt"+marker+"\x00\x00\x00\x00")),
		"too many pixels":      withPNGChunks(pngHeaderOnly(MaxDimension+1, 1), pngChunk("tEXt", []byte(marker))),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := n.normalize(context.Background(), raw)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("err = %v, want a validation error", err)
			}
			msg := err.Error()
			if strings.Contains(msg, marker) || strings.Contains(msg, "MARKER") {
				t.Errorf("the error holds the input: %q", msg)
			}
			if !strings.HasPrefix(msg, "avatar: invalid input: avatar: ") || len(msg) > 60 {
				t.Errorf("the error is more than a field and a code: %q", msg)
			}
			if errors.Unwrap(err) != nil {
				t.Errorf("the error wraps another: %v", errors.Unwrap(err))
			}
		})
	}
}
