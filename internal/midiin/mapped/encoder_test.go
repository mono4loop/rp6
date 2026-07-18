package mapped

import "testing"

func TestDecodeRelTwosComp(t *testing.T) {
	cases := map[uint8]int{0: 0, 1: 1, 63: 63, 64: -64, 127: -1, 65: -63}
	for v, want := range cases {
		if got := decodeRel("twoscomp", v); got != want {
			t.Errorf("twoscomp %d = %d, want %d", v, got, want)
		}
	}
}

func TestDecodeRelSignBit(t *testing.T) {
	// bit 6 set = positive; low 6 bits = magnitude.
	cases := map[uint8]int{0x41: 1, 0x01: -1, 0x43: 3, 0x03: -3, 0x40: 0}
	for v, want := range cases {
		if got := decodeRel("signbit", v); got != want {
			t.Errorf("signbit %#x = %d, want %d", v, got, want)
		}
	}
}

func TestDecodeRelBinOffset(t *testing.T) {
	cases := map[uint8]int{65: 1, 63: -1, 64: 0, 74: 10, 54: -10}
	for v, want := range cases {
		if got := decodeRel("binoffset", v); got != want {
			t.Errorf("binoffset %d = %d, want %d", v, got, want)
		}
	}
}

func TestValidEncoding(t *testing.T) {
	for _, ok := range []string{"twoscomp", "signbit", "binoffset"} {
		if !validEncoding(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	if validEncoding("bogus") {
		t.Error("bogus should be invalid")
	}
}
