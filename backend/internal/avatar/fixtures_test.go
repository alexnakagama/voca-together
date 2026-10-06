package avatar

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"
)

// The fixtures are generated: no image file is checked in.

var (
	red    = color.NRGBA{R: 230, G: 20, B: 20, A: 255}
	green  = color.NRGBA{R: 20, G: 180, B: 20, A: 255}
	blue   = color.NRGBA{R: 20, G: 20, B: 230, A: 255}
	yellow = color.NRGBA{R: 240, G: 220, B: 20, A: 255}
	white  = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	black  = color.NRGBA{A: 255}
)

func fill(img draw.Image, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

func solid(w, h int, c color.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill(img, img.Bounds(), c)
	return img
}

// scene is an upright w by h picture whose centre square has four coloured
// quadrants (red, green over blue, yellow) and whose margins, the part a
// centre crop drops, are black.
func scene(w, h int) *image.NRGBA {
	img := solid(w, h, black)
	side := min(w, h)
	x0, y0 := (w-side)/2, (h-side)/2
	half := side / 2
	fill(img, image.Rect(x0, y0, x0+half, y0+half), red)
	fill(img, image.Rect(x0+half, y0, x0+side, y0+half), green)
	fill(img, image.Rect(x0, y0+half, x0+half, y0+side), blue)
	fill(img, image.Rect(x0+half, y0+half, x0+side, y0+side), yellow)
	return img
}

// storedAs returns upright as a camera would store it with the given EXIF
// orientation: the pixels that, once that orientation is applied, show
// upright again. Orientations 5 to 8 swap the width and the height.
func storedAs(upright *image.NRGBA, orientation int) *image.NRGBA {
	w, h := upright.Bounds().Dx(), upright.Bounds().Dy()
	sw, sh := w, h
	if orientation >= 5 {
		sw, sh = h, w
	}
	stored := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	for sy := range sh {
		for sx := range sw {
			var x, y int
			switch orientation {
			case 1:
				x, y = sx, sy
			case 2:
				x, y = w-1-sx, sy
			case 3:
				x, y = w-1-sx, h-1-sy
			case 4:
				x, y = sx, h-1-sy
			case 5:
				x, y = sy, sx
			case 6:
				x, y = w-1-sy, sx
			case 7:
				x, y = w-1-sy, h-1-sx
			case 8:
				x, y = sy, h-1-sx
			}
			stored.SetNRGBA(sx, sy, upright.NRGBAAt(x, y))
		}
	}
	return stored
}

func encodeJPEG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodePNG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// jpegSegment is one JPEG segment: the marker, its length and the payload.
func jpegSegment(marker byte, payload []byte) []byte {
	seg := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	return append(seg, payload...)
}

// withJPEGSegments returns the JPEG file with segments inserted right after
// its start-of-image marker, where cameras put their metadata.
func withJPEGSegments(file []byte, segments ...[]byte) []byte {
	out := append([]byte{}, file[:2]...)
	for _, s := range segments {
		out = append(out, s...)
	}
	return append(out, file[2:]...)
}

// exifBody is the payload of an Exif APP1 segment whose first directory
// holds entries, each 12 bytes, in the byte order "II" or "MM".
func exifBody(byteOrder string, entries ...[]byte) []byte {
	order := tiffOrder(byteOrder)
	body := append([]byte("Exif\x00\x00"), byteOrder...)
	body = order.AppendUint16(body, 42)
	body = order.AppendUint32(body, 8) // the directory follows the header
	body = order.AppendUint16(body, uint16(len(entries)))
	for _, e := range entries {
		body = append(body, e...)
	}
	return order.AppendUint32(body, 0) // no next directory
}

func tiffOrder(byteOrder string) binary.AppendByteOrder {
	if byteOrder == "MM" {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// ifdEntry is one directory entry holding a single value in the entry.
func ifdEntry(byteOrder string, tag, typ uint16, count uint32, value uint16) []byte {
	order := tiffOrder(byteOrder)
	e := order.AppendUint16(nil, tag)
	e = order.AppendUint16(e, typ)
	e = order.AppendUint32(e, count)
	e = order.AppendUint16(e, value)
	return append(e, 0, 0)
}

// exifOrientation is an Exif APP1 segment that records orientation.
func exifOrientation(byteOrder string, orientation int) []byte {
	return jpegSegment(markerAPP1, exifBody(byteOrder, ifdEntry(byteOrder, tagOrientation, typeShort, 1, uint16(orientation))))
}

// orientedJPEG is upright stored with orientation, tag included.
func orientedJPEG(t testing.TB, upright *image.NRGBA, orientation int) []byte {
	t.Helper()
	return withJPEGSegments(encodeJPEG(t, storedAs(upright, orientation)), exifOrientation("MM", orientation))
}

// withJPEGDimensions returns the JPEG file with the width and height in its
// frame header replaced, leaving everything else: a small file that declares
// an image of another size.
func withJPEGDimensions(t testing.TB, file []byte, w, h int) []byte {
	t.Helper()
	out := append([]byte{}, file...)
	for i := 2; i+9 < len(out); {
		marker := out[i+1]
		length := int(binary.BigEndian.Uint16(out[i+2:]))
		if marker == 0xC0 || marker == 0xC2 {
			binary.BigEndian.PutUint16(out[i+5:], uint16(h))
			binary.BigEndian.PutUint16(out[i+7:], uint16(w))
			return out
		}
		i += 2 + length
	}
	t.Fatal("no frame header in the JPEG")
	return nil
}

func pngChunk(typ string, data []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	chunk = append(chunk, typ...)
	chunk = append(chunk, data...)
	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
}

// pngHeaderOnly is a PNG that declares a w by h image and holds no pixels:
// 33 bytes, whatever the size it declares.
func pngHeaderOnly(w, h int) []byte {
	ihdr := binary.BigEndian.AppendUint32(nil, uint32(w))
	ihdr = binary.BigEndian.AppendUint32(ihdr, uint32(h))
	ihdr = append(ihdr, 8, 2, 0, 0, 0) // 8 bits, truecolour
	return append(append([]byte{}, pngSignature...), pngChunk("IHDR", ihdr)...)
}

// withPNGChunks returns the PNG file with chunks inserted after its header.
func withPNGChunks(file []byte, chunks ...[]byte) []byte {
	const afterHeader = 8 + 25 // the signature and the IHDR chunk
	out := append([]byte{}, file[:afterHeader]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, file[afterHeader:]...)
}

// decodeStored decodes a stored picture and checks it is what every stored
// picture must be: a JPEG of Size by Size, and nothing after it.
func decodeStored(t testing.TB, stored []byte) image.Image {
	t.Helper()
	if !bytes.HasPrefix(stored, jpegSignature) {
		t.Fatalf("the stored picture is not a JPEG: % x", stored[:min(len(stored), 8)])
	}
	if !bytes.HasSuffix(stored, []byte{0xFF, markerEOI}) {
		t.Error("the stored picture does not end at its end-of-image marker")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(stored))
	if err != nil {
		t.Fatalf("stored picture: %v", err)
	}
	if cfg.Width != Size || cfg.Height != Size {
		t.Fatalf("stored picture is %d by %d, want %d by %d", cfg.Width, cfg.Height, Size, Size)
	}
	img, err := jpeg.Decode(bytes.NewReader(stored))
	if err != nil {
		t.Fatalf("stored picture: %v", err)
	}
	return img
}

// requireColour checks the pixel at (x, y), with the tolerance a lossy
// format needs.
func requireColour(t testing.TB, img image.Image, x, y int, want color.NRGBA, what string) {
	t.Helper()
	r, g, b, _ := img.At(x, y).RGBA()
	got := [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
	for i, w := range [3]int{int(want.R), int(want.G), int(want.B)} {
		if d := got[i] - w; d < -40 || d > 40 {
			t.Errorf("%s: pixel (%d, %d) = %v, want about %v", what, x, y, got, want)
			return
		}
	}
}

// requireQuadrants checks that img shows the four quadrants of scene, upright.
func requireQuadrants(t testing.TB, img image.Image, what string) {
	t.Helper()
	const near, far = Size / 4, Size * 3 / 4
	requireColour(t, img, near, near, red, what+": top left")
	requireColour(t, img, far, near, green, what+": top right")
	requireColour(t, img, near, far, blue, what+": bottom left")
	requireColour(t, img, far, far, yellow, what+": bottom right")
	// Nothing of the margins made it in: the edges are the quadrants' too.
	requireColour(t, img, 3, 3, red, what+": top left corner")
	requireColour(t, img, Size-4, Size-4, yellow, what+": bottom right corner")
	requireColour(t, img, Size-4, 3, green, what+": top right corner")
	requireColour(t, img, 3, Size-4, blue, what+": bottom left corner")
}

// jpegMarkers lists the markers of the segments before the image data.
func jpegMarkers(t testing.TB, file []byte) []byte {
	t.Helper()
	var markers []byte
	for i := 2; i+3 < len(file); {
		if file[i] != 0xFF {
			t.Fatalf("no marker at byte %d", i)
		}
		marker := file[i+1]
		markers = append(markers, marker)
		if marker == markerSOS {
			return markers
		}
		i += 2 + int(binary.BigEndian.Uint16(file[i+2:]))
	}
	t.Fatal("no start of scan in the JPEG")
	return nil
}

func testNormalizer() *normalizer { return newNormalizer(decodeSlots, decodeQueueTimeout) }
