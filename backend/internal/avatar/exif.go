package avatar

import "encoding/binary"

// The EXIF orientation of an image: how its stored pixels must be turned to
// be seen upright. The values are the ones of the tag (EXIF 2.3, tag 0x0112).
const (
	orientationUpright = 1 // as stored
	orientationMax     = 8

	markerSOI  = 0xD8
	markerEOI  = 0xD9
	markerSOS  = 0xDA
	markerAPP1 = 0xE1
	markerTEM  = 0x01

	tagOrientation = 0x0112
	typeShort      = 3
	ifdEntrySize   = 12
)

var exifHeader = []byte{'E', 'x', 'i', 'f', 0, 0}

// jpegOrientation returns the EXIF orientation recorded in the JPEG raw, 1
// to 8, or orientationUpright when there is none or it can't be read.
//
// It reads one 16-bit value and nothing else, by hand, so that no EXIF
// library is needed. It only looks at the segments before the image data,
// at the first Exif APP1 segment among them and at that segment's first
// directory; it follows no pointer but the one to that directory, so nothing
// in the data can make it loop. Every offset is checked against the bytes
// that exist, and anything unexpected means "upright": a picture shown
// sideways is the worst a malformed tag can do.
func jpegOrientation(raw []byte) int {
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != markerSOI {
		return orientationUpright
	}
	for i := 2; i+1 < len(raw); {
		if raw[i] != 0xFF {
			return orientationUpright
		}
		marker := raw[i+1]
		switch {
		case marker == 0xFF: // a fill byte before the marker
			i++
			continue
		case marker == markerSOS || marker == markerEOI:
			// The image data starts, or the file ends: no EXIF before it.
			return orientationUpright
		case marker == markerSOI || marker == markerTEM || (marker >= 0xD0 && marker <= 0xD7):
			i += 2 // markers that stand alone, without a length
			continue
		}
		// Every other segment: two bytes of length, counting themselves.
		if i+4 > len(raw) {
			return orientationUpright
		}
		length := int(binary.BigEndian.Uint16(raw[i+2:]))
		if length < 2 || i+2+length > len(raw) {
			return orientationUpright
		}
		payload := raw[i+4 : i+2+length]
		if marker == markerAPP1 && len(payload) >= len(exifHeader) && string(payload[:len(exifHeader)]) == string(exifHeader) {
			return tiffOrientation(payload[len(exifHeader):])
		}
		i += 2 + length
	}
	return orientationUpright
}

// tiffOrientation returns the orientation in the first directory of the TIFF
// structure tiff (the body of an Exif segment), or orientationUpright.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return orientationUpright
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return orientationUpright
	}
	if order.Uint16(tiff[2:]) != 42 {
		return orientationUpright
	}
	// The directory's offset is unsigned and may be anything: compare it as
	// 64 bits so that no sum can wrap.
	dir := uint64(order.Uint32(tiff[4:]))
	size := uint64(len(tiff))
	if dir < 8 || dir+2 > size {
		return orientationUpright
	}
	entries := uint64(order.Uint16(tiff[dir:]))
	first := dir + 2
	for n := uint64(0); n < entries; n++ {
		at := first + n*ifdEntrySize
		if at+ifdEntrySize > size {
			return orientationUpright
		}
		entry := tiff[at : at+ifdEntrySize]
		if order.Uint16(entry) != tagOrientation {
			continue
		}
		// One SHORT, held in the entry itself. Any other shape is not an
		// orientation this reader trusts.
		if order.Uint16(entry[2:]) != typeShort || order.Uint32(entry[4:]) != 1 {
			return orientationUpright
		}
		value := int(order.Uint16(entry[8:]))
		if value < orientationUpright || value > orientationMax {
			return orientationUpright
		}
		return value
	}
	return orientationUpright
}
