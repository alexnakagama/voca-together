// Package avatar owns members' profile pictures (decision 031): what an
// upload must be to be accepted, and how it becomes the picture that is
// stored. It knows nothing about HTTP or sessions and imports none of auth,
// server, profile and language.
//
// Nothing a member uploads is ever kept: the stored picture is produced only
// from the decoded pixels, so no metadata, hidden data or second file format
// of the upload can survive in it.
package avatar

import "time"

const (
	// MaxUploadBytes is the largest upload that is looked at.
	MaxUploadBytes = 5 << 20
	// MaxDimension is the largest width or height, in pixels, an upload may
	// declare. It is checked before any pixel buffer is allocated.
	MaxDimension = 4096
	// Size is the width and the height, in pixels, of every stored picture.
	Size = 512

	// jpegQuality is the quality every stored picture is encoded at. It is
	// part of what makes the same upload always give the same bytes.
	jpegQuality = 85

	// decodeSlots is how many uploads are decoded at once, and
	// decodeQueueTimeout how long a request waits for its turn. A 4096x4096
	// image takes tens of megabytes while it is processed, so the cap makes
	// the worst case a constant (see normalizer.withSlot).
	decodeSlots        = 2
	decodeQueueTimeout = 5 * time.Second
)
