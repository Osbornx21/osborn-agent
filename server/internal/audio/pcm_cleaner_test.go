package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestPCM16StreamCleanerRemovesDCOffset(t *testing.T) {
	pcm := pcm16CleanFixture(1000, 1200, 800, 1000)
	before, err := AnalyzePCM16LE(pcm, PCM16AnalysisOptions{SampleRateHz: 1000, Channels: 1})
	if err != nil {
		t.Fatalf("AnalyzePCM16LE(before) error = %v", err)
	}
	if before.DCOffset < 0.02 {
		t.Fatalf("before DCOffset = %f, want visible positive bias", before.DCOffset)
	}

	cleaner := NewPCM16StreamCleaner(PCM16CleanerOptions{
		SampleRateHz: 1000,
		Channels:     1,
		FadeInMS:     -1,
		FadeOutMS:    -1,
		RemoveDC:     true,
	})
	if err := cleaner.CleanFrame(pcm, false); err != nil {
		t.Fatalf("CleanFrame() error = %v", err)
	}
	after, err := AnalyzePCM16LE(pcm, PCM16AnalysisOptions{SampleRateHz: 1000, Channels: 1})
	if err != nil {
		t.Fatalf("AnalyzePCM16LE(after) error = %v", err)
	}
	if math.Abs(after.DCOffset) > 0.001 {
		t.Fatalf("after DCOffset = %f, want near zero", after.DCOffset)
	}
}

func TestPCM16StreamCleanerAppliesFirstAndFinalFrameFade(t *testing.T) {
	first := pcm16CleanFixture(10000, 10000, 10000, 10000)
	second := pcm16CleanFixture(10000, 10000, 10000, 10000)
	cleaner := NewPCM16StreamCleaner(PCM16CleanerOptions{
		SampleRateHz: 1000,
		Channels:     1,
		FadeInMS:     2,
		FadeOutMS:    2,
	})
	if err := cleaner.CleanFrame(first, false); err != nil {
		t.Fatalf("CleanFrame(first) error = %v", err)
	}
	if got := pcm16Sample(first, 0); got >= pcm16Sample(first, 2) {
		t.Fatalf("first frame fade-in samples = %d then %d, want rising gain", got, pcm16Sample(first, 2))
	}
	if got := pcm16Sample(first, 3); got != 10000 {
		t.Fatalf("first frame final sample = %d, want untouched non-final tail", got)
	}

	if err := cleaner.CleanFrame(second, true); err != nil {
		t.Fatalf("CleanFrame(second) error = %v", err)
	}
	if got := pcm16Sample(second, 0); got != 10000 {
		t.Fatalf("second frame first sample = %d, want no repeated fade-in", got)
	}
	if got := pcm16Sample(second, 3); got >= pcm16Sample(second, 1) {
		t.Fatalf("final frame fade-out samples = %d vs %d, want falling gain", got, pcm16Sample(second, 1))
	}
}

func pcm16CleanFixture(samples ...int16) []byte {
	pcm := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(pcm[i*2:i*2+2], uint16(sample))
	}
	return pcm
}

func pcm16Sample(pcm []byte, index int) int16 {
	return int16(binary.LittleEndian.Uint16(pcm[index*2 : index*2+2]))
}
