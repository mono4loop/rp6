package main

import "io"

// copyAndClose copies r into w and then closes w, returning the first error.
// A deferred Close() error is folded into the result when the copy itself
// succeeded, so a late write-flush failure (e.g. out of space) isn't silently
// dropped and mistaken for a successful copy.
func copyAndClose(w io.WriteCloser, r io.Reader) (err error) {
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(w, r)
	return err
}
