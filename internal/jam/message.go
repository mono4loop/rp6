package jam

// Wire format — a compact, fixed, forward-compatible framing keyed off a magic
// byte and a message-kind byte, so unknown kinds from a newer peer are ignored
// rather than misparsed:
//
//	[ 'J', kind, payload... ]
//
// kindPad payload: pad(1) velocity(1)  -> 4 bytes total.
//
// Pad hits are tiny and idempotent, so they ride an unreliable/unordered
// WebRTC data channel: a late or dropped hit is simply discarded (a stale drum
// hit is worse than a missing one).
const wireMagic = 'J'

type kind byte

const kindPad kind = 1

type message struct {
	kind     kind
	pad      uint8
	velocity uint8
}

func encodePad(pad, velocity uint8) []byte {
	return []byte{wireMagic, byte(kindPad), pad, velocity}
}

func decode(frame []byte) (message, bool) {
	if len(frame) < 2 || frame[0] != wireMagic {
		return message{}, false
	}
	switch kind(frame[1]) {
	case kindPad:
		if len(frame) < 4 {
			return message{}, false
		}
		return message{kind: kindPad, pad: frame[2], velocity: frame[3]}, true
	}
	return message{}, false
}
