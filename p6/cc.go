package p6

// Control Change numbers understood by the P-6.
//
// Per Roland's MIDI implementation, control-change messages are received on the
// Granular MIDI channel (default 4) or the Auto MIDI channel (default 15, which
// targets the pad currently selected on the hardware). Roland documents them as
// GRANULAR parameters.
//
// IMPORTANT: the sample-pad LOOP, GATE and playback-direction buttons have NO
// MIDI representation and cannot be controlled remotely.
const (
	CCLevel      uint8 = 7  // Level
	CCPan        uint8 = 10 // Pan
	CCFilterType uint8 = 12 // Filter type (0:Off 1:LPF 2:BPF 3:HPF 4+:PKG)
	CCLoFiInt    uint8 = 17 // Lo-Fi intensity
	CCAmpSwitch  uint8 = 28 // Amp envelope switch (0:Off 1+:On)
	CCEnvMode    uint8 = 29 // Envelope mode (0:ADSR 1:ADR 2+:Cyclic)
	CCFilterFreq uint8 = 74 // Filter cutoff frequency
	CCFilterReso uint8 = 71 // Filter resonance
	CCLoFiSwitch uint8 = 87 // Lo-Fi switch (0:Off 1+:On)
	CCSample     uint8 = 88 // Granular source sample select (0:A1 .. 127:H6)
	CCSendDelay  uint8 = 85 // Per-voice send to delay
	CCSendReverb uint8 = 86 // Per-voice send to reverb

	// Global delay/reverb effect parameters (Roland: "DELAY/REVERB").
	CCReverbTime  uint8 = 89 // Reverb time
	CCDelayTime   uint8 = 90 // Delay time
	CCReverbLevel uint8 = 91 // Reverb level
	CCDelayLevel  uint8 = 92 // Delay level
)

// AutoCC sends a control change on the Auto MIDI channel, which the P-6 applies
// to whichever pad is currently selected on the hardware.
func (d *Device) AutoCC(cc, value uint8) error {
	return d.ControlChange(d.cfg.AutoChannel, cc, value)
}

// GranularCC sends a control change on the Granular MIDI channel, controlling
// the GRANULAR engine regardless of the selected pad.
func (d *Device) GranularCC(cc, value uint8) error {
	return d.ControlChange(d.cfg.GranularChannel, cc, value)
}
