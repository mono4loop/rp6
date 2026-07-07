package p6

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoteFor(t *testing.T) {
	tests := []struct {
		name    string
		bank    int
		pad     int
		want    uint8
		wantErr bool
	}{
		{"A1 is the first pad note", 0, 1, 48, false},
		{"A6", 0, 6, 53, false},
		{"B1", 1, 1, 54, false},
		{"D6 is the last of page one", 3, 6, 71, false},
		{"E1 is the first of page two", 4, 1, 72, false},
		{"H6 is the last pad note", 7, 6, 95, false},
		{"bank too low", -1, 1, 0, true},
		{"bank too high", 8, 1, 0, true},
		{"pad zero", 0, 0, 0, true},
		{"pad too high", 0, 7, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NoteFor(tt.bank, tt.pad)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNoteForCoversAll48PadsUniquely(t *testing.T) {
	seen := map[uint8]string{}
	for bank := range NumBanks {
		for pad := 1; pad <= PadsPerBank; pad++ {
			note, err := NoteFor(bank, pad)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, note, uint8(firstPadNote))
			assert.LessOrEqual(t, note, uint8(lastPadNote))
			label := PadLabel(bank, pad)
			_, dup := seen[note]
			assert.False(t, dup, "note %d already used by %s (want unique for %s)", note, seen[note], label)
			seen[note] = label
		}
	}
	assert.Len(t, seen, NumPads, "expected all 48 pads to map to distinct notes")
}

func TestPadForNoteRoundTrip(t *testing.T) {
	for bank := range NumBanks {
		for pad := 1; pad <= PadsPerBank; pad++ {
			note, err := NoteFor(bank, pad)
			require.NoError(t, err)
			gotBank, gotPad, err := PadForNote(note)
			require.NoError(t, err)
			assert.Equal(t, bank, gotBank)
			assert.Equal(t, pad, gotPad)
		}
	}
}

func TestPadForNoteOutOfRange(t *testing.T) {
	_, _, err := PadForNote(47)
	assert.Error(t, err)
	_, _, err = PadForNote(96)
	assert.Error(t, err)
}

func TestBankName(t *testing.T) {
	assert.Equal(t, "A", BankName(0))
	assert.Equal(t, "H", BankName(7))
	assert.Equal(t, "?", BankName(-1))
	assert.Equal(t, "?", BankName(8))
}

func TestPadLabel(t *testing.T) {
	assert.Equal(t, "A1", PadLabel(0, 1))
	assert.Equal(t, "E6", PadLabel(4, 6))
}
