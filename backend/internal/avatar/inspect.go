package avatar

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
)

// format is what an upload's content is. Only these two are accepted, and
// both are decoded by the standard library.
type format int

const (
	formatJPEG format = iota + 1
	formatPNG
)

var (
	jpegSignature = []byte{0xFF, 0xD8, 0xFF}
	pngSignature  = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}
)

// sniff returns what raw's first bytes say it is. The content decides:
// nothing a client declares about an upload is ever consulted.
func sniff(raw []byte) (format, bool) {
	switch {
	case bytes.HasPrefix(raw, jpegSignature):
		return formatJPEG, true
	case bytes.HasPrefix(raw, pngSignature):
		return formatPNG, true
	}
	return 0, false
}

// inspect runs every check that needs no pixel buffer and returns what raw
// is, or a *ValidationError naming the first rule it breaks, in this order:
// required (empty), too_large (over MaxUploadBytes), unsupported_type
// (neither signature), invalid_image (the header can't be read),
// dimensions_too_large (a side over MaxDimension).
//
// The dimensions come from the image's header alone. This is the guard
// against a small file that declares an enormous image: it is refused here,
// before anything is allocated for its pixels.
func inspect(raw []byte) (format, error) {
	switch {
	case len(raw) == 0:
		return 0, refused(ErrRequired)
	case len(raw) > MaxUploadBytes:
		return 0, refused(ErrTooLarge)
	}
	f, ok := sniff(raw)
	if !ok {
		return 0, refused(ErrUnsupportedType)
	}
	cfg, err := decodeConfig(f, raw)
	if err != nil {
		// The decoder's message is dropped: it may quote the input.
		return 0, refused(ErrInvalidImage)
	}
	if cfg.Width > MaxDimension || cfg.Height > MaxDimension {
		return 0, refused(ErrDimensionsTooLarge)
	}
	if cfg.Width < 1 || cfg.Height < 1 {
		return 0, refused(ErrInvalidImage)
	}
	return f, nil
}

// decodeConfig reads raw's header with the decoder of f and no other: the
// image package's registry of formats is not used, so no import elsewhere in
// the program can widen what is accepted.
func decodeConfig(f format, raw []byte) (image.Config, error) {
	if f == formatPNG {
		return png.DecodeConfig(bytes.NewReader(raw))
	}
	return jpeg.DecodeConfig(bytes.NewReader(raw))
}

// decode decodes raw's pixels with the decoder of f.
func decode(f format, raw []byte) (image.Image, error) {
	if f == formatPNG {
		return png.Decode(bytes.NewReader(raw))
	}
	return jpeg.Decode(bytes.NewReader(raw))
}
