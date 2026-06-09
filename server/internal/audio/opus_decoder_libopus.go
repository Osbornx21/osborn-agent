//go:build cgo

package audio

/*
#cgo darwin CFLAGS: -I/opt/homebrew/include
#cgo darwin LDFLAGS: -L/opt/homebrew/lib -lopus
#cgo linux LDFLAGS: -lopus
#include <stdlib.h>
#include <opus/opus.h>
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"unsafe"
)

type LibOpusPCMDecoder struct {
	ptr          *C.OpusDecoder
	sampleRateHz int
	channels     int
}

func NewLibOpusPCMDecoder(sampleRateHz int, channels int) (*LibOpusPCMDecoder, error) {
	if sampleRateHz <= 0 {
		sampleRateHz = DefaultSampleRateHz
	}
	if channels <= 0 {
		channels = DefaultChannels
	}
	var opusErr C.int
	ptr := C.opus_decoder_create(C.opus_int32(sampleRateHz), C.int(channels), &opusErr)
	if ptr == nil || opusErr != C.OPUS_OK {
		return nil, fmt.Errorf("%w: create decoder: %s", ErrOpusDecodeFailed, opusErrorString(opusErr))
	}
	return &LibOpusPCMDecoder{
		ptr:          ptr,
		sampleRateHz: sampleRateHz,
		channels:     channels,
	}, nil
}

func (d *LibOpusPCMDecoder) DecodeOpus(frame Frame) ([]byte, error) {
	if d == nil || d.ptr == nil {
		return nil, ErrOpusDecoderUnavailable
	}
	if frame.Format != FormatOpus || frame.SampleRateHz != d.sampleRateHz || frame.Channels != d.channels {
		return nil, fmt.Errorf("%w: frame does not match decoder configuration", ErrOpusDecodeFailed)
	}
	if len(frame.Payload) == 0 {
		return nil, fmt.Errorf("%w: empty opus payload", ErrOpusDecodeFailed)
	}
	frameSamples := d.sampleRateHz * frame.FrameDurationMS / 1000
	if frameSamples <= 0 {
		return nil, fmt.Errorf("%w: invalid frame duration", ErrOpusDecodeFailed)
	}
	samples := make([]int16, frameSamples*d.channels)
	decoded := C.opus_decode(
		d.ptr,
		(*C.uchar)(unsafe.Pointer(&frame.Payload[0])),
		C.opus_int32(len(frame.Payload)),
		(*C.opus_int16)(unsafe.Pointer(&samples[0])),
		C.int(frameSamples),
		0,
	)
	if decoded < 0 {
		return nil, fmt.Errorf("%w: %s", ErrOpusDecodeFailed, opusErrorString(decoded))
	}
	outSamples := int(decoded) * d.channels
	out := make([]byte, outSamples*2)
	for i := 0; i < outSamples; i++ {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(samples[i]))
	}
	return out, nil
}

func (d *LibOpusPCMDecoder) Close() error {
	if d == nil || d.ptr == nil {
		return nil
	}
	C.opus_decoder_destroy(d.ptr)
	d.ptr = nil
	return nil
}

func opusErrorString(code C.int) string {
	return C.GoString(C.opus_strerror(code))
}
