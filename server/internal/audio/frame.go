package audio

import "time"

const (
	FormatOpus             = "opus"
	FormatPCM              = "pcm"
	DefaultSampleRateHz    = 16000
	DefaultDownlinkRateHz  = 24000
	DefaultFrameDurationMS = 60
	DefaultChannels        = 1
)

type Frame struct {
	Format          string
	SampleRateHz    int
	Channels        int
	FrameDurationMS int
	TimestampMS     uint32
	Payload         []byte
	ReceivedAt      time.Time
}

func NewOpusFrame(payload []byte, sampleRateHz int, frameDurationMS int, receivedAt time.Time) Frame {
	framePayload := make([]byte, len(payload))
	copy(framePayload, payload)

	return Frame{
		Format:          FormatOpus,
		SampleRateHz:    sampleRateHz,
		Channels:        DefaultChannels,
		FrameDurationMS: frameDurationMS,
		Payload:         framePayload,
		ReceivedAt:      receivedAt,
	}
}
