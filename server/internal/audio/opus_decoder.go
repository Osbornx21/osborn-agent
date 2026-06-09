package audio

import "errors"

var (
	ErrOpusDecoderUnavailable = errors.New("opus decoder is unavailable")
	ErrOpusDecodeFailed       = errors.New("opus decode failed")
)

type OpusPCMDecoder interface {
	DecodeOpus(frame Frame) ([]byte, error)
}
