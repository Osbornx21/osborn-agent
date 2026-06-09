package xiaozhi

import (
	"encoding/binary"
	"fmt"
	"time"

	"stackchan-gateway/internal/audio"
)

const (
	BinaryProtocolV1 = 1
	BinaryProtocolV2 = 2
	BinaryProtocolV3 = 3

	ErrorCodeEmptyAudioFrame            = "EMPTY_AUDIO_FRAME"
	ErrorCodeUnsupportedBinaryProtocol  = "UNSUPPORTED_BINARY_PROTOCOL_VERSION"
	ErrorCodeUnsupportedBinaryFrameType = "UNSUPPORTED_BINARY_FRAME_TYPE"
	ErrorCodeMalformedBinaryFrame       = "MALFORMED_BINARY_FRAME"
)

type BinaryFrameOptions struct {
	ProtocolVersion int
	SampleRateHz    int
	FrameDurationMS int
	TimestampMS     uint32
	ReceivedAt      time.Time
}

func ParseBinaryAudioFrame(data []byte, options BinaryFrameOptions) (audio.Frame, error) {
	version := options.ProtocolVersion
	if version == 0 {
		version = BinaryProtocolV1
	}

	switch version {
	case BinaryProtocolV1:
		return parseRawOpusV1(data, options)
	case BinaryProtocolV2:
		return parseWrappedOpusV2(data, options)
	case BinaryProtocolV3:
		return parseWrappedOpusV3(data, options)
	default:
		return audio.Frame{}, &ProtocolError{
			Code:    ErrorCodeUnsupportedBinaryProtocol,
			Field:   "protocol_version",
			Message: fmt.Sprintf("unsupported binary protocol v%d", version),
		}
	}
}

func parseRawOpusV1(data []byte, options BinaryFrameOptions) (audio.Frame, error) {
	if len(data) == 0 {
		return audio.Frame{}, &ProtocolError{
			Code:    ErrorCodeEmptyAudioFrame,
			Field:   "payload",
			Message: "binary audio frame must not be empty",
		}
	}

	sampleRateHz := options.SampleRateHz
	if sampleRateHz == 0 {
		sampleRateHz = XiaozhiUplinkSampleRateHz
	}

	frameDurationMS := options.FrameDurationMS
	if frameDurationMS == 0 {
		frameDurationMS = XiaozhiFrameDurationMS
	}

	receivedAt := options.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}

	return audio.NewOpusFrame(data, sampleRateHz, frameDurationMS, receivedAt), nil
}

func parseWrappedOpusV2(data []byte, options BinaryFrameOptions) (audio.Frame, error) {
	const headerSize = 16
	if len(data) < headerSize {
		return audio.Frame{}, malformedBinaryFrame("payload", "binary protocol v2 frame is shorter than header")
	}
	version := int(binary.BigEndian.Uint16(data[0:2]))
	if version != BinaryProtocolV2 {
		return audio.Frame{}, malformedBinaryFrame("version", fmt.Sprintf("binary protocol v2 embedded version is %d", version))
	}
	frameType := binary.BigEndian.Uint16(data[2:4])
	if frameType != 0 {
		return audio.Frame{}, unsupportedBinaryFrameType(frameType)
	}
	timestampMS := binary.BigEndian.Uint32(data[8:12])
	payloadSize := binary.BigEndian.Uint32(data[12:16])
	if payloadSize != uint32(len(data)-headerSize) {
		return audio.Frame{}, malformedBinaryFrame("payload_size", fmt.Sprintf("binary protocol v2 payload_size=%d bytes=%d", payloadSize, len(data)-headerSize))
	}
	frame, err := parseRawOpusV1(data[headerSize:], options)
	if err != nil {
		return audio.Frame{}, err
	}
	frame.TimestampMS = timestampMS
	return frame, nil
}

func parseWrappedOpusV3(data []byte, options BinaryFrameOptions) (audio.Frame, error) {
	const headerSize = 4
	if len(data) < headerSize {
		return audio.Frame{}, malformedBinaryFrame("payload", "binary protocol v3 frame is shorter than header")
	}
	frameType := data[0]
	if frameType != 0 {
		return audio.Frame{}, unsupportedBinaryFrameType(uint16(frameType))
	}
	payloadBytes := len(data) - headerSize
	if payloadBytes > 0xffff {
		return audio.Frame{}, malformedBinaryFrame("payload_size", fmt.Sprintf("binary protocol v3 payload too large: %d", payloadBytes))
	}
	payloadSize := binary.BigEndian.Uint16(data[2:4])
	if int(payloadSize) != payloadBytes {
		return audio.Frame{}, malformedBinaryFrame("payload_size", fmt.Sprintf("binary protocol v3 payload_size=%d bytes=%d", payloadSize, payloadBytes))
	}
	return parseRawOpusV1(data[headerSize:], options)
}

func EncodeBinaryAudioFrame(payload []byte, options BinaryFrameOptions) ([]byte, error) {
	version := options.ProtocolVersion
	if version == 0 {
		version = BinaryProtocolV1
	}
	if len(payload) == 0 {
		return nil, &ProtocolError{
			Code:    ErrorCodeEmptyAudioFrame,
			Field:   "payload",
			Message: "binary audio frame must not be empty",
		}
	}

	switch version {
	case BinaryProtocolV1:
		return clonePayload(payload), nil
	case BinaryProtocolV2:
		frame := make([]byte, 16+len(payload))
		binary.BigEndian.PutUint16(frame[0:2], BinaryProtocolV2)
		binary.BigEndian.PutUint16(frame[2:4], 0)
		binary.BigEndian.PutUint32(frame[4:8], 0)
		binary.BigEndian.PutUint32(frame[8:12], options.TimestampMS)
		binary.BigEndian.PutUint32(frame[12:16], uint32(len(payload)))
		copy(frame[16:], payload)
		return frame, nil
	case BinaryProtocolV3:
		if len(payload) > 0xffff {
			return nil, malformedBinaryFrame("payload_size", fmt.Sprintf("binary protocol v3 payload too large: %d", len(payload)))
		}
		frame := make([]byte, 4+len(payload))
		frame[0] = 0
		frame[1] = 0
		binary.BigEndian.PutUint16(frame[2:4], uint16(len(payload)))
		copy(frame[4:], payload)
		return frame, nil
	default:
		return nil, &ProtocolError{
			Code:    ErrorCodeUnsupportedBinaryProtocol,
			Field:   "protocol_version",
			Message: fmt.Sprintf("unsupported binary protocol v%d", version),
		}
	}
}

func clonePayload(payload []byte) []byte {
	clone := make([]byte, len(payload))
	copy(clone, payload)
	return clone
}

func unsupportedBinaryFrameType(frameType uint16) *ProtocolError {
	return &ProtocolError{
		Code:    ErrorCodeUnsupportedBinaryFrameType,
		Field:   "type",
		Message: fmt.Sprintf("unsupported binary frame type %d", frameType),
	}
}

func malformedBinaryFrame(field string, message string) *ProtocolError {
	return &ProtocolError{
		Code:    ErrorCodeMalformedBinaryFrame,
		Field:   field,
		Message: message,
	}
}
