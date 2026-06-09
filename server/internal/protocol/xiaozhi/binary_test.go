package xiaozhi

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestBinaryV1NonEmptyFrameReturnsAudioFrame(t *testing.T) {
	payload := []byte{0x11, 0x22, 0x33}
	receivedAt := time.Date(2026, 6, 6, 1, 2, 3, 4, time.UTC)

	frame, err := ParseBinaryAudioFrame(payload, BinaryFrameOptions{
		ProtocolVersion: BinaryProtocolV1,
		SampleRateHz:    XiaozhiDownlinkRateHz,
		FrameDurationMS: XiaozhiFrameDurationMS,
		ReceivedAt:      receivedAt,
	})

	if err != nil {
		t.Fatalf("ParseBinaryAudioFrame() error = %v", err)
	}
	if !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("payload = %v, want %v", frame.Payload, payload)
	}
	if frame.SampleRateHz != XiaozhiDownlinkRateHz {
		t.Fatalf("sample rate = %d, want %d", frame.SampleRateHz, XiaozhiDownlinkRateHz)
	}
	if frame.FrameDurationMS != XiaozhiFrameDurationMS {
		t.Fatalf("frame duration = %d, want %d", frame.FrameDurationMS, XiaozhiFrameDurationMS)
	}
	if !frame.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("received_at = %v, want %v", frame.ReceivedAt, receivedAt)
	}
}

func TestBinaryV1CopiesPayload(t *testing.T) {
	payload := []byte{0x11, 0x22, 0x33}

	frame, err := ParseBinaryAudioFrame(payload, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV1})
	if err != nil {
		t.Fatalf("ParseBinaryAudioFrame() error = %v", err)
	}

	payload[0] = 0xff

	if frame.Payload[0] != 0x11 {
		t.Fatalf("frame payload was mutated after parse: %v", frame.Payload)
	}
}

func TestBinaryV1DefaultsToXiaozhiUplinkAudioParams(t *testing.T) {
	frame, err := ParseBinaryAudioFrame([]byte{0x01}, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV1})
	if err != nil {
		t.Fatalf("ParseBinaryAudioFrame() error = %v", err)
	}

	if frame.SampleRateHz != XiaozhiUplinkSampleRateHz {
		t.Fatalf("sample rate = %d, want uplink %d", frame.SampleRateHz, XiaozhiUplinkSampleRateHz)
	}
	if frame.FrameDurationMS != XiaozhiFrameDurationMS {
		t.Fatalf("frame duration = %d, want %d", frame.FrameDurationMS, XiaozhiFrameDurationMS)
	}
}

func TestBinaryEmptyFrameFails(t *testing.T) {
	_, err := ParseBinaryAudioFrame(nil, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV1})

	requireProtocolError(t, err, ErrorCodeEmptyAudioFrame)
}

func TestBinaryV2ParsesOfficialWrappedOpusFrame(t *testing.T) {
	payload := []byte{0x11, 0x22, 0x33}
	data := make([]byte, 16+len(payload))
	binary.BigEndian.PutUint16(data[0:2], BinaryProtocolV2)
	binary.BigEndian.PutUint16(data[2:4], 0)
	binary.BigEndian.PutUint32(data[4:8], 0)
	binary.BigEndian.PutUint32(data[8:12], 123456)
	binary.BigEndian.PutUint32(data[12:16], uint32(len(payload)))
	copy(data[16:], payload)

	frame, err := ParseBinaryAudioFrame(data, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV2})

	if err != nil {
		t.Fatalf("ParseBinaryAudioFrame() error = %v", err)
	}
	if !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("payload = %v, want %v", frame.Payload, payload)
	}
	if frame.TimestampMS != 123456 {
		t.Fatalf("timestamp_ms = %d, want 123456", frame.TimestampMS)
	}
}

func TestBinaryV3ParsesOfficialWrappedOpusFrame(t *testing.T) {
	payload := []byte{0x44, 0x55}
	data := make([]byte, 4+len(payload))
	data[0] = 0
	data[1] = 0
	binary.BigEndian.PutUint16(data[2:4], uint16(len(payload)))
	copy(data[4:], payload)

	frame, err := ParseBinaryAudioFrame(data, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV3})

	if err != nil {
		t.Fatalf("ParseBinaryAudioFrame() error = %v", err)
	}
	if !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("payload = %v, want %v", frame.Payload, payload)
	}
	if frame.TimestampMS != 0 {
		t.Fatalf("timestamp_ms = %d, want 0", frame.TimestampMS)
	}
}

func TestBinaryWrappedFrameRejectsUnsupportedType(t *testing.T) {
	data := make([]byte, 17)
	binary.BigEndian.PutUint16(data[0:2], BinaryProtocolV2)
	binary.BigEndian.PutUint16(data[2:4], 1)
	binary.BigEndian.PutUint32(data[12:16], 1)
	data[16] = 0x01

	_, err := ParseBinaryAudioFrame(data, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV2})

	requireProtocolError(t, err, ErrorCodeUnsupportedBinaryFrameType)
}

func TestBinaryWrappedFrameRejectsPayloadSizeMismatch(t *testing.T) {
	data := make([]byte, 4)
	data[0] = 0
	binary.BigEndian.PutUint16(data[2:4], 3)

	_, err := ParseBinaryAudioFrame(data, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV3})

	requireProtocolError(t, err, ErrorCodeMalformedBinaryFrame)
}

func TestBinaryV3RejectsOversizedPayloadBeforeUint16Wrap(t *testing.T) {
	data := make([]byte, 4+0x10001)
	data[0] = 0
	binary.BigEndian.PutUint16(data[2:4], 1)

	_, err := ParseBinaryAudioFrame(data, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV3})

	requireProtocolError(t, err, ErrorCodeMalformedBinaryFrame)
}

func TestEncodeBinaryAudioFrameWrapsV2AndV3(t *testing.T) {
	payload := []byte{0xaa, 0xbb}

	v2, err := EncodeBinaryAudioFrame(payload, BinaryFrameOptions{
		ProtocolVersion: BinaryProtocolV2,
		TimestampMS:     98765,
	})
	if err != nil {
		t.Fatalf("EncodeBinaryAudioFrame(v2) error = %v", err)
	}
	if binary.BigEndian.Uint16(v2[0:2]) != BinaryProtocolV2 ||
		binary.BigEndian.Uint16(v2[2:4]) != 0 ||
		binary.BigEndian.Uint32(v2[8:12]) != 98765 ||
		binary.BigEndian.Uint32(v2[12:16]) != uint32(len(payload)) ||
		!bytes.Equal(v2[16:], payload) {
		t.Fatalf("v2 encoded frame = %v", v2)
	}

	v3, err := EncodeBinaryAudioFrame(payload, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV3})
	if err != nil {
		t.Fatalf("EncodeBinaryAudioFrame(v3) error = %v", err)
	}
	if v3[0] != 0 || v3[1] != 0 || binary.BigEndian.Uint16(v3[2:4]) != uint16(len(payload)) || !bytes.Equal(v3[4:], payload) {
		t.Fatalf("v3 encoded frame = %v", v3)
	}
}

func TestBinaryFrameMetadataIncludesReceivedAt(t *testing.T) {
	frame, err := ParseBinaryAudioFrame([]byte{0x01}, BinaryFrameOptions{ProtocolVersion: BinaryProtocolV1})
	if err != nil {
		t.Fatalf("ParseBinaryAudioFrame() error = %v", err)
	}

	if frame.ReceivedAt.IsZero() {
		t.Fatal("received_at is zero, want parse timestamp")
	}
}

func TestBinaryUnknownVersionReturnsUnsupported(t *testing.T) {
	_, err := ParseBinaryAudioFrame([]byte{0x01}, BinaryFrameOptions{ProtocolVersion: 99})

	requireProtocolError(t, err, ErrorCodeUnsupportedBinaryProtocol)
}
