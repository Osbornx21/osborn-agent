package stackchanapp

import (
	"bytes"
	"errors"
	"testing"
)

func TestMessageTypeValuesMatchOfficialStackChanAppWS(t *testing.T) {
	cases := map[MessageType]byte{
		TypeOpus:              0x01,
		TypeJpeg:              0x02,
		TypeControlAvatar:     0x03,
		TypeControlMotion:     0x04,
		TypeStartCameraStream: 0x05,
		TypeStopCameraStream:  0x06,
		TypeTextMessage:       0x07,
		TypeRequestCall:       0x09,
		TypeDeclineCall:       0x0A,
		TypeAcceptCall:        0x0B,
		TypeEndCall:           0x0C,
		TypeSetDeviceName:     0x0D,
		TypeGetDeviceName:     0x0E,
		TypeHeartbeatPing:     0x10,
		TypeHeartbeatPong:     0x11,
		TypeVideoModeOn:       0x12,
		TypeVideoModeOff:      0x13,
		TypeDanceSequence:     0x14,
		TypeGetAvatarPosture:  0x15,
		TypeDeviceOffline:     0x16,
		TypeDeviceOnline:      0x17,
		TypeStartAudioStream:  0x18,
		TypeStopAudioStream:   0x19,
		TypeAimedTakePhoto:    0x1A,
	}
	for typ, want := range cases {
		if got := typ.Byte(); got != want {
			t.Fatalf("%s byte = 0x%02x, want 0x%02x", typ, got, want)
		}
	}
}

func TestEncodeDecodeFrameUsesOfficialHeader(t *testing.T) {
	payload := []byte(`{"name":"A21","content":"hello"}`)
	frame := Frame{Type: TypeTextMessage, Payload: payload}

	encoded := EncodeFrame(frame)
	wantHeader := []byte{0x07, 0x00, 0x00, 0x00, byte(len(payload))}
	if !bytes.Equal(encoded[:5], wantHeader) {
		t.Fatalf("header = % x, want % x", encoded[:5], wantHeader)
	}
	if !bytes.Equal(encoded[5:], payload) {
		t.Fatalf("payload = %q, want %q", encoded[5:], payload)
	}

	decoded, err := DecodeFrame(encoded)
	if err != nil {
		t.Fatalf("DecodeFrame() error = %v", err)
	}
	if decoded.Type != TypeTextMessage || !bytes.Equal(decoded.Payload, payload) {
		t.Fatalf("decoded = %+v, want type %s payload %q", decoded, TypeTextMessage, payload)
	}
}

func TestEncodeFrameAllowsEmptyPayload(t *testing.T) {
	encoded := EncodeFrame(Frame{Type: TypeHeartbeatPing})
	want := []byte{0x10, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded = % x, want % x", encoded, want)
	}

	decoded, err := DecodeFrame(encoded)
	if err != nil {
		t.Fatalf("DecodeFrame() error = %v", err)
	}
	if decoded.Type != TypeHeartbeatPing || len(decoded.Payload) != 0 {
		t.Fatalf("decoded = %+v, want ping with empty payload", decoded)
	}
}

func TestDecodeFrameRejectsLengthMismatch(t *testing.T) {
	_, err := DecodeFrame([]byte{0x03, 0x00, 0x00, 0x00, 0x02, 'a'})
	if !errors.Is(err, ErrFrameLengthMismatch) {
		t.Fatalf("DecodeFrame() error = %v, want ErrFrameLengthMismatch", err)
	}
}

func TestDecodeFrameRejectsShortHeader(t *testing.T) {
	_, err := DecodeFrame([]byte{0x03, 0x00, 0x00})
	if !errors.Is(err, ErrFrameTooShort) {
		t.Fatalf("DecodeFrame() error = %v, want ErrFrameTooShort", err)
	}
}

func TestFramePayloadsAreCopied(t *testing.T) {
	encoded := EncodeString(TypeControlMotion, "abc")
	encoded[5] = 'x'
	decoded, err := DecodeFrame(encoded)
	if err != nil {
		t.Fatalf("DecodeFrame() error = %v", err)
	}
	encoded[5] = 'y'
	if string(decoded.Payload) != "xbc" {
		t.Fatalf("decoded payload changed after source mutation: %q", decoded.Payload)
	}

	source := []byte("avatar")
	out := EncodeFrame(Frame{Type: TypeControlAvatar, Payload: source})
	source[0] = 'X'
	if string(out[5:]) != "avatar" {
		t.Fatalf("encoded payload aliases source: %q", out[5:])
	}
}
