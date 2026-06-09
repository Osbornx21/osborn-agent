package audio

import "errors"

var (
	ErrOpusEncoderUnavailable = errors.New("opus encoder is unavailable")
	ErrOpusEncodeFailed       = errors.New("opus encode failed")
)

type OpusPCMEncoder interface {
	EncodePCM(pcm []byte) ([]byte, error)
}

type OpusPCMEncoderFactory interface {
	NewOpusEncoder(sampleRateHz int, channels int, frameDurationMS int) (OpusPCMEncoder, error)
}

type OpusPCMEncoderFactoryFunc func(sampleRateHz int, channels int, frameDurationMS int) (OpusPCMEncoder, error)

func (f OpusPCMEncoderFactoryFunc) NewOpusEncoder(sampleRateHz int, channels int, frameDurationMS int) (OpusPCMEncoder, error) {
	return f(sampleRateHz, channels, frameDurationMS)
}
