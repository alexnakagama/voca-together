package avatar

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestJPEGOrientationReadsTheEightValues(t *testing.T) {
	file := encodeJPEG(t, solid(16, 16, red))
	for _, byteOrder := range []string{"II", "MM"} {
		for orientation := 1; orientation <= 8; orientation++ {
			raw := withJPEGSegments(file, exifOrientation(byteOrder, orientation))
			if got := jpegOrientation(raw); got != orientation {
				t.Errorf("%s, orientation %d: read %d", byteOrder, orientation, got)
			}
		}
	}
}

func TestJPEGOrientationFindsTheTagWhereCamerasPutIt(t *testing.T) {
	file := encodeJPEG(t, solid(16, 16, red))
	jfif := jpegSegment(0xE0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"))
	comment := jpegSegment(0xFE, []byte("a comment"))
	xmp := jpegSegment(markerAPP1, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta/>"))

	// A directory as a camera writes it: the orientation among other tags,
	// some of them with values of other types.
	camera := func(byteOrder string) []byte {
		return jpegSegment(markerAPP1, exifBody(byteOrder,
			ifdEntry(byteOrder, 0x010F, 2, 6, 0x0026), // Make, a string elsewhere
			ifdEntry(byteOrder, 0x0110, 2, 8, 0x002C), // Model
			ifdEntry(byteOrder, tagOrientation, typeShort, 1, 6),
			ifdEntry(byteOrder, 0x011A, 5, 1, 0x0034), // XResolution, a rational
			ifdEntry(byteOrder, 0x8769, 4, 1, 0x0044), // the Exif sub-directory
		))
	}

	for name, raw := range map[string][]byte{
		"after JFIF":                     withJPEGSegments(file, jfif, exifOrientation("MM", 6)),
		"after a comment":                withJPEGSegments(file, comment, exifOrientation("II", 6)),
		"after XMP in another APP1":      withJPEGSegments(file, xmp, exifOrientation("MM", 6)),
		"among other tags, big-endian":   withJPEGSegments(file, camera("MM")),
		"among other tags, little":       withJPEGSegments(file, jfif, camera("II")),
		"after fill bytes":               withJPEGSegments(file, []byte{0xFF, 0xFF, 0xFF}, exifOrientation("MM", 6)),
		"the first Exif segment decides": withJPEGSegments(file, exifOrientation("MM", 6), exifOrientation("MM", 3)),
		"with bytes after the directory": withJPEGSegments(file, jpegSegment(markerAPP1,
			append(exifBody("II", ifdEntry("II", tagOrientation, typeShort, 1, 6)), "MARKER thumbnail data"...))),
	} {
		t.Run(name, func(t *testing.T) {
			if got := jpegOrientation(raw); got != 6 {
				t.Errorf("read %d, want 6", got)
			}
		})
	}
}

// A directory whose "next directory" pointer points back at itself, and an
// Exif structure that points at its own header: the reader follows neither.
func TestJPEGOrientationFollowsNoPointerInACircle(t *testing.T) {
	file := encodeJPEG(t, solid(16, 16, red))

	selfNext := exifBody("MM", ifdEntry("MM", tagOrientation, typeShort, 1, 8))
	binary.BigEndian.PutUint32(selfNext[len(selfNext)-4:], 8) // next directory: this one
	if got := jpegOrientation(withJPEGSegments(file, jpegSegment(markerAPP1, selfNext))); got != 8 {
		t.Errorf("a directory that names itself as the next: read %d, want 8", got)
	}

	// A sub-directory entry that points at the directory holding it, before
	// the orientation: it is an entry like any other, never entered.
	loop := exifBody("MM", ifdEntry("MM", 0x8769, 4, 1, 0), ifdEntry("MM", tagOrientation, typeShort, 1, 3))
	if got := jpegOrientation(withJPEGSegments(file, jpegSegment(markerAPP1, loop))); got != 3 {
		t.Errorf("a sub-directory pointing at its parent: read %d, want 3", got)
	}

	for name, offset := range map[string]uint32{
		"the header itself": 0, "inside the header": 4, "its own offset field": 4, "the last byte": 0, "past the end": 4096,
		"far past the end": 0xFFFFFFFF, "almost the end": 0xFFFFFFFE,
	} {
		body := exifBody("MM", ifdEntry("MM", tagOrientation, typeShort, 1, 6))
		binary.BigEndian.PutUint32(body[len(exifHeader)+4:], offset)
		if name == "the last byte" {
			binary.BigEndian.PutUint32(body[len(exifHeader)+4:], uint32(len(body)-len(exifHeader)-1))
		}
		if got := jpegOrientation(withJPEGSegments(file, jpegSegment(markerAPP1, body))); got != orientationUpright {
			t.Errorf("directory offset at %s: read %d, want upright", name, got)
		}
	}
}

func TestJPEGOrientationIsUprightWhenThereIsNothingToTrust(t *testing.T) {
	file := encodeJPEG(t, solid(16, 16, red))
	entry := func(typ uint16, count uint32, value uint16) []byte {
		return withJPEGSegments(file, jpegSegment(markerAPP1, exifBody("MM", ifdEntry("MM", tagOrientation, typ, count, value))))
	}
	body := func(edit func(b []byte) []byte) []byte {
		return withJPEGSegments(file, jpegSegment(markerAPP1, edit(exifBody("MM", ifdEntry("MM", tagOrientation, typeShort, 1, 6)))))
	}
	// A segment whose length says more than the file holds.
	overlong := withJPEGSegments(file, exifOrientation("MM", 6))
	binary.BigEndian.PutUint16(overlong[4:], 0xFFFF)
	// The tag sits after the image data began: too late to be metadata.
	afterScan := append(append([]byte{}, file[:len(file)-2]...), exifOrientation("MM", 6)...)
	afterScan = append(afterScan, 0xFF, markerEOI)

	for name, raw := range map[string][]byte{
		"no exif":                  file,
		"empty":                    {},
		"one byte":                 {0xFF},
		"not a jpeg":               []byte("not a jpeg at all"),
		"a png":                    encodePNG(t, solid(16, 16, red)),
		"only the start marker":    {0xFF, markerSOI},
		"value 0":                  entry(typeShort, 1, 0),
		"value 9":                  entry(typeShort, 1, 9),
		"value 65535":              entry(typeShort, 1, 0xFFFF),
		"a LONG, not a SHORT":      entry(4, 1, 6),
		"a BYTE":                   entry(1, 1, 6),
		"two values":               entry(typeShort, 2, 6),
		"no values":                entry(typeShort, 0, 6),
		"another tag":              withJPEGSegments(file, jpegSegment(markerAPP1, exifBody("MM", ifdEntry("MM", 0x0113, typeShort, 1, 6)))),
		"an empty directory":       withJPEGSegments(file, jpegSegment(markerAPP1, exifBody("MM"))),
		"an empty exif segment":    withJPEGSegments(file, jpegSegment(markerAPP1, []byte("Exif\x00\x00"))),
		"an empty APP1":            withJPEGSegments(file, jpegSegment(markerAPP1, nil)),
		"exif without its zeros":   withJPEGSegments(file, jpegSegment(markerAPP1, []byte("ExifMM\x00\x2a\x00\x00\x00\x08"))),
		"exif in APP2":             withJPEGSegments(file, jpegSegment(0xE2, exifBody("MM", ifdEntry("MM", tagOrientation, typeShort, 1, 6)))),
		"an unknown byte order":    body(func(b []byte) []byte { b[6], b[7] = 'X', 'X'; return b }),
		"mixed byte order marks":   body(func(b []byte) []byte { b[6], b[7] = 'I', 'M'; return b }),
		"a wrong magic number":     body(func(b []byte) []byte { b[9] = 43; return b }),
		"the wrong byte order":     body(func(b []byte) []byte { b[6], b[7] = 'I', 'I'; return b }),
		"more entries than bytes":  body(func(b []byte) []byte { b[14], b[15] = 0xFF, 0xFF; b[16] = 0; return b }),
		"cut inside the entry":     body(func(b []byte) []byte { return b[:len(b)-8] }),
		"cut inside the header":    body(func(b []byte) []byte { return b[:10] }),
		"a segment length of zero": withJPEGSegments(file, []byte{0xFF, markerAPP1, 0, 0}, exifOrientation("MM", 6)),
		"a segment length of one":  withJPEGSegments(file, []byte{0xFF, markerAPP1, 0, 1}, exifOrientation("MM", 6)),
		"a length past the end":    overlong,
		"junk between segments":    withJPEGSegments(file, []byte("junk"), exifOrientation("MM", 6)),
		"exif after the scan":      afterScan,
		"a marker with no length":  {0xFF, markerSOI, 0xFF, markerAPP1},
		"a marker and one byte":    {0xFF, markerSOI, 0xFF, markerAPP1, 0x00},
		"only fill bytes":          append([]byte{0xFF, markerSOI}, bytes.Repeat([]byte{0xFF}, 4096)...),
		"restart markers only":     append([]byte{0xFF, markerSOI}, bytes.Repeat([]byte{0xFF, 0xD0}, 2048)...),
	} {
		t.Run(name, func(t *testing.T) {
			if got := jpegOrientation(raw); got != orientationUpright {
				t.Errorf("read %d, want upright", got)
			}
		})
	}
}

// Cut anywhere, a file with a tag never makes the reader panic or answer
// anything but an orientation; cut after the tag, it still reads it.
func TestJPEGOrientationOfEveryTruncation(t *testing.T) {
	for _, byteOrder := range []string{"II", "MM"} {
		segment := exifOrientation(byteOrder, 7)
		raw := withJPEGSegments(encodeJPEG(t, solid(16, 16, red)),
			jpegSegment(0xE0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")), segment)
		whole := bytes.Index(raw, segment) + len(segment)
		for cut := 0; cut <= len(raw); cut++ {
			got := jpegOrientation(raw[:cut])
			want := orientationUpright
			if cut >= whole {
				want = 7
			}
			if got != want {
				t.Fatalf("%s, cut at %d of %d: read %d, want %d", byteOrder, cut, len(raw), got, want)
			}
		}
		// Every single byte of the segment damaged in turn: still an orientation.
		start := bytes.Index(raw, segment)
		for i := start; i < whole; i++ {
			for _, b := range []byte{0x00, 0xFF, raw[i] ^ 0x80} {
				damaged := append([]byte{}, raw...)
				damaged[i] = b
				if got := jpegOrientation(damaged); got < orientationUpright || got > orientationMax {
					t.Fatalf("%s, byte %d set to %#x: read %d", byteOrder, i, b, got)
				}
			}
		}
	}
}

// FuzzJPEGOrientation: whatever the bytes, the reader returns one of the
// eight orientations and never panics or hangs.
func FuzzJPEGOrientation(f *testing.F) {
	file := encodeJPEG(f, solid(8, 8, red))
	f.Add([]byte{})
	f.Add(file)
	for _, byteOrder := range []string{"II", "MM"} {
		for orientation := 1; orientation <= 8; orientation++ {
			f.Add(withJPEGSegments(file, exifOrientation(byteOrder, orientation)))
			f.Add(withJPEGSegments([]byte{0xFF, markerSOI}, exifOrientation(byteOrder, orientation)))
		}
	}
	f.Add(withJPEGSegments(file, jpegSegment(0xE0, []byte("JFIF\x00")), exifOrientation("MM", 6)))
	f.Add([]byte{0xFF, markerSOI, 0xFF, markerAPP1, 0xFF, 0xFF})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if got := jpegOrientation(raw); got < orientationUpright || got > orientationMax {
			t.Errorf("read %d, want 1 to 8", got)
		}
	})
}
