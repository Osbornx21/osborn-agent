//go:build cgo

package audio

import "testing"

func TestLibOpusPCMEncoderUsesSpeechQualityTuning(t *testing.T) {
	encoder, err := NewLibOpusPCMEncoder(DefaultDownlinkRateHz, DefaultChannels, DefaultFrameDurationMS)
	if err != nil {
		t.Fatalf("NewLibOpusPCMEncoder() error = %v", err)
	}
	defer encoder.Close()

	if got, err := encoder.bitrateBPS(); err != nil || got != defaultOpusSpeechBitrateBPS {
		t.Fatalf("bitrate = %d, err = %v; want %d", got, err, defaultOpusSpeechBitrateBPS)
	}
	if got, err := encoder.complexity(); err != nil || got != defaultOpusSpeechComplexity {
		t.Fatalf("complexity = %d, err = %v; want %d", got, err, defaultOpusSpeechComplexity)
	}
	if got, err := encoder.vbrEnabled(); err != nil || !got {
		t.Fatalf("vbr enabled = %t, err = %v; want true", got, err)
	}
}

func TestLibOpusPCMEncoderUsesConfiguredSpeechTuning(t *testing.T) {
	encoder, err := NewLibOpusPCMEncoderWithTuning(DefaultDownlinkRateHz, DefaultChannels, DefaultFrameDurationMS, LibOpusSpeechTuning{
		BitrateBPS: 48000,
		Complexity: 8,
	})
	if err != nil {
		t.Fatalf("NewLibOpusPCMEncoderWithTuning() error = %v", err)
	}
	defer encoder.Close()

	if got, err := encoder.bitrateBPS(); err != nil || got != 48000 {
		t.Fatalf("bitrate = %d, err = %v; want 48000", got, err)
	}
	if got, err := encoder.complexity(); err != nil || got != 8 {
		t.Fatalf("complexity = %d, err = %v; want 8", got, err)
	}
	if got, err := encoder.vbrEnabled(); err != nil || !got {
		t.Fatalf("vbr enabled = %t, err = %v; want true", got, err)
	}
}

func TestLibOpusPCMEncoderFallsBackForOutOfRangeTuning(t *testing.T) {
	encoder, err := NewLibOpusPCMEncoderWithTuning(DefaultDownlinkRateHz, DefaultChannels, DefaultFrameDurationMS, LibOpusSpeechTuning{
		BitrateBPS: 4000,
		Complexity: 99,
	})
	if err != nil {
		t.Fatalf("NewLibOpusPCMEncoderWithTuning() error = %v", err)
	}
	defer encoder.Close()

	if got, err := encoder.bitrateBPS(); err != nil || got != defaultOpusSpeechBitrateBPS {
		t.Fatalf("bitrate = %d, err = %v; want default %d", got, err, defaultOpusSpeechBitrateBPS)
	}
	if got, err := encoder.complexity(); err != nil || got != defaultOpusSpeechComplexity {
		t.Fatalf("complexity = %d, err = %v; want default %d", got, err, defaultOpusSpeechComplexity)
	}
}
