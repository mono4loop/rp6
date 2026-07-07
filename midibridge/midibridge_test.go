package midibridge

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePort captures bytes sent to it, standing in for the Java MidiInputPort
// wrapper.
type fakePort struct {
	sent [][]byte
	err  error
}

func (f *fakePort) Send(data []byte) error {
	if f.err != nil {
		return f.err
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	f.sent = append(f.sent, cp)
	return nil
}

// cleanup resets global state between tests (the bridge is a process singleton).
func cleanup(t *testing.T) { t.Cleanup(Reset) }

func TestEnumerationByDirection(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("1", "P-6", true, true)        // both directions
	AddDevice("2", "MacroPad", true, false)  // input only
	AddDevice("3", "Synth OUT", false, true) // output only

	assert.Equal(t, 2, OutputCount(), "P-6 + Synth OUT are output-capable")
	assert.Equal(t, 2, InputCount(), "P-6 + MacroPad are input-capable")

	// Insertion order is preserved within a direction.
	assert.Equal(t, "P-6", OutputName(0))
	assert.Equal(t, "Synth OUT", OutputName(1))
	assert.Equal(t, "1", OutputID(0))

	assert.Equal(t, "P-6", InputName(0))
	assert.Equal(t, "MacroPad", InputName(1))

	// Out-of-range indices are safe.
	assert.Equal(t, "", OutputName(9))
	assert.Equal(t, "", OutputID(-1))
}

func TestWriterForwardsToOutputPort(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	fp := &fakePort{}
	SetOutput("p6", fp)

	w := Writer("p6")
	n, err := w.Write([]byte{0x9A, 0x30, 0x64})
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	require.Len(t, fp.sent, 1)
	assert.Equal(t, []byte{0x9A, 0x30, 0x64}, fp.sent[0])
}

func TestWriterCopiesBuffer(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	fp := &fakePort{}
	SetOutput("p6", fp)

	buf := []byte{0x90, 0x40, 0x7F}
	_, err := Writer("p6").Write(buf)
	require.NoError(t, err)
	buf[1] = 0x00 // mutate after send
	assert.Equal(t, byte(0x40), fp.sent[0][1], "sent bytes must not alias the caller's buffer")
}

func TestWriterNoOutput(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)

	// No SetOutput yet.
	_, err := Writer("p6").Write([]byte{0xF8})
	assert.ErrorIs(t, err, ErrNoOutput)

	// After ClearOutput it errors again.
	SetOutput("p6", &fakePort{})
	ClearOutput("p6")
	_, err = Writer("p6").Write([]byte{0xF8})
	assert.ErrorIs(t, err, ErrNoOutput)

	// Unknown device.
	_, err = Writer("nope").Write([]byte{0xF8})
	assert.ErrorIs(t, err, ErrNoOutput)
}

func TestWriterPropagatesSendError(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	sendErr := errors.New("boom")
	SetOutput("p6", &fakePort{err: sendErr})
	_, err := Writer("p6").Write([]byte{0x90, 0x40, 0x7F})
	assert.ErrorIs(t, err, sendErr)
}

func TestReaderReceivesPushedInput(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	r := OpenReader("p6")
	require.NotNil(t, r)

	PushInput("p6", []byte{0x9A, 0x30, 0x64})

	got := make([]byte, 3)
	n, err := io.ReadFull(r, got)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []byte{0x9A, 0x30, 0x64}, got)
}

func TestReaderCloseUnblocks(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	r := OpenReader("p6")

	done := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 8))
		done <- err
	}()
	// Nothing pushed; closing must unblock the Read with io.EOF.
	r.Close()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, io.EOF)
	case <-time.After(time.Second):
		t.Fatal("Read did not unblock after Close")
	}
}

func TestRemoveDeviceUnblocksReader(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	r := OpenReader("p6")

	done := make(chan error, 1)
	go func() { _, err := r.Read(make([]byte, 8)); done <- err }()
	RemoveDevice("p6")
	select {
	case err := <-done:
		assert.ErrorIs(t, err, io.EOF)
	case <-time.After(time.Second):
		t.Fatal("Read did not unblock after RemoveDevice")
	}
	assert.Equal(t, 0, OutputCount())
	assert.Equal(t, 0, InputCount())
}

func TestOpenReaderReplacesPrevious(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	first := OpenReader("p6")
	done := make(chan error, 1)
	go func() { _, err := first.Read(make([]byte, 8)); done <- err }()

	second := OpenReader("p6") // should close first
	require.NotNil(t, second)
	select {
	case err := <-done:
		assert.ErrorIs(t, err, io.EOF, "opening a new reader closes the old one")
	case <-time.After(time.Second):
		t.Fatal("previous reader was not closed on re-open")
	}

	// Input now flows to the second reader.
	PushInput("p6", []byte{0x01})
	got := make([]byte, 1)
	_, err := io.ReadFull(second, got)
	require.NoError(t, err)
	assert.Equal(t, byte(0x01), got[0])
}

func TestPushInputToUnknownOrReaderlessIsNoop(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	// No reader opened, and unknown device — must not panic.
	PushInput("p6", []byte{0x01})
	PushInput("ghost", []byte{0x02})
}

func TestResetClearsEverything(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	r := OpenReader("p6")
	done := make(chan error, 1)
	go func() { _, err := r.Read(make([]byte, 8)); done <- err }()

	Reset()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, io.EOF)
	case <-time.After(time.Second):
		t.Fatal("Reset did not unblock open readers")
	}
	assert.Equal(t, 0, OutputCount())
	assert.Equal(t, 0, InputCount())
}

func TestReaderDropsWhenBufferFull(t *testing.T) {
	cleanup(t)
	Reset()
	AddDevice("p6", "P-6", true, true)
	r := OpenReader("p6")
	// Push more than the queue depth without draining; must not block/panic.
	for i := range inputQueue * 2 {
		PushInput("p6", []byte{byte(i)})
	}
	// The reader still yields buffered bytes.
	got := make([]byte, 1)
	n, err := r.Read(got)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}
