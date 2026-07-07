// Package androidusb reads and writes USB-MIDI for devices plugged into an
// Android phone (a P-6, an Adafruit MacroPad, …) entirely from Go, and bridges
// them into the midibridge package (so the p6 + midiin backends work unchanged).
//
// Android forbids /dev/snd, so desktop rp6's ALSA path can't work; but a phone
// in USB-host mode exposes plugged-in USB-MIDI gear through android.hardware.usb
// (UsbManager). Those APIs are all *concrete* Java methods, so we can drive them
// from Go over JNI — via Fyne's public driver.RunNative — without any custom
// Java class (which the `fyne package` toolchain can't compile in). We read the
// device's bulk IN endpoint (USB-MIDI event packets, decoded to a raw MIDI
// stream) and, for a device with a bulk-OUT endpoint (a P-6), write packets
// encoded from the raw MIDI the app sends.
//
// The JNI transport lives in the android-tagged files (androidusb_android.go +
// androidusb_cb.go); this file holds the pure-Go USB-MIDI packet codec
// (DecodeUSBMIDI / EncodeUSBMIDI), which is platform-independent and
// unit-tested on the host.
package androidusb

// DecodeUSBMIDI converts a buffer of USB-MIDI event packets into a raw MIDI byte
// stream. Every USB-MIDI packet is exactly 4 bytes:
//
//	byte 0: (cable number << 4) | CIN   (Code Index Number)
//	byte 1..3: up to three MIDI data bytes
//
// The CIN says how many of the following bytes are real MIDI (see cinLength).
// Cable numbers are ignored (rp6 treats the device as a single MIDI stream).
// A trailing partial (<4 byte) fragment is ignored. The result is exactly what a
// classic serial MIDI IN would have produced, ready for p6.ParseMIDI.
func DecodeUSBMIDI(packets []byte) []byte {
	out := make([]byte, 0, len(packets)) // at most 3/4 of the input
	for i := 0; i+4 <= len(packets); i += 4 {
		cin := packets[i] & 0x0F
		n := cinLength(cin)
		if n == 0 {
			continue
		}
		out = append(out, packets[i+1:i+1+n]...)
	}
	return out
}

// cinLength returns how many of a USB-MIDI packet's three data bytes are valid
// MIDI for a given Code Index Number, per the USB Device Class Definition for
// MIDI Devices (§4, Table 4-1). 0 means "no MIDI bytes" (reserved/misc).
func cinLength(cin byte) int {
	switch cin {
	case 0x5, // single-byte SysEx end / 1-byte system-common
		0xF: // single byte (system realtime, e.g. clock/start/stop)
		return 1
	case 0x2, // 2-byte system-common
		0x6, // 2-byte SysEx end
		0xC, // program change
		0xD: // channel pressure
		return 2
	case 0x3, // 3-byte system-common
		0x4, // SysEx start / continue
		0x7, // 3-byte SysEx end
		0x8, // note off
		0x9, // note on
		0xA, // poly key press
		0xB, // control change
		0xE: // pitch bend
		return 3
	default: // 0x0, 0x1: miscellaneous / cable events (reserved) — no MIDI
		return 0
	}
}

// EncodeUSBMIDI is the inverse of DecodeUSBMIDI: it packs a raw MIDI byte stream
// (one or more complete messages, as rp6's p6.Device emits — Note On, CC,
// Program Change, and system-realtime Start/Stop/Continue/Clock) into 32-bit
// USB-MIDI event packets on cable 0, ready for a bulk-OUT endpoint.
//
// It does not implement running status or SysEx (rp6 never sends either); a
// leading data byte with no status, or an incomplete trailing message, is
// dropped.
func EncodeUSBMIDI(midi []byte) []byte {
	out := make([]byte, 0, len(midi)/3*4+4)
	for i := 0; i < len(midi); {
		status := midi[i]
		if status < 0x80 {
			i++ // stray data byte without a status — skip
			continue
		}
		cin, mlen := encodeInfo(status)
		if mlen == 0 || i+mlen > len(midi) {
			break // unsupported (e.g. SysEx) or an incomplete trailing message
		}
		pkt := [4]byte{cin}
		copy(pkt[1:], midi[i:i+mlen])
		out = append(out, pkt[:]...)
		i += mlen
	}
	return out
}

// encodeInfo returns the USB-MIDI Code Index Number and the MIDI message length
// (in bytes, including the status byte) for a status byte. mlen 0 means the
// message isn't representable here (SysEx / unsupported) and is dropped.
func encodeInfo(status byte) (cin byte, mlen int) {
	if status >= 0xF8 { // system realtime — one byte
		return 0xF, 1
	}
	switch status & 0xF0 {
	case 0xC0, 0xD0: // program change, channel pressure — 2 bytes
		return status >> 4, 2
	case 0x80, 0x90, 0xA0, 0xB0, 0xE0: // note off/on, poly-AT, CC, pitch-bend
		return status >> 4, 3
	}
	switch status { // system-common we might see (not emitted by rp6 today)
	case 0xF1, 0xF3: // MTC quarter-frame, song select — 2 bytes
		return 0x2, 2
	case 0xF2: // song position pointer — 3 bytes
		return 0x3, 3
	}
	return 0, 0 // SysEx (0xF0) and anything else: unsupported
}
