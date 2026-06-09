package stackchanapp

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const HeaderSize = 5

var (
	ErrFrameTooShort       = errors.New("stackchan app frame too short")
	ErrFrameLengthMismatch = errors.New("stackchan app frame length mismatch")
)

type MessageType byte

const (
	TypeOpus              MessageType = 0x01
	TypeJpeg              MessageType = 0x02
	TypeControlAvatar     MessageType = 0x03
	TypeControlMotion     MessageType = 0x04
	TypeStartCameraStream MessageType = 0x05
	TypeStopCameraStream  MessageType = 0x06
	TypeTextMessage       MessageType = 0x07
	TypeRequestCall       MessageType = 0x09
	TypeDeclineCall       MessageType = 0x0A
	TypeAcceptCall        MessageType = 0x0B
	TypeEndCall           MessageType = 0x0C
	TypeSetDeviceName     MessageType = 0x0D
	TypeGetDeviceName     MessageType = 0x0E
	TypeHeartbeatPing     MessageType = 0x10
	TypeHeartbeatPong     MessageType = 0x11
	TypeVideoModeOn       MessageType = 0x12
	TypeVideoModeOff      MessageType = 0x13
	TypeDanceSequence     MessageType = 0x14
	TypeGetAvatarPosture  MessageType = 0x15
	TypeDeviceOffline     MessageType = 0x16
	TypeDeviceOnline      MessageType = 0x17
	TypeStartAudioStream  MessageType = 0x18
	TypeStopAudioStream   MessageType = 0x19
	TypeAimedTakePhoto    MessageType = 0x1A
)

type Frame struct {
	Type    MessageType
	Payload []byte
}

func (t MessageType) Byte() byte {
	return byte(t)
}

func (t MessageType) String() string {
	switch t {
	case TypeOpus:
		return "opus"
	case TypeJpeg:
		return "jpeg"
	case TypeControlAvatar:
		return "control_avatar"
	case TypeControlMotion:
		return "control_motion"
	case TypeStartCameraStream:
		return "start_camera_stream"
	case TypeStopCameraStream:
		return "stop_camera_stream"
	case TypeTextMessage:
		return "text_message"
	case TypeRequestCall:
		return "request_call"
	case TypeDeclineCall:
		return "decline_call"
	case TypeAcceptCall:
		return "accept_call"
	case TypeEndCall:
		return "end_call"
	case TypeSetDeviceName:
		return "set_device_name"
	case TypeGetDeviceName:
		return "get_device_name"
	case TypeHeartbeatPing:
		return "heartbeat_ping"
	case TypeHeartbeatPong:
		return "heartbeat_pong"
	case TypeVideoModeOn:
		return "video_mode_on"
	case TypeVideoModeOff:
		return "video_mode_off"
	case TypeDanceSequence:
		return "dance_sequence"
	case TypeGetAvatarPosture:
		return "get_avatar_posture"
	case TypeDeviceOffline:
		return "device_offline"
	case TypeDeviceOnline:
		return "device_online"
	case TypeStartAudioStream:
		return "start_audio_stream"
	case TypeStopAudioStream:
		return "stop_audio_stream"
	case TypeAimedTakePhoto:
		return "aimed_take_photo"
	default:
		return fmt.Sprintf("unknown_0x%02x", byte(t))
	}
}

func EncodeFrame(frame Frame) []byte {
	payloadLen := len(frame.Payload)
	out := make([]byte, HeaderSize+payloadLen)
	out[0] = frame.Type.Byte()
	binary.BigEndian.PutUint32(out[1:HeaderSize], uint32(payloadLen))
	copy(out[HeaderSize:], frame.Payload)
	return out
}

func EncodeString(messageType MessageType, payload string) []byte {
	return EncodeFrame(Frame{Type: messageType, Payload: []byte(payload)})
}

func DecodeFrame(data []byte) (Frame, error) {
	if len(data) < HeaderSize {
		return Frame{}, ErrFrameTooShort
	}
	payloadLen := int(binary.BigEndian.Uint32(data[1:HeaderSize]))
	if len(data)-HeaderSize != payloadLen {
		return Frame{}, fmt.Errorf("%w: header=%d actual=%d", ErrFrameLengthMismatch, payloadLen, len(data)-HeaderSize)
	}
	payload := make([]byte, payloadLen)
	copy(payload, data[HeaderSize:])
	return Frame{Type: MessageType(data[0]), Payload: payload}, nil
}
