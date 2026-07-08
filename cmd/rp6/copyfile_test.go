package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wcSpy is a WriteCloser that captures writes and returns a configurable
// Close error, so copyAndClose's error handling can be tested without real I/O.
type wcSpy struct {
	buf      strings.Builder
	closeErr error
	closed   bool
}

func (w *wcSpy) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *wcSpy) Close() error                { w.closed = true; return w.closeErr }

// TestCopyAndCloseSurfacesCloseError verifies a Close() failure after a
// successful copy is surfaced, not silently dropped (jaeb part 2).
func TestCopyAndCloseSurfacesCloseError(t *testing.T) {
	w := &wcSpy{closeErr: errors.New("disk full on flush")}
	err := copyAndClose(w, strings.NewReader("wavdata"))
	require.Error(t, err, "a Close error after a good copy must be surfaced")
	assert.Contains(t, err.Error(), "disk full on flush")
	assert.Equal(t, "wavdata", w.buf.String(), "data still copied")
	assert.True(t, w.closed)
}

// TestCopyAndCloseSuccess verifies the happy path returns nil and closes w.
func TestCopyAndCloseSuccess(t *testing.T) {
	w := &wcSpy{}
	err := copyAndClose(w, strings.NewReader("ok"))
	require.NoError(t, err)
	assert.Equal(t, "ok", w.buf.String())
	assert.True(t, w.closed)
}
