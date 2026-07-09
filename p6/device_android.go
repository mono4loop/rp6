//go:build android

// Android MIDI backend for the P-6. It implements the same Discover/Open/OpenPath
// surface as the ALSA backend (device_alsa.go) and the Web MIDI backend
// (device_js.go), but talks to the P-6 through the midibridge package rather than
// a rawmidi node or the browser: on Android, USB MIDI is owned by the Java layer
// (android.media.midi.MidiManager), which reports devices and shuttles bytes via
// midibridge. This *Device reuses device.go's message builders and input.go's
// parser — only the byte transport is Android-specific (a midibridge.Writer for
// output and a midibridge.InputReader for input).
//
// Until the Java layer registers a P-6 with the bridge, Discover returns
// ErrNotFound and the app stays on the built-in emulator — exactly like the
// desktop/web backends when no hardware is present.
package p6

import "github.com/mono4loop/rp6/midibridge"

// pathPrefix identifies Android-MIDI device paths.
const pathPrefix = "androidmidi:"

// Discover reports a connected P-6 as seen by the Android MIDI layer.
func Discover() (string, error) {
	if _, name, ok := findBridgeOutput(isP6Name); ok {
		return pathPrefix + name, nil
	}
	return "", ErrNotFound
}

// Open connects to a P-6 over Android MIDI using the default configuration.
func Open() (*Device, error) { return OpenPath("", DefaultConfig()) }

// OpenPath connects to the P-6's Android MIDI output (and its input, if present,
// for Listen). The path is informational; the P-6 is located by port name.
func OpenPath(_ string, cfg Config) (*Device, error) {
	outID, outName, ok := findBridgeOutput(isP6Name)
	if !ok {
		return nil, ErrNotFound
	}

	d := New(midibridge.Writer(outID), cfg)
	d.path = pathPrefix + outName

	closer := &androidCloser{}

	// Wire the P-6's MIDI input (if any) so Listen reflects hardware pad presses.
	if inID, _, ok := findBridgeInput(isP6Name); ok {
		rd := midibridge.OpenReader(inID)
		d.r = rd
		closer.reader = rd
	}

	d.c = closer
	return d, nil
}

// findBridgeOutput returns the first bridge output device whose name matches.
func findBridgeOutput(match func(string) bool) (id, name string, ok bool) {
	for i := 0; i < midibridge.OutputCount(); i++ {
		if n := midibridge.OutputName(i); match(n) {
			return midibridge.OutputID(i), n, true
		}
	}
	return "", "", false
}

// findBridgeInput returns the first bridge input device whose name matches.
func findBridgeInput(match func(string) bool) (id, name string, ok bool) {
	for i := 0; i < midibridge.InputCount(); i++ {
		if n := midibridge.InputName(i); match(n) {
			return midibridge.InputID(i), n, true
		}
	}
	return "", "", false
}

// androidCloser closes this Device's input reader, unblocking Listen. The
// physical USB transport owns the output sender and removes it only when the
// hardware disconnects; clearing it here would break a later OpenPath while the
// same P-6 remains attached.
type androidCloser struct {
	reader *midibridge.InputReader
}

func (c *androidCloser) Close() error {
	if c.reader != nil {
		c.reader.Close()
	}
	return nil
}
