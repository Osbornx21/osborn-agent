package audio

import (
	"encoding/binary"
	"testing"
)

func TestAnalyzePCM16LEReportsClippingSilenceAndDCOffset(t *testing.T) {
	pcm := pcm16LE(0, 0, 32767, -32768, 12000, 12000)

	stats, err := AnalyzePCM16LE(pcm, PCM16AnalysisOptions{
		SampleRateHz:         1000,
		Channels:             1,
		SilenceThresholdDBFS: -50,
	})
	if err != nil {
		t.Fatalf("AnalyzePCM16LE() error = %v", err)
	}
	if stats.SampleRateHz != 1000 || stats.Channels != 1 || stats.BitsPerSample != 16 {
		t.Fatalf("metadata = %+v, want pcm16 mono 1000Hz", stats)
	}
	if stats.SampleCount != 6 || stats.FrameCount != 6 || stats.DurationMS != 6 {
		t.Fatalf("count/duration = %+v, want 6 samples and 6ms", stats)
	}
	if stats.PeakDBFS != 0 {
		t.Fatalf("peak_dbfs = %f, want 0", stats.PeakDBFS)
	}
	if stats.ClippedPercent != 33.333 {
		t.Fatalf("clipped_percent = %f, want 33.333", stats.ClippedPercent)
	}
	if stats.SilencePercent != 33.333 {
		t.Fatalf("silence_percent = %f, want 33.333", stats.SilencePercent)
	}
	if stats.DCOffset <= 0.1 {
		t.Fatalf("dc_offset = %f, want positive bias", stats.DCOffset)
	}
}

func TestPCM16StatsAccumulatorAggregatesFrameStats(t *testing.T) {
	first, err := AnalyzePCM16LE(pcm16LE(0, -32768), PCM16AnalysisOptions{SampleRateHz: 1000, Channels: 1, SilenceThresholdDBFS: -50})
	if err != nil {
		t.Fatalf("first AnalyzePCM16LE() error = %v", err)
	}
	second, err := AnalyzePCM16LE(pcm16LE(32767, 32767), PCM16AnalysisOptions{SampleRateHz: 1000, Channels: 1, SilenceThresholdDBFS: -50})
	if err != nil {
		t.Fatalf("second AnalyzePCM16LE() error = %v", err)
	}
	accumulator := NewPCM16StatsAccumulator(PCM16AnalysisOptions{SampleRateHz: 1000, Channels: 1, SilenceThresholdDBFS: -50})
	accumulator.Add(first)
	accumulator.Add(second)

	stats := accumulator.Snapshot()
	if stats.SampleCount != 4 || stats.FrameCount != 4 {
		t.Fatalf("aggregated count = %+v, want 4 samples", stats)
	}
	if stats.PeakDBFS != 0 {
		t.Fatalf("aggregated peak_dbfs = %f, want 0", stats.PeakDBFS)
	}
	if stats.ClippedPercent != 75 {
		t.Fatalf("aggregated clipped_percent = %f, want 75", stats.ClippedPercent)
	}
	if stats.SilencePercent != 25 {
		t.Fatalf("aggregated silence_percent = %f, want 25", stats.SilencePercent)
	}
}

func TestAnalyzePCM16LERejectsOddByteInput(t *testing.T) {
	if _, err := AnalyzePCM16LE([]byte{0x01}, PCM16AnalysisOptions{}); err == nil {
		t.Fatal("AnalyzePCM16LE() error = nil, want odd byte error")
	}
}

func pcm16LE(samples ...int16) []byte {
	pcm := make([]byte, len(samples)*2)
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(pcm[index*2:], uint16(sample))
	}
	return pcm
}
