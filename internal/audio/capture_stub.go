//go:build !capture

package audio

// OpenCapture is a no-op without the "capture" build tag; it reports that no
// capture backend is available so callers fall back gracefully.
func OpenCapture(nameMatch string) (Capturer, error) {
	return nil, ErrUnavailable
}
