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
		switch {
		case b >= 0xF8: // system realtime — may interleave anywhere
			handler(Event{Type: EventRealTime, Status: b})
		case b == 0xF0: // sysex — skip to end (0xF7)
			if err := skipSysex(br, handler); err != nil {
				return err
			}
			running = 0
		case b >= 0xF1 && b <= 0xF7: // system common — consume its data bytes
			if err := skipSystemCommon(br, b); err != nil {
				return err
			}
			running = 0
		case b >= 0x80: // channel status byte
			running = b
			if err := dispatch(br, running, nil, handler); err != nil {
				return err
			}
		default: // data byte -> running status
			if running == 0 {
				continue // stray data with no status; resync
			}
			first := b
			if err := dispatch(br, running, &first, handler); err != nil {
				return err
			}
		}
	}
}

// dispatch reads the data bytes for a channel message with the given status and
// emits an Event. If first is non-nil it is used as the first data byte (running
// status); otherwise both are read from br.
func dispatch(br *bufio.Reader, status byte, first *byte, handler func(Event)) error {
	read := func() (byte, error) {
		if first != nil {
			v := *first
			first = nil
			return v, nil
		}
		// System realtime messages may interleave between data bytes; emit and
		// skip them so they don't corrupt the message.
		for {
			bb, err := br.ReadByte()
			if err != nil {
				return 0, err
			}
			if bb >= 0xF8 {
				handler(Event{Type: EventRealTime, Status: bb})
				continue
			}
			return bb, nil
		}
	}
	ch := int(status&0x0F) + 1
	switch status & 0xF0 {
	case 0x90: // note on (velocity 0 == note off)
		d1, err := read()
		if err != nil {
			return err
		}
		d2, err := read()
		if err != nil {
			return err
		}
		t := EventNoteOn
		if d2 == 0 {
			t = EventNoteOff
		}
		handler(Event{Type: t, Channel: ch, Data1: d1 & 0x7F, Data2: d2 & 0x7F, Status: status})
	case 0x80: // note off
		d1, err := read()
		if err != nil {
			return err
		}
		d2, err := read()
		if err != nil {
			return err
		}
		handler(Event{Type: EventNoteOff, Channel: ch, Data1: d1 & 0x7F, Data2: d2 & 0x7F, Status: status})
	case 0xB0: // control change
		d1, err := read()
		if err != nil {
			return err
		}
		d2, err := read()
		if err != nil {
			return err
		}
		handler(Event{Type: EventControlChange, Channel: ch, Data1: d1 & 0x7F, Data2: d2 & 0x7F, Status: status})
	case 0xC0: // program change (1 data byte)
		d1, err := read()
		if err != nil {
			return err
		}
		handler(Event{Type: EventProgramChange, Channel: ch, Data1: d1 & 0x7F, Status: status})
	case 0xA0, 0xE0: // poly aftertouch, pitch bend (2 data bytes, ignored)
		if _, err := read(); err != nil {
			return err
		}
		if _, err := read(); err != nil {
			return err
		}
	case 0xD0: // channel aftertouch (1 data byte, ignored)
		if _, err := read(); err != nil {
			return err
		}
	}
	return nil
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

func skipSystemCommon(br *bufio.Reader, status byte) error {
	n := 0
	switch status {
	case 0xF1, 0xF3: // MTC quarter frame, song select
		n = 1
	case 0xF2: // song position pointer
		n = 2
	}
	for i := 0; i < n; i++ {
		if _, err := br.ReadByte(); err != nil {
			return err
		}
	}
	return nil
}
