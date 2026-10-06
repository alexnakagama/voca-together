package avatar

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustNormalize(t testing.TB, raw []byte) []byte {
	t.Helper()
	out, err := testNormalizer().normalize(context.Background(), raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return out
}

// Whatever its shape and size, an accepted upload becomes a Size by Size
// JPEG of its centre square: the margins a crop drops are nowhere in it.
func TestNormalizeKeepsTheCentreSquareAtTheStoredSize(t *testing.T) {
	for name, upright := range map[string]*image.NRGBA{
		"landscape":            scene(300, 200),
		"portrait":             scene(200, 300),
		"very wide":            scene(1200, 80),
		"very tall":            scene(80, 1200),
		"small, scaled up":     scene(100, 100),
		"small, even smaller":  scene(16, 16),
		"already the size":     scene(Size, Size),
		"3000 by 2000":         scene(3000, 2000),
		"the largest accepted": scene(MaxDimension, MaxDimension),
	} {
		t.Run(name, func(t *testing.T) {
			for format, raw := range map[string][]byte{"png": encodePNG(t, upright), "jpeg": encodeJPEG(t, upright)} {
				if len(raw) > MaxUploadBytes {
					t.Fatalf("the %s fixture is %d bytes, over the limit", format, len(raw))
				}
				requireQuadrants(t, decodeStored(t, mustNormalize(t, raw)), format)
			}
		})
	}
}

// Four pixels, one of each colour, as a PNG: a JPEG that small can't hold
// four colours, so it is no fixture for this.
func TestNormalizeFourPixels(t *testing.T) {
	requireQuadrants(t, decodeStored(t, mustNormalize(t, encodePNG(t, scene(2, 2)))), "2 by 2")
}

// A one-pixel image is an image: it becomes a square of its colour.
func TestNormalizeOnePixel(t *testing.T) {
	img := decodeStored(t, mustNormalize(t, encodePNG(t, solid(1, 1, blue))))
	for _, p := range []image.Point{{0, 0}, {Size / 2, Size / 2}, {Size - 1, Size - 1}} {
		requireColour(t, img, p.X, p.Y, blue, "one pixel")
	}
}

// Each of the eight orientations a camera can record gives the same upright
// picture as the upright file does.
func TestNormalizeAppliesTheOrientation(t *testing.T) {
	for _, upright := range []*image.NRGBA{scene(320, 240), scene(240, 320), scene(256, 256)} {
		for orientation := 1; orientation <= 8; orientation++ {
			for _, byteOrder := range []string{"II", "MM"} {
				stored := encodeJPEG(t, storedAs(upright, orientation))
				raw := withJPEGSegments(stored, exifOrientation(byteOrder, orientation))
				what := "orientation " + string(rune('0'+orientation)) + " " + byteOrder
				requireQuadrants(t, decodeStored(t, mustNormalize(t, raw)), what)
			}
		}
	}
}

// The meaning of each value, straight from the EXIF table and independent of
// the fixtures above: where the stored image's first row and first column
// are shown.
func TestNormalizeOrientationFollowsTheEXIFTable(t *testing.T) {
	// Row 0 is a red band, column 0 a blue one; their corner is black.
	stored := solid(200, 200, white)
	fill(stored, image.Rect(0, 0, 200, 40), red)
	fill(stored, image.Rect(0, 0, 40, 200), blue)
	fill(stored, image.Rect(0, 0, 40, 40), black)
	file := encodeJPEG(t, stored)

	const mid, near, far = Size / 2, 30, Size - 30
	sides := map[string]image.Point{"top": {mid, near}, "bottom": {mid, far}, "left": {near, mid}, "right": {far, mid}}
	for orientation, shown := range map[int][2]string{ // row 0, column 0
		1: {"top", "left"}, 2: {"top", "right"}, 3: {"bottom", "right"}, 4: {"bottom", "left"},
		5: {"left", "top"}, 6: {"right", "top"}, 7: {"right", "bottom"}, 8: {"left", "bottom"},
	} {
		img := decodeStored(t, mustNormalize(t, withJPEGSegments(file, exifOrientation("MM", orientation))))
		for side, p := range sides {
			want := white
			switch side {
			case shown[0]:
				want = red
			case shown[1]:
				want = blue
			}
			requireColour(t, img, p.X, p.Y, want, "orientation "+string(rune('0'+orientation))+", "+side+" side")
		}
	}
}

// Without a tag, with one that can't be trusted, and in a PNG, the pixels
// are shown as stored.
func TestNormalizeWithoutAUsableOrientationIsUpright(t *testing.T) {
	upright := scene(320, 240)
	file := encodeJPEG(t, upright)
	for name, raw := range map[string][]byte{
		"no exif":       file,
		"value 0":       withJPEGSegments(file, exifOrientation("MM", 0)),
		"value 9":       withJPEGSegments(file, exifOrientation("MM", 9)),
		"damaged exif":  withJPEGSegments(file, jpegSegment(markerAPP1, []byte("Exif\x00\x00MM\x00\x2a\xff\xff\xff\xff"))),
		"a png":         encodePNG(t, upright),
		"exif in a png": withPNGChunks(encodePNG(t, upright), pngChunk("eXIf", exifBody("MM", ifdEntry("MM", tagOrientation, typeShort, 1, 6))[6:])),
	} {
		t.Run(name, func(t *testing.T) {
			requireQuadrants(t, decodeStored(t, mustNormalize(t, raw)), name)
		})
	}
}

// Transparent areas come out white, whatever colour hides under the alpha,
// and half-transparent ones are blended with white.
func TestNormalizeTurnsTransparencyWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	fill(img, image.Rect(0, 0, 100, 100), color.NRGBA{R: 255, A: 0})       // invisible red
	fill(img, image.Rect(100, 0, 200, 100), color.NRGBA{})                 // nothing at all
	fill(img, image.Rect(0, 100, 100, 200), color.NRGBA{B: 255, A: 128})   // half blue
	fill(img, image.Rect(100, 100, 200, 200), color.NRGBA{G: 180, A: 255}) // opaque green
	stored := decodeStored(t, mustNormalize(t, encodePNG(t, img)))

	requireColour(t, stored, Size/4, Size/4, white, "fully transparent, red beneath")
	requireColour(t, stored, Size*3/4, Size/4, white, "fully transparent")
	requireColour(t, stored, Size/4, Size*3/4, color.NRGBA{R: 127, G: 127, B: 255, A: 255}, "half transparent blue")
	requireColour(t, stored, Size*3/4, Size*3/4, color.NRGBA{G: 180, A: 255}, "opaque")

	// A palette with a transparent entry, and grey with alpha: other ways a
	// PNG says "transparent".
	paletted := image.NewPaletted(image.Rect(0, 0, 64, 64), color.Palette{color.NRGBA{}, red})
	requireColour(t, decodeStored(t, mustNormalize(t, encodePNG(t, paletted))), Size/2, Size/2, white, "transparent palette entry")
	grey := image.NewNRGBA64(image.Rect(0, 0, 64, 64))
	requireColour(t, decodeStored(t, mustNormalize(t, encodePNG(t, grey))), Size/2, Size/2, white, "16-bit, transparent")
}

// Every kind of pixel the two decoders can return is drawn: grey, palette,
// 16-bit, and a JPEG in grey.
func TestNormalizeHandlesEveryPixelFormat(t *testing.T) {
	grey := image.NewGray(image.Rect(0, 0, 80, 60))
	fill(grey, grey.Bounds(), color.Gray{Y: 200})
	deep := image.NewRGBA64(image.Rect(0, 0, 80, 60))
	fill(deep, deep.Bounds(), red)
	paletted := image.NewPaletted(image.Rect(0, 0, 80, 60), color.Palette{red, blue})

	lightGrey := color.NRGBA{R: 200, G: 200, B: 200, A: 255}
	for name, tc := range map[string]struct {
		raw  []byte
		want color.NRGBA
	}{
		"grey png":     {encodePNG(t, grey), lightGrey},
		"grey jpeg":    {encodeJPEG(t, grey), lightGrey},
		"16-bit png":   {encodePNG(t, deep), red},
		"paletted png": {encodePNG(t, paletted), red},
	} {
		t.Run(name, func(t *testing.T) {
			requireColour(t, decodeStored(t, mustNormalize(t, tc.raw)), Size/2, Size/2, tc.want, name)
		})
	}
}

// Nothing but pixels survives: no metadata segment of any kind is in a
// stored picture, and no byte string planted anywhere in the upload is.
func TestNormalizeDropsEverythingButThePixels(t *testing.T) {
	const marker = "MARKERSECRET-GPS-40.4168N-3.7038W"
	upright := scene(320, 240)

	gps := exifBody("MM",
		ifdEntry("MM", tagOrientation, typeShort, 1, 1),
		ifdEntry("MM", 0x8825, 4, 1, 0x0026)) // the GPS directory
	gps = append(gps, marker...)
	jpegUpload := withJPEGSegments(encodeJPEG(t, upright),
		jpegSegment(0xE0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")),
		jpegSegment(markerAPP1, gps),
		jpegSegment(markerAPP1, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>"+marker+"</x:xmpmeta>")),
		jpegSegment(0xE2, []byte("ICC_PROFILE\x00\x01\x01"+marker)),
		jpegSegment(0xED, []byte("Photoshop 3.0\x00"+marker)),
		jpegSegment(0xFE, []byte(marker)))
	jpegUpload = append(jpegUpload, marker...) // after the end of the image

	pngUpload := withPNGChunks(encodePNG(t, upright),
		pngChunk("tEXt", []byte("Comment\x00"+marker)),
		pngChunk("iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00"+marker)),
		pngChunk("eXIf", gps[6:]),
		pngChunk("tIME", []byte{0x07, 0xEA, 10, 6, 12, 0, 0}),
		pngChunk("prVt", []byte(marker)))
	pngUpload = append(pngUpload, marker...)

	for format, raw := range map[string][]byte{"jpeg": jpegUpload, "png": pngUpload} {
		t.Run(format, func(t *testing.T) {
			if !bytes.Contains(raw, []byte(marker)) {
				t.Fatal("the fixture does not hold the marker")
			}
			stored := mustNormalize(t, raw)
			requireQuadrants(t, decodeStored(t, stored), format)

			for _, planted := range []string{marker, "MARKER", "GPS", "Exif", "JFIF", "ICC_PROFILE", "xmp", "Photoshop", "Comment"} {
				if bytes.Contains(stored, []byte(planted)) {
					t.Errorf("the stored picture holds %q", planted)
				}
			}
			// Only what a JPEG needs to be decoded: tables, the frame and
			// the scan. No application segment (EXIF, ICC, XMP) and no comment.
			markers := jpegMarkers(t, stored)
			for _, m := range markers {
				if m != 0xDB && m != 0xC0 && m != 0xC4 && m != markerSOS {
					t.Errorf("the stored picture has a segment %#x; its segments are % x", m, markers)
				}
			}
			for _, forbidden := range []byte{markerAPP1, 0xE2, 0xFE} {
				if slices.Contains(markers, forbidden) {
					t.Errorf("the stored picture has a %#x segment", forbidden)
				}
			}
		})
	}

	// The upload itself is never what is stored, even when it already is a
	// Size by Size JPEG with nothing to remove.
	plain := encodeJPEG(t, scene(Size, Size))
	if stored := mustNormalize(t, plain); bytes.Equal(stored, plain) {
		t.Error("an upload was stored as it came")
	}
}

// The same upload always gives the same bytes: nothing in a stored picture
// depends on when or how often it was made.
func TestNormalizeIsDeterministic(t *testing.T) {
	for name, raw := range map[string][]byte{
		"jpeg":          encodeJPEG(t, scene(300, 200)),
		"png":           encodePNG(t, scene(200, 300)),
		"oriented jpeg": orientedJPEG(t, scene(320, 240), 6),
	} {
		t.Run(name, func(t *testing.T) {
			first := mustNormalize(t, raw)
			time.Sleep(5 * time.Millisecond)
			for range 3 {
				// A new normalizer each time, as after a restart.
				if again := mustNormalize(t, raw); !bytes.Equal(again, first) {
					t.Fatal("the same upload gave different bytes")
				}
			}
			// Well inside what the table accepts for a picture.
			if len(first) == 0 || len(first) > 512<<10 {
				t.Errorf("the stored picture is %d bytes", len(first))
			}
		})
	}
	// The input is not touched.
	raw := orientedJPEG(t, scene(320, 240), 6)
	before := bytes.Clone(raw)
	mustNormalize(t, raw)
	if !bytes.Equal(raw, before) {
		t.Error("normalize changed its input")
	}
}

// A file whose header is fine and whose pixels can't be decoded is refused
// once decoding is tried, with the same code as an unreadable header.
func TestNormalizeRefusesAnImageThatDoesNotDecode(t *testing.T) {
	noisy := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for i := range noisy.Pix {
		noisy.Pix[i] = byte(i*31 + i/7)
	}
	jpegFile, pngFile := encodeJPEG(t, noisy), encodePNG(t, noisy)
	// Where the JPEG's scan header ends and its pixel data begins.
	scan := bytes.Index(jpegFile, []byte{0xFF, markerSOS})
	scanData := scan + 2 + int(jpegFile[scan+2])<<8 + int(jpegFile[scan+3])

	for name, raw := range map[string][]byte{
		"jpeg cut in its scan":     jpegFile[:len(jpegFile)/2],
		"jpeg with no pixel data":  jpegFile[:scanData],
		"png cut in its data":      pngFile[:len(pngFile)/2],
		"png cut after its header": pngFile[:33],
		"png declaring more":       append(pngHeaderOnly(64, 64), pngFile[33:]...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := inspect(raw); err != nil {
				t.Fatalf("the fixture is refused before decoding: %v", err)
			}
			out, err := testNormalizer().normalize(context.Background(), raw)
			requireRefused(t, err, ErrInvalidImage)
			if out != nil {
				t.Error("a refused upload returned a picture")
			}
		})
	}
}

// ---- Decode slots ----

// held returns a normalizer whose slots are all taken, as by uploads that
// are being decoded and never finish.
func held(slots int, queueTimeout time.Duration) *normalizer {
	n := newNormalizer(slots, queueTimeout)
	for range slots {
		n.slots <- struct{}{}
	}
	return n
}

func TestNormalizeIsOverloadedWhenEverySlotIsHeld(t *testing.T) {
	const wait = 60 * time.Millisecond
	n := held(decodeSlots, wait)
	rendered := false
	n.render = func(format, []byte) ([]byte, error) { rendered = true; return nil, nil }

	start := time.Now()
	out, err := n.normalize(context.Background(), encodePNG(t, scene(64, 64)))
	waited := time.Since(start)
	if !errors.Is(err, ErrOverloaded) {
		t.Fatalf("err = %v, want ErrOverloaded", err)
	}
	if out != nil || rendered {
		t.Error("an overloaded upload was decoded")
	}
	if waited < wait {
		t.Errorf("gave up after %v, before the %v queue timeout", waited, wait)
	}
	if waited > 5*time.Second {
		t.Errorf("gave up only after %v", waited)
	}
	// It is not a validation error: the upload may be fine.
	var verr *ValidationError
	if errors.As(err, &verr) {
		t.Error("ErrOverloaded is a validation error")
	}
	// It took no slot: the ones held are still the only ones.
	if len(n.slots) != decodeSlots {
		t.Errorf("%d slots held, want %d", len(n.slots), decodeSlots)
	}

	// Once a slot frees up, the same upload goes through.
	<-n.slots
	n.render = render
	if _, err := n.normalize(context.Background(), encodePNG(t, scene(64, 64))); err != nil {
		t.Errorf("after a slot was freed: %v", err)
	}
}

// A request whose context ends gives up at once, without waiting out the
// queue timeout, and one whose context had ended never queues.
func TestNormalizeGivesUpWhenItsContextEnds(t *testing.T) {
	raw := encodePNG(t, scene(64, 64))
	n := held(decodeSlots, time.Hour)

	ended, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := n.normalize(ended, raw); !errors.Is(err, context.Canceled) {
		t.Errorf("ended context: err = %v, want context.Canceled", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)
	if _, err := n.normalize(ctx, raw); !errors.Is(err, context.Canceled) {
		t.Errorf("context cancelled while queued: err = %v, want context.Canceled", err)
	}

	deadline, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := n.normalize(deadline, raw); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline while queued: err = %v, want context.DeadlineExceeded", err)
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("the three gave up after %v, want promptly", waited)
	}

	// With a free slot too, an ended context does no work.
	free := testNormalizer()
	free.render = func(format, []byte) ([]byte, error) { t.Error("decoded with an ended context"); return nil, nil }
	if _, err := free.normalize(ended, raw); !errors.Is(err, context.Canceled) {
		t.Errorf("ended context, free slot: err = %v", err)
	}
}

// An upload that is refused by its bytes or its header never waits: it is
// answered while every slot is held.
func TestNormalizeRefusesBeforeQueueing(t *testing.T) {
	n := held(decodeSlots, time.Hour)
	for reason, raw := range map[*FieldError][]byte{
		ErrRequired:           nil,
		ErrTooLarge:           make([]byte, MaxUploadBytes+1),
		ErrUnsupportedType:    []byte("GIF89a"),
		ErrInvalidImage:       pngSignature,
		ErrDimensionsTooLarge: pngHeaderOnly(MaxDimension+1, 1),
	} {
		done := make(chan error, 1)
		go func() { _, err := n.normalize(context.Background(), raw); done <- err }()
		select {
		case err := <-done:
			requireRefused(t, err, reason)
		case <-time.After(5 * time.Second):
			t.Fatalf("%v waited for a slot", reason)
		}
	}
}

// Never more uploads in the decoder than there are slots, however many
// arrive; the rest wait their turn and every one is served.
func TestNormalizeDecodesAtMostSlotsAtOnce(t *testing.T) {
	const slots, uploads = 2, 12
	n := newNormalizer(slots, time.Minute)
	var running, peak atomic.Int32
	n.render = func(f format, raw []byte) ([]byte, error) {
		now := running.Add(1)
		for {
			seen := peak.Load()
			if now <= seen || peak.CompareAndSwap(seen, now) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		running.Add(-1)
		return render(f, raw)
	}

	raw := encodePNG(t, scene(64, 64))
	errs := make([]error, uploads)
	var wg sync.WaitGroup
	for i := range uploads {
		wg.Go(func() { _, errs[i] = n.normalize(context.Background(), raw) })
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("upload %d: %v", i, err)
		}
	}
	if got := peak.Load(); got != slots {
		t.Errorf("%d uploads were decoded at once, want %d", got, slots)
	}
	if len(n.slots) != 0 {
		t.Errorf("%d slots still held after every upload finished", len(n.slots))
	}
}

// A slot is given back when decoding fails, too.
func TestNormalizeReleasesItsSlotAfterAFailure(t *testing.T) {
	n := newNormalizer(1, 50*time.Millisecond)
	pngFile := encodePNG(t, scene(64, 64))
	for range 3 {
		_, err := n.normalize(context.Background(), pngFile[:len(pngFile)-30])
		requireRefused(t, err, ErrInvalidImage)
	}
	if _, err := n.normalize(context.Background(), pngFile); err != nil {
		t.Errorf("after failed uploads: %v", err)
	}
}

func TestProductionLimits(t *testing.T) {
	if MaxUploadBytes != 5*1024*1024 || MaxDimension != 4096 || Size != 512 || jpegQuality != 85 {
		t.Errorf("limits = %d bytes, %d px, %d px, quality %d", MaxUploadBytes, MaxDimension, Size, jpegQuality)
	}
	if decodeSlots != 2 || decodeQueueTimeout != 5*time.Second {
		t.Errorf("slots = %d, queue timeout %v", decodeSlots, decodeQueueTimeout)
	}
}

// FuzzNormalize: whatever the bytes, the pipeline either refuses them with a
// validation error or returns a Size by Size JPEG, and never panics.
func FuzzNormalize(f *testing.F) {
	small := scene(24, 16)
	jpegFile, pngFile := encodeJPEG(f, small), encodePNG(f, small)
	f.Add([]byte{})
	f.Add([]byte("GIF89a"))
	f.Add(jpegFile)
	f.Add(pngFile)
	f.Add(jpegFile[:len(jpegFile)/2])
	f.Add(pngFile[:len(pngFile)/2])
	for orientation := 1; orientation <= 8; orientation++ {
		f.Add(orientedJPEG(f, small, orientation))
	}
	f.Add(withPNGChunks(pngFile, pngChunk("tEXt", []byte("Comment\x00text"))))
	f.Add(pngHeaderOnly(MaxDimension+1, 1))
	f.Add(encodePNG(f, image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.NRGBA{}, red})))
	f.Add(encodeJPEG(f, image.NewGray(image.Rect(0, 0, 8, 8))))

	n := testNormalizer()
	f.Fuzz(func(t *testing.T, raw []byte) {
		// Large images are the same code doing more of the same work: keep
		// each run fast.
		if f, ok := sniff(raw); ok {
			if cfg, err := decodeConfig(f, raw); err == nil && (cfg.Width > 256 || cfg.Height > 256) {
				t.Skip()
			}
		}
		out, err := n.normalize(context.Background(), raw)
		if err != nil {
			var verr *ValidationError
			if !errors.As(err, &verr) || len(verr.Fields) != 1 || out != nil {
				t.Fatalf("err = %v with %d bytes, want a validation error alone", err, len(out))
			}
			return
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
		if err != nil || cfg.Width != Size || cfg.Height != Size {
			t.Fatalf("stored picture: %v, %d by %d", err, cfg.Width, cfg.Height)
		}
		if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
			t.Fatalf("stored picture does not decode: %v", err)
		}
	})
}
