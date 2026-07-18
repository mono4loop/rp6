package mapped

// Relative-encoder decoders. Endless encoders that are configured to transmit
// *relative* movement disagree on the wire format; a binding's `rel=<encoding>`
// selects one. Each decodes one CC data byte into a signed detent count.
// (The Synido C16's factory encoders are absolute, so they use `abs` instead —
// these exist for controllers that send relative.)

// validEncoding reports whether name is a known rel= encoding.
func validEncoding(name string) bool {
	switch name {
	case "twoscomp", "signbit", "binoffset":
		return true
	}
	return false
}

// decodeRel converts one CC value to a signed detent count for the encoding.
func decodeRel(encoding string, v uint8) int {
	switch encoding {
	case "twoscomp":
		// 7-bit two's complement: 1..63 = +1..+63, 65..127 = -63..-1.
		if v < 64 {
			return int(v)
		}
		return int(v) - 128
	case "signbit":
		// bit 6 set = positive direction; low 6 bits = magnitude.
		mag := int(v & 0x3F)
		if v&0x40 != 0 {
			return mag
		}
		return -mag
	case "binoffset":
		// offset-64: 65 = +1, 63 = -1.
		return int(v) - 64
	}
	return 0
}
