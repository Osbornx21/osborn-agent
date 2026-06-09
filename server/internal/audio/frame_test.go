package audio

import (
	"bytes"
	"testing"
	"time"
)

func TestNewOpusFrameCopiesPayloadAndSetsMetadata(t *testing.T) {
	payload := []byte{0x01, 0x02}
	receivedAt := time.Date(2026, 6, 6, 1, 2, 3, 0, time.UTC)

	frame := NewOpusFrame(payload, 24000, 60, receivedAt)
	payload[0] = 0xff

	if frame.Format != FormatOpus {
		t.Fatalf("format = %q, want opus", frame.Format)
	}
	if frame.SampleRateHz != 24000 {
		t.Fatalf("sample rate = %d, want 24000", frame.SampleRateHz)
	}
	if frame.Channels != DefaultChannels {
		t.Fatalf("channels = %d, want %d", frame.Channels, DefaultChannels)
	}
	if frame.FrameDurationMS != 60 {
		t.Fatalf("frame duration = %d, want 60", frame.FrameDurationMS)
	}
	if !frame.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("received_at = %v, want %v", frame.ReceivedAt, receivedAt)
	}
	if !bytes.Equal(frame.Payload, []byte{0x01, 0x02}) {
		t.Fatalf("payload = %v, want original copy", frame.Payload)
	}
}
