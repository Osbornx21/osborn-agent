//go:build !cgo

package audio

type LibOpusPCMEncoder struct{}

type LibOpusSpeechTuning struct {
	BitrateBPS int
	Complexity int
}

func NewLibOpusPCMEncoder(sampleRateHz int, channels int, frameDurationMS int) (*LibOpusPCMEncoder, error) {
	return nil, ErrOpusEncoderUnavailable
}

func NewLibOpusPCMEncoderWithTuning(sampleRateHz int, channels int, frameDurationMS int, tuning LibOpusSpeechTuning) (*LibOpusPCMEncoder, error) {
	return nil, ErrOpusEncoderUnavailable
}

func (e *LibOpusPCMEncoder) EncodePCM(pcm []byte) ([]byte, error) {
	return nil, ErrOpusEncoderUnavailable
}

func (e *LibOpusPCMEncoder) Close() error {
	return nil
}
