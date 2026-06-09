//go:build cgo

package audio

/*
#cgo darwin CFLAGS: -I/opt/homebrew/include
#cgo darwin LDFLAGS: -L/opt/homebrew/lib -lopus
#cgo linux LDFLAGS: -lopus
#include <stdlib.h>
#include <opus/opus.h>

static int a21_opus_encoder_apply_speech_tuning(OpusEncoder* encoder, opus_int32 bitrate, opus_int32 complexity) {
    int err = opus_encoder_ctl(encoder, OPUS_SET_BITRATE(bitrate));
    if (err != OPUS_OK) return err;
    err = opus_encoder_ctl(encoder, OPUS_SET_COMPLEXITY(complexity));
    if (err != OPUS_OK) return err;
    err = opus_encoder_ctl(encoder, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE));
    if (err != OPUS_OK) return err;
    err = opus_encoder_ctl(encoder, OPUS_SET_VBR(1));
    if (err != OPUS_OK) return err;
    err = opus_encoder_ctl(encoder, OPUS_SET_DTX(0));
    if (err != OPUS_OK) return err;
    return opus_encoder_ctl(encoder, OPUS_SET_INBAND_FEC(0));
}

static int a21_opus_encoder_get_bitrate(OpusEncoder* encoder, opus_int32* value) {
    return opus_encoder_ctl(encoder, OPUS_GET_BITRATE(value));
}

static int a21_opus_encoder_get_complexity(OpusEncoder* encoder, opus_int32* value) {
    return opus_encoder_ctl(encoder, OPUS_GET_COMPLEXITY(value));
}

static int a21_opus_encoder_get_vbr(OpusEncoder* encoder, opus_int32* value) {
    return opus_encoder_ctl(encoder, OPUS_GET_VBR(value));
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"unsafe"
)

const (
	maxOpusPacketBytes            = 4000
	minOpusSpeechBitrateBPS       = 24000
	maxOpusSpeechBitrateBPS       = 96000
	minOpusSpeechComplexity       = 1
	maxOpusSpeechComplexity       = 10
	defaultOpusSpeechBitrateBPS   = 64000
	defaultOpusSpeechComplexity   = 10
	defaultOpusSpeechPacketTarget = "speech-quality"
)

type LibOpusSpeechTuning struct {
	BitrateBPS int
	Complexity int
}

type LibOpusPCMEncoder struct {
	ptr             *C.OpusEncoder
	sampleRateHz    int
	channels        int
	frameDurationMS int
	frameSamples    int
}

func NewLibOpusPCMEncoder(sampleRateHz int, channels int, frameDurationMS int) (*LibOpusPCMEncoder, error) {
	return NewLibOpusPCMEncoderWithTuning(sampleRateHz, channels, frameDurationMS, LibOpusSpeechTuning{})
}

func NewLibOpusPCMEncoderWithTuning(sampleRateHz int, channels int, frameDurationMS int, tuning LibOpusSpeechTuning) (*LibOpusPCMEncoder, error) {
	if sampleRateHz <= 0 {
		sampleRateHz = DefaultDownlinkRateHz
	}
	if channels <= 0 {
		channels = DefaultChannels
	}
	if frameDurationMS <= 0 {
		frameDurationMS = DefaultFrameDurationMS
	}
	frameSamples := sampleRateHz * frameDurationMS / 1000
	if frameSamples <= 0 {
		return nil, fmt.Errorf("%w: invalid frame duration", ErrOpusEncodeFailed)
	}
	tuning = normalizedLibOpusSpeechTuning(tuning)
	var opusErr C.int
	ptr := C.opus_encoder_create(C.opus_int32(sampleRateHz), C.int(channels), C.OPUS_APPLICATION_AUDIO, &opusErr)
	if ptr == nil || opusErr != C.OPUS_OK {
		return nil, fmt.Errorf("%w: create encoder: %s", ErrOpusEncodeFailed, opusErrorString(opusErr))
	}
	if err := C.a21_opus_encoder_apply_speech_tuning(ptr, C.opus_int32(tuning.BitrateBPS), C.opus_int32(tuning.Complexity)); err != C.OPUS_OK {
		C.opus_encoder_destroy(ptr)
		return nil, fmt.Errorf("%w: configure %s encoder: %s", ErrOpusEncodeFailed, defaultOpusSpeechPacketTarget, opusErrorString(err))
	}
	return &LibOpusPCMEncoder{
		ptr:             ptr,
		sampleRateHz:    sampleRateHz,
		channels:        channels,
		frameDurationMS: frameDurationMS,
		frameSamples:    frameSamples,
	}, nil
}

func normalizedLibOpusSpeechTuning(tuning LibOpusSpeechTuning) LibOpusSpeechTuning {
	if tuning.BitrateBPS < minOpusSpeechBitrateBPS || tuning.BitrateBPS > maxOpusSpeechBitrateBPS {
		tuning.BitrateBPS = defaultOpusSpeechBitrateBPS
	}
	if tuning.Complexity < minOpusSpeechComplexity || tuning.Complexity > maxOpusSpeechComplexity {
		tuning.Complexity = defaultOpusSpeechComplexity
	}
	return tuning
}

func (e *LibOpusPCMEncoder) EncodePCM(pcm []byte) ([]byte, error) {
	if e == nil || e.ptr == nil {
		return nil, ErrOpusEncoderUnavailable
	}
	expectedBytes := e.frameSamples * e.channels * 2
	if len(pcm) != expectedBytes {
		return nil, fmt.Errorf("%w: pcm frame has %d bytes, want %d", ErrOpusEncodeFailed, len(pcm), expectedBytes)
	}
	samples := make([]int16, e.frameSamples*e.channels)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	out := make([]byte, maxOpusPacketBytes)
	encoded := C.opus_encode(
		e.ptr,
		(*C.opus_int16)(unsafe.Pointer(&samples[0])),
		C.int(e.frameSamples),
		(*C.uchar)(unsafe.Pointer(&out[0])),
		C.opus_int32(len(out)),
	)
	if encoded < 0 {
		return nil, fmt.Errorf("%w: %s", ErrOpusEncodeFailed, opusErrorString(encoded))
	}
	packet := make([]byte, int(encoded))
	copy(packet, out[:int(encoded)])
	return packet, nil
}

func (e *LibOpusPCMEncoder) bitrateBPS() (int, error) {
	if e == nil || e.ptr == nil {
		return 0, ErrOpusEncoderUnavailable
	}
	var value C.opus_int32
	if err := C.a21_opus_encoder_get_bitrate(e.ptr, &value); err != C.OPUS_OK {
		return 0, fmt.Errorf("%w: get bitrate: %s", ErrOpusEncodeFailed, opusErrorString(err))
	}
	return int(value), nil
}

func (e *LibOpusPCMEncoder) complexity() (int, error) {
	if e == nil || e.ptr == nil {
		return 0, ErrOpusEncoderUnavailable
	}
	var value C.opus_int32
	if err := C.a21_opus_encoder_get_complexity(e.ptr, &value); err != C.OPUS_OK {
		return 0, fmt.Errorf("%w: get complexity: %s", ErrOpusEncodeFailed, opusErrorString(err))
	}
	return int(value), nil
}

func (e *LibOpusPCMEncoder) vbrEnabled() (bool, error) {
	if e == nil || e.ptr == nil {
		return false, ErrOpusEncoderUnavailable
	}
	var value C.opus_int32
	if err := C.a21_opus_encoder_get_vbr(e.ptr, &value); err != C.OPUS_OK {
		return false, fmt.Errorf("%w: get vbr: %s", ErrOpusEncodeFailed, opusErrorString(err))
	}
	return value != 0, nil
}

func (e *LibOpusPCMEncoder) Close() error {
	if e == nil || e.ptr == nil {
		return nil
	}
	C.opus_encoder_destroy(e.ptr)
	e.ptr = nil
	return nil
}
