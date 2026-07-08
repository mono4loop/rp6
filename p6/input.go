package p6

import (
	"bufio"
	"errors"
	"io"
)

// EventType classifies a parsed incoming MIDI message.
type EventType int

const (
	EventOther EventType = iota
	EventNoteOn
	EventNoteOff
	EventControlChange
	EventProgramChange
	EventRealTime
)

// Event is a parsed incoming MIDI message. Channel is 1-based (0 for realtime);
// Data1/Data2 are the message data bytes (Data2 unused for 1-byte messages).
type Event struct {
	Type    EventType
	Channel int
	Data1   uint8
	Data2   uint8
	Status  byte // raw status byte (e.g. realtime type)
}

// ErrNoInput is returned by Listen when the device was not opened for reading.
var ErrNoInput = errors.New("p6: device not open for reading")

// errResync signals that a status byte appeared where a data byte was expected
// (a truncated/corrupt message). The offending byte is unread so the top-level
// loop reprocesses it as a new status. It never escapes parseMIDI.
var errResync = errors.New("p6: resync on unexpected status byte")

// Listen reads MIDI from the device and calls handler for each parsed message,
// blocking until the device is closed (then returning the read error, typically
// due to Close). Run it in a goroutine. handler is called on that goroutine, so
// UI code must marshal onto its own thread.
func (d *Device) Listen(handler func(Event)) error {
	if d.r == nil {
		return ErrNoInput
	}
	return parseMIDI(bufio.NewReader(d.r), handler)
}

// ParseMIDI reads MIDI from r and calls handler for each parsed message,
// blocking until r returns an error (which it then returns — typically io.EOF
// or a read error when the underlying node is closed). It is the same streaming
// parser Device.Listen uses (running status, interleaved realtime, etc.),
// exported so other MIDI *input* devices — e.g. external controllers under
// internal/midiin — can reuse it instead of reimplementing MIDI parsing.
func ParseMIDI(r io.Reader, handler func(Event)) error {
	return parseMIDI(bufio.NewReader(r), handler)
}

// parseMIDI is a streaming MIDI parser (with running status) that forwards
// channel-voice, program-change and system-realtime messages to handler.
func parseMIDI(br *bufio.Reader, handler func(Event)) error {
	var running byte // last channel status byte, or 0
	for {
		b, err := br.ReadByte()
		if err != nil {
			return err
		}
		switch err := processByte(br, b, &running, handler); {
		case err == nil:
			// handled
		case errors.Is(err, errResync):
			running = 0 // truncated message; the offending byte was unread
		default:
			return err
		}
	}
}

// processByte handles one leading byte of the stream, reading any further data
// bytes it implies. It updates running status via running. It returns errResync
// when a message was truncated by an unexpected status byte (already unread), so
// the caller can resynchronize on the next iteration.
func processByte(br *bufio.Reader, b byte, running *byte, handler func(Event)) error {
	switch {
	case b >= 0xF8: // system realtime — may interleave anywhere
		handler(Event{Type: EventRealTime, Status: b})
	case b == 0xF0: // sysex — skip to end (0xF7)
		if err := skipSysex(br, handler); err != nil {
			return err
		}
		*running = 0
	case b >= 0xF1 && b <= 0xF7: // system common — consume its data bytes
		if err := skipSystemCommon(br, b, handler); err != nil {
			return err
		}
		*running = 0
	case b >= 0x80: // channel status byte
		*running = b
		return dispatch(br, *running, nil, handler)
	default: // data byte -> running status
		if *running == 0 {
			return nil // stray data with no status; resync
		}
		first := b
		return dispatch(br, *running, &first, handler)
	}
	return nil
}

// dispatch reads the data bytes for a channel message with the given status and
// emits an Event. If first is non-nil it is used as the first data byte (running
// status); otherwise all data bytes are read from br.
func dispatch(br *bufio.Reader, status byte, first *byte, handler func(Event)) error {
	statusHi := status & 0xF0

	// Read the message's data bytes (interleaved realtime handled by readData).
	var data [2]byte
	for i := 0; i < dataBytesFor(statusHi); i++ {
		if first != nil {
			data[i] = *first
			first = nil
			continue
		}
		b, err := readData(br, handler)
		if err != nil {
			return err
		}
		data[i] = b
	}

	ch := int(status&0x0F) + 1
	d1, d2 := data[0]&0x7F, data[1]&0x7F
	switch statusHi {
	case statusNoteOn: // note on (velocity 0 == note off)
		t := EventNoteOn
		if d2 == 0 {
			t = EventNoteOff
		}
		handler(Event{Type: t, Channel: ch, Data1: d1, Data2: d2, Status: status})
	case statusNoteOff:
		handler(Event{Type: EventNoteOff, Channel: ch, Data1: d1, Data2: d2, Status: status})
	case statusControlChange:
		handler(Event{Type: EventControlChange, Channel: ch, Data1: d1, Data2: d2, Status: status})
	case statusProgramChange:
		handler(Event{Type: EventProgramChange, Channel: ch, Data1: d1, Status: status})
		// Poly aftertouch (0xA0), channel aftertouch (0xD0) and pitch bend
		// (0xE0) have their data bytes consumed above but emit no Event.
	}
	return nil
}

// dataBytesFor returns how many data bytes a channel-voice message with the
// given status nibble carries: program change and channel aftertouch carry one,
// every other channel message carries two.
func dataBytesFor(statusHi byte) int {
	switch statusHi {
	case statusProgramChange, statusChannelAftertouch:
		return 1
	default:
		return 2
	}
}

func skipSysex(br *bufio.Reader, handler func(Event)) error {
	for {
		b, err := br.ReadByte()
		if err != nil {
			return err
		}
		if b == 0xF7 {
			return nil
		}
		if b >= 0xF8 { // realtime can interleave inside sysex
			handler(Event{Type: EventRealTime, Status: b})
		}
	}
}

func skipSystemCommon(br *bufio.Reader, status byte, handler func(Event)) error {
	n := 0
	switch status {
	case 0xF1, 0xF3: // MTC quarter frame, song select
		n = 1
	case 0xF2: // song position pointer
		n = 2
	}
	for i := 0; i < n; i++ {
		if _, err := readData(br, handler); err != nil {
			return err
		}
	}
	return nil
}

// readData reads the next MIDI data byte. System-realtime bytes (0xF8..0xFF) may
// interleave anywhere, so they are emitted and skipped transparently. If a
// status byte (>=0x80) appears where a data byte was expected — a truncated or
// corrupt message — the byte is unread and errResync is returned so the caller
// aborts the current message and the top-level loop resynchronizes on it.
func readData(br *bufio.Reader, handler func(Event)) (byte, error) {
	for {
		bb, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		if bb >= 0xF8 { // system realtime — may interleave anywhere
			handler(Event{Type: EventRealTime, Status: bb})
			continue
		}
		if bb >= 0x80 { // unexpected status byte — truncated message, resync
			_ = br.UnreadByte()
			return 0, errResync
		}
		return bb, nil
	}
}
