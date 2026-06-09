//go:build !cgo

package audio

type LibOpusPCMDecoder struct{}

func NewLibOpusPCMDecoder(sampleRateHz int, channels int) (*LibOpusPCMDecoder, error) {
	return nil, ErrOpusDecoderUnavailable
}

func (d *LibOpusPCMDecoder) DecodeOpus(frame Frame) ([]byte, error) {
	return nil, ErrOpusDecoderUnavailable
}

func (d *LibOpusPCMDecoder) Close() error {
	return nil
}
