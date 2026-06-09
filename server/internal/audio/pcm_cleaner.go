package audio

import (
	"encoding/binary"
	"fmt"
	"math"
)

const defaultPCM16CleanerFadeMS = 3

type PCM16CleanerOptions struct {
	SampleRateHz int
	Channels     int
	FadeInMS     int
	FadeOutMS    int
	RemoveDC     bool
}

type PCM16StreamCleaner struct {
	sampleRateHz int
	channels     int
	fadeInMS     int
	fadeOutMS    int
	removeDC     bool
	framesSeen   int64
}

func NewPCM16StreamCleaner(options PCM16CleanerOptions) *PCM16StreamCleaner {
	sampleRateHz := options.SampleRateHz
	if sampleRateHz <= 0 {
		sampleRateHz = DefaultDownlinkRateHz
	}
	channels := options.Channels
	if channels <= 0 {
		channels = DefaultChannels
	}
	fadeInMS := options.FadeInMS
	if fadeInMS < 0 {
		fadeInMS = 0
	}
	fadeOutMS := options.FadeOutMS
	if fadeOutMS < 0 {
		fadeOutMS = 0
	}
	if options.FadeInMS == 0 && options.FadeOutMS == 0 {
		fadeInMS = defaultPCM16CleanerFadeMS
		fadeOutMS = defaultPCM16CleanerFadeMS
	}
	return &PCM16StreamCleaner{
		sampleRateHz: sampleRateHz,
		channels:     channels,
		fadeInMS:     fadeInMS,
		fadeOutMS:    fadeOutMS,
		removeDC:     options.RemoveDC,
	}
}

func (c *PCM16StreamCleaner) CleanFrame(pcm []byte, final bool) error {
	if c == nil || len(pcm) == 0 {
		return nil
	}
	if len(pcm)%2 != 0 {
		return fmt.Errorf("pcm16 data has odd byte length: %d", len(pcm))
	}
	if c.channels <= 0 {
		c.channels = DefaultChannels
	}
	if c.removeDC {
		removePCM16DCOffset(pcm, c.channels)
	}
	frameSamples := len(pcm) / 2 / c.channels
	if frameSamples <= 0 {
		return nil
	}
	if c.framesSeen == 0 && c.fadeInMS > 0 {
		applyPCM16Fade(pcm, c.channels, fadeSamples(c.sampleRateHz, c.fadeInMS, frameSamples), true)
	}
	if final && c.fadeOutMS > 0 {
		applyPCM16Fade(pcm, c.channels, fadeSamples(c.sampleRateHz, c.fadeOutMS, frameSamples), false)
	}
	c.framesSeen++
	return nil
}

func removePCM16DCOffset(pcm []byte, channels int) {
	if channels <= 0 {
		channels = 1
	}
	sampleCount := len(pcm) / 2
	sums := make([]int64, channels)
	counts := make([]int64, channels)
	for sampleIndex, offset := 0, 0; offset+1 < len(pcm); sampleIndex, offset = sampleIndex+1, offset+2 {
		channel := sampleIndex % channels
		sums[channel] += int64(int16(binary.LittleEndian.Uint16(pcm[offset : offset+2])))
		counts[channel]++
	}
	for sampleIndex, offset := 0, 0; sampleIndex < sampleCount; sampleIndex, offset = sampleIndex+1, offset+2 {
		channel := sampleIndex % channels
		if counts[channel] == 0 {
			continue
		}
		mean := int(math.Round(float64(sums[channel]) / float64(counts[channel])))
		sample := int(int16(binary.LittleEndian.Uint16(pcm[offset:offset+2]))) - mean
		binary.LittleEndian.PutUint16(pcm[offset:offset+2], uint16(clampPCM16(sample)))
	}
}

func fadeSamples(sampleRateHz int, fadeMS int, frameSamples int) int {
	if sampleRateHz <= 0 || fadeMS <= 0 || frameSamples <= 0 {
		return 0
	}
	samples := sampleRateHz * fadeMS / 1000
	if samples > frameSamples {
		return frameSamples
	}
	return samples
}

func applyPCM16Fade(pcm []byte, channels int, fadeFrameSamples int, fadeIn bool) {
	if fadeFrameSamples <= 0 {
		return
	}
	for frameSample := 0; frameSample < fadeFrameSamples; frameSample++ {
		var gain float64
		if fadeIn {
			gain = float64(frameSample+1) / float64(fadeFrameSamples)
		} else {
			gain = float64(fadeFrameSamples-frameSample) / float64(fadeFrameSamples)
		}
		targetFrame := frameSample
		if !fadeIn {
			totalFrames := len(pcm) / 2 / channels
			targetFrame = totalFrames - fadeFrameSamples + frameSample
		}
		for channel := 0; channel < channels; channel++ {
			offset := (targetFrame*channels + channel) * 2
			sample := int16(binary.LittleEndian.Uint16(pcm[offset : offset+2]))
			scaled := int(math.Round(float64(sample) * gain))
			binary.LittleEndian.PutUint16(pcm[offset:offset+2], uint16(clampPCM16(scaled)))
		}
	}
}

func clampPCM16(value int) int16 {
	if value > math.MaxInt16 {
		return math.MaxInt16
	}
	if value < math.MinInt16 {
		return math.MinInt16
	}
	return int16(value)
}
