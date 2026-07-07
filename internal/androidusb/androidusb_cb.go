//go:build android

package androidusb

// #include <stdlib.h>
import "C"

import (
	"sync"
	"unsafe"

	"github.com/mono4loop/rp6/midibridge"
)

var (
	logMu   sync.Mutex
	loggerF func(string)
)

func setLogger(f func(string)) {
	logMu.Lock()
	loggerF = f
	logMu.Unlock()
}

func logMsg(s string) {
	logMu.Lock()
	f := loggerF
	logMu.Unlock()
	if f != nil {
		f(s)
	}
}

//export goUSBLog
func goUSBLog(msg *C.char) { logMsg(C.GoString(msg)) }

// goUSBDevice registers a discovered USB-MIDI device with the bridge so the
// p6 / midiin backends can find it. hasIn/hasOut are 1/0 (currently input only).
//
//export goUSBDevice
func goUSBDevice(id *C.char, name *C.char, hasIn C.int, hasOut C.int) {
	midibridge.AddDevice(C.GoString(id), C.GoString(name), hasIn != 0, hasOut != 0)
}

// goUSBData decodes a chunk of raw USB-MIDI packets into a MIDI byte stream and
// pushes it to the device's bridge reader.
//
//export goUSBData
func goUSBData(id *C.char, data *C.uchar, n C.int) {
	raw := C.GoBytes(unsafe.Pointer(data), n)
	midi := DecodeUSBMIDI(raw)
	if len(midi) > 0 {
		midibridge.PushInput(C.GoString(id), midi)
	}
}

//export goUSBRemove
func goUSBRemove(id *C.char) {
	s := C.GoString(id)
	midibridge.ClearOutput(s)
	midibridge.RemoveDevice(s)
}

// goUSBOutputReady registers the bridge OutputPort for a device that has a
// bulk-OUT endpoint (a P-6), so p6.Discover sees it as output-capable and
// p6.Writer routes sends through usbOutPort -> the USB OUT endpoint.
//
//export goUSBOutputReady
func goUSBOutputReady(id *C.char) {
	s := C.GoString(id)
	midibridge.SetOutput(s, usbOutPort{id: s})
}
