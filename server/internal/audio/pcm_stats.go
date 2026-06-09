package audio

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	PCM16BitsPerSample               = 16
	DefaultPCM16SilenceThresholdDBFS = -50.0
	minPCM16DBFS                     = -120.0
	clippedPCM16ThresholdSample      = 32760
)

type PCM16AnalysisOptions struct {
	SampleRateHz         int
	Channels             int
	SilenceThresholdDBFS float64
}

type PCM16Stats struct {
	SampleRateHz         int     `json:"sample_rate_hz,omitempty"`
	Channels             int     `json:"channels,omitempty"`
	BitsPerSample        int     `json:"bits_per_sample,omitempty"`
	SampleCount          int64   `json:"sample_count,omitempty"`
	FrameCount           int64   `json:"frame_count,omitempty"`
	DurationMS           int     `json:"duration_ms,omitempty"`
	PeakDBFS             float64 `json:"peak_dbfs"`
	RMSDBFS              float64 `json:"rms_dbfs"`
	ClippedPercent       float64 `json:"clipped_percent"`
	SilencePercent       float64 `json:"silence_percent"`
	SilenceThresholdDBFS float64 `json:"silence_threshold_dbfs"`
	DCOffset             float64 `json:"dc_offset"`

	maxAbs         float64
	sumSquares     float64
	sumSamples     float64
	clippedSamples int64
	silentSamples  int64
}

func AnalyzePCM16LE(pcm []byte, options PCM16AnalysisOptions) (PCM16Stats, error) {
	accumulator := NewPCM16StatsAccumulator(options)
	if err := accumulator.AddPCM16LE(pcm); err != nil {
		return PCM16Stats{}, err
	}
	return accumulator.Snapshot(), nil
}

type PCM16StatsAccumulator struct {
	sampleRateHz         int
	channels             int
	silenceThresholdDBFS float64
	sampleCount          int64
	maxAbs               float64
	sumSquares           float64
	sumSamples           float64
	clippedSamples       int64
	silentSamples        int64
}

func NewPCM16StatsAccumulator(options PCM16AnalysisOptions) *PCM16StatsAccumulator {
	sampleRateHz := options.SampleRateHz
	if sampleRateHz <= 0 {
		sampleRateHz = DefaultDownlinkRateHz
	}
	channels := options.Channels
	if channels <= 0 {
		channels = DefaultChannels
	}
	silenceThresholdDBFS := options.SilenceThresholdDBFS
	if silenceThresholdDBFS == 0 {
		silenceThresholdDBFS = DefaultPCM16SilenceThresholdDBFS
	}
	return &PCM16StatsAccumulator{
		sampleRateHz:         sampleRateHz,
		channels:             channels,
		silenceThresholdDBFS: silenceThresholdDBFS,
	}
}

func (a *PCM16StatsAccumulator) AddPCM16LE(pcm []byte) error {
	if a == nil {
		return nil
	}
	if len(pcm)%2 != 0 {
		return fmt.Errorf("pcm16 data has odd byte length: %d", len(pcm))
	}
	threshold := math.Pow(10, a.silenceThresholdDBFS/20)
	for offset := 0; offset < len(pcm); offset += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcm[offset : offset+2]))
		normalized := float64(sample) / 32768.0
		abs := math.Abs(normalized)
		if abs > a.maxAbs {
			a.maxAbs = abs
		}
		if math.Abs(float64(sample)) >= clippedPCM16ThresholdSample {
			a.clippedSamples++
		}
		if abs <= threshold {
			a.silentSamples++
		}
		a.sumSamples += normalized
		a.sumSquares += abs * abs
		a.sampleCount++
	}
	return nil
}

func (a *PCM16StatsAccumulator) Add(stats PCM16Stats) {
	if a == nil || stats.SampleCount <= 0 {
		return
	}
	if a.sampleRateHz <= 0 && stats.SampleRateHz > 0 {
		a.sampleRateHz = stats.SampleRateHz
	}
	if a.channels <= 0 && stats.Channels > 0 {
		a.channels = stats.Channels
	}
	if stats.SilenceThresholdDBFS != 0 {
		a.silenceThresholdDBFS = stats.SilenceThresholdDBFS
	}
	if stats.maxAbs > a.maxAbs {
		a.maxAbs = stats.maxAbs
	}
	a.sumSquares += stats.sumSquares
	a.sumSamples += stats.sumSamples
	a.clippedSamples += stats.clippedSamples
	a.silentSamples += stats.silentSamples
	a.sampleCount += stats.SampleCount
}

func (a *PCM16StatsAccumulator) Snapshot() PCM16Stats {
	if a == nil {
		return PCM16Stats{}
	}
	sampleRateHz := a.sampleRateHz
	if sampleRateHz <= 0 {
		sampleRateHz = DefaultDownlinkRateHz
	}
	channels := a.channels
	if channels <= 0 {
		channels = DefaultChannels
	}
	if a.sampleCount <= 0 {
		return PCM16Stats{
			SampleRateHz:         sampleRateHz,
			Channels:             channels,
			BitsPerSample:        PCM16BitsPerSample,
			PeakDBFS:             minPCM16DBFS,
			RMSDBFS:              minPCM16DBFS,
			SilenceThresholdDBFS: a.silenceThresholdDBFS,
		}
	}
	count := float64(a.sampleCount)
	frameCount := a.sampleCount / int64(channels)
	durationMS := 0
	if sampleRateHz > 0 {
		durationMS = int((frameCount * 1000) / int64(sampleRateHz))
	}
	rms := math.Sqrt(a.sumSquares / count)
	return PCM16Stats{
		SampleRateHz:         sampleRateHz,
		Channels:             channels,
		BitsPerSample:        PCM16BitsPerSample,
		SampleCount:          a.sampleCount,
		FrameCount:           frameCount,
		DurationMS:           durationMS,
		PeakDBFS:             roundPCM16Float(dbfsPCM16(a.maxAbs), 2),
		RMSDBFS:              roundPCM16Float(dbfsPCM16(rms), 2),
		ClippedPercent:       roundPCM16Float(float64(a.clippedSamples)*100/count, 3),
		SilencePercent:       roundPCM16Float(float64(a.silentSamples)*100/count, 3),
		SilenceThresholdDBFS: a.silenceThresholdDBFS,
		DCOffset:             roundPCM16Float(a.sumSamples/count, 6),
		maxAbs:               a.maxAbs,
		sumSquares:           a.sumSquares,
		sumSamples:           a.sumSamples,
		clippedSamples:       a.clippedSamples,
		silentSamples:        a.silentSamples,
	}
}

func (s PCM16Stats) HasSamples() bool {
	return s.SampleCount > 0
}

func (s PCM16Stats) TraceFields() map[string]any {
	return map[string]any{
		"sample_rate_hz":         s.SampleRateHz,
		"channels":               s.Channels,
		"bits_per_sample":        s.BitsPerSample,
		"sample_count":           s.SampleCount,
		"frame_count":            s.FrameCount,
		"duration_ms":            s.DurationMS,
		"peak_dbfs":              s.PeakDBFS,
		"rms_dbfs":               s.RMSDBFS,
		"clipped_percent":        s.ClippedPercent,
		"silence_percent":        s.SilencePercent,
		"silence_threshold_dbfs": s.SilenceThresholdDBFS,
		"dc_offset":              s.DCOffset,
	}
}

func dbfsPCM16(value float64) float64 {
	if value <= 0 {
		return minPCM16DBFS
	}
	result := 20 * math.Log10(value)
	if result < minPCM16DBFS {
		return minPCM16DBFS
	}
	if result > 0 {
		return 0
	}
	return result
}

func roundPCM16Float(value float64, places int) float64 {
	scale := math.Pow10(places)
	return math.Round(value*scale) / scale
}
