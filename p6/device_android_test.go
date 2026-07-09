//go:build android

package p6

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mono4loop/rp6/midibridge"
)

type testOutputPort struct {
	messages [][]byte
}

func (p *testOutputPort) Send(data []byte) error {
	p.messages = append(p.messages, append([]byte(nil), data...))
	return nil
}

func TestAndroidDeviceReopenKeepsUSBOutput(t *testing.T) {
	midibridge.Reset()
	t.Cleanup(midibridge.Reset)

	const id = "p6-test"
	out := &testOutputPort{}
	midibridge.AddDevice(id, "P-6", true, true)
	midibridge.SetOutput(id, out)

	first, err := Open()
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := Open()
	require.NoError(t, err)
	require.NoError(t, second.PlayNote(KeyboardCenterNote, DefaultVelocity))
	require.Len(t, out.messages, 1)
}
