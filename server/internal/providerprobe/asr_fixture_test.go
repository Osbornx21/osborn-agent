package providerprobe

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadASROpusFixtureParsesHexAndBase64Frames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spoken-opus.json")
	if err := os.WriteFile(path, []byte(`{
		"format": "xiaozhi_opus_frames_v1",
		"sample_rate_hz": 16000,
		"frame_duration_ms": 60,
		"frames": [
			{"payload_hex": "f8fffe"},
			{"payload_base64": "+P/9"}
		]
	}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	frames, err := LoadASROpusFixture(path)
	if err != nil {
		t.Fatalf("LoadASROpusFixture() error = %v", err)
	}

	if len(frames) != 2 {
		t.Fatalf("frames len = %d, want 2", len(frames))
	}
	if got := len(frames[0]) + len(frames[1]); got != 6 {
		t.Fatalf("total bytes = %d, want 6", got)
	}
}

func TestLoadASROpusFixtureRejectsEmptyFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty-opus.json")
	if err := os.WriteFile(path, []byte(`{
		"format": "xiaozhi_opus_frames_v1",
		"sample_rate_hz": 16000,
		"frame_duration_ms": 60,
		"frames": []
	}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := LoadASROpusFixture(path); err == nil {
		t.Fatal("LoadASROpusFixture() error = nil, want empty fixture error")
	}
}

func TestWriteASROpusFixtureRoundTripsCapturedFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixtures", "asr", "spoken-opus.json")
	want := [][]byte{
		{0xf8, 0xff, 0xfe},
		{0xf8, 0xff, 0xfd},
	}

	if err := WriteASROpusFixture(path, want); err != nil {
		t.Fatalf("WriteASROpusFixture() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("fixture permissions = %o, want 0600", got)
	}

	got, err := LoadASROpusFixture(path)
	if err != nil {
		t.Fatalf("LoadASROpusFixture() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("frames len = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if !bytes.Equal(got[index], want[index]) {
			t.Fatalf("frame %d = %v, want %v", index, got[index], want[index])
		}
	}
}

func TestWriteASROpusFixtureRejectsEmptyPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spoken-opus.json")

	if err := WriteASROpusFixture(path, [][]byte{{0x01}, {}}); err == nil {
		t.Fatal("WriteASROpusFixture() error = nil, want empty payload error")
	}
}

func TestValidateASROpusFramesForSemanticProbeAcceptsVariedLongFixture(t *testing.T) {
	frames := variedASROpusFrames(12, 16)

	inspection, err := ValidateASROpusFramesForSemanticProbe(frames)
	if err != nil {
		t.Fatalf("ValidateASROpusFramesForSemanticProbe() error = %v", err)
	}
	if inspection.Frames != 12 || inspection.Bytes != 192 || inspection.DurationMS != 720 || inspection.UniquePayloads != 12 {
		t.Fatalf("inspection = %+v", inspection)
	}
}

func TestValidateASROpusFramesForSemanticProbeRejectsTooShortFixture(t *testing.T) {
	_, err := ValidateASROpusFramesForSemanticProbe(variedASROpusFrames(2, 16))
	if err == nil {
		t.Fatal("ValidateASROpusFramesForSemanticProbe() error = nil, want short fixture failure")
	}
}

func TestValidateASROpusFramesForSemanticProbeRejectsRepeatedPlaceholder(t *testing.T) {
	frames := make([][]byte, 12)
	for index := range frames {
		frames[index] = []byte{0xf8, 0xff, 0xfe}
	}

	_, err := ValidateASROpusFramesForSemanticProbe(frames)
	if err == nil {
		t.Fatal("ValidateASROpusFramesForSemanticProbe() error = nil, want repeated fixture failure")
	}
}

func variedASROpusFrames(count int, size int) [][]byte {
	frames := make([][]byte, count)
	for index := range frames {
		frame := make([]byte, size)
		for offset := range frame {
			frame[offset] = byte(0x20 + index + offset)
		}
		frames[index] = frame
	}
	return frames
}
