package avatar

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"time"

	xdraw "golang.org/x/image/draw"
)

// normalizer turns uploads into stored pictures, a bounded number at a time.
type normalizer struct {
	// slots bounds how many uploads are decoded at once; a request that
	// waits longer than queueTimeout for one gets ErrOverloaded.
	slots        chan struct{}
	queueTimeout time.Duration
	// render does the work that needs a slot. It is a field so that tests
	// can hold a slot for as long as they need; it is always render.
	render func(f format, raw []byte) ([]byte, error)
}

// newNormalizer returns a normalizer that decodes at most slots uploads at
// once and lets a request wait queueTimeout for its turn.
func newNormalizer(slots int, queueTimeout time.Duration) *normalizer {
	return &normalizer{slots: make(chan struct{}, slots), queueTimeout: queueTimeout, render: render}
}

// normalize returns the picture to store for the upload raw: a Size by Size
// JPEG made from raw's decoded pixels, with the orientation raw records
// applied, the centre square kept, transparency turned white, and nothing
// else of raw in it. The same raw always gives the same bytes.
//
// An upload that is not acceptable returns a *ValidationError and never
// waits for a slot: the checks that need no pixel buffer run first
// (inspect). Otherwise it returns ErrOverloaded if no slot came free in
// time, or the context's error if ctx ended while waiting.
func (n *normalizer) normalize(ctx context.Context, raw []byte) ([]byte, error) {
	f, err := inspect(raw)
	if err != nil {
		return nil, err
	}
	return n.produce(ctx, f, raw)
}

// produce is normalize for an upload that inspect already accepted as f: the
// part that needs a decode slot.
func (n *normalizer) produce(ctx context.Context, f format, raw []byte) ([]byte, error) {
	var out []byte
	if err := n.withSlot(ctx, func() (err error) {
		out, err = n.render(f, raw)
		return err
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// withSlot runs work once a decode slot is free. Without this bound,
// concurrent uploads could each hold the pixels of a MaxDimension square
// image and exhaust memory; with it the worst case is a constant. Waiting
// requests give up when their context ends or with ErrOverloaded after
// queueTimeout, as argon2 work does (auth.Service.withHashSlot, decision
// 018). The work itself is not interrupted: it is bounded by the size of the
// image, not by the context.
func (n *normalizer) withSlot(ctx context.Context, work func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout := time.NewTimer(n.queueTimeout)
	defer timeout.Stop()
	select {
	case n.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout.C:
		return ErrOverloaded
	}
	defer func() { <-n.slots }()
	return work()
}

// render decodes raw, which inspect accepted as f, and returns the stored
// picture. A decoding failure is a *ValidationError (invalid_image); the
// decoder's own message is dropped, since it may quote the input.
func render(f format, raw []byte) ([]byte, error) {
	src, err := decode(f, raw)
	if err != nil {
		return nil, refused(ErrInvalidImage)
	}
	b := src.Bounds()
	// What was decoded is what inspect measured; checked again rather than
	// relied on, since everything below sizes itself by it.
	if b.Dx() < 1 || b.Dy() < 1 || b.Dx() > MaxDimension || b.Dy() > MaxDimension {
		return nil, refused(ErrInvalidImage)
	}

	// The centre square, scaled onto white. Drawing "over" is what turns
	// transparent areas white.
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	square := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.Draw(square, square.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(square, square.Bounds(), src, image.Rect(x0, y0, x0+side, y0+side), xdraw.Over, nil)

	// The orientation is applied to the small square, not to the upload:
	// the centre square of an image is the centre square of the same image
	// turned or mirrored, so the result is the same and costs far less.
	orientation := orientationUpright
	if f == formatJPEG {
		orientation = jpegOrientation(raw)
	}
	upright := orient(square, orientation)

	var out bytes.Buffer
	// The encoder writes the tables and the pixels and nothing else: no
	// EXIF, comment, profile or timestamp.
	if err := jpeg.Encode(&out, upright, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("avatar: encode: %w", err)
	}
	return out.Bytes(), nil
}

// orient returns the square image stored as it must be seen, given its EXIF
// orientation; orientationUpright returns it as it is. For each orientation
// the tag says where the stored image's row 0 and column 0 belong:
//
//	1 top, left      2 top, right      3 bottom, right   4 bottom, left
//	5 left, top      6 right, top      7 right, bottom   8 left, bottom
func orient(stored *image.RGBA, orientation int) *image.RGBA {
	if orientation == orientationUpright {
		return stored
	}
	size := stored.Bounds().Dx()
	last := size - 1
	upright := image.NewRGBA(stored.Bounds())
	for y := range size {
		for x := range size {
			sx, sy := x, y
			switch orientation {
			case 2:
				sx, sy = last-x, y
			case 3:
				sx, sy = last-x, last-y
			case 4:
				sx, sy = x, last-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, last-x
			case 7:
				sx, sy = last-y, last-x
			case 8:
				sx, sy = last-y, x
			}
			upright.SetRGBA(x, y, stored.RGBAAt(sx, sy))
		}
	}
	return upright
}
