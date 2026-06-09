package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseMacAudioDevicesFindsUSBMic(t *testing.T) {
	devices := parseMacAudioDevices(`Audio:

    Devices:

        ECM999U:

          Input Channels: 2
          Manufacturer: Superlux
          Current SampleRate: 48000
          Transport: USB
          Input Source: Default

        MacBook Pro麦克风:

          Default Input Device: Yes
          Input Channels: 1
          Manufacturer: Apple Inc.
          Current SampleRate: 44100
          Transport: Built-in
`)
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(devices))
	}
	ecm := devices[0]
	if ecm.Name != "ECM999U" || ecm.Manufacturer != "Superlux" || ecm.Transport != "USB" || ecm.InputChannels != 2 || ecm.CurrentSampleRate != 48000 {
		t.Fatalf("unexpected ECM device: %+v", ecm)
	}
	if !devices[1].DefaultInputDevice {
		t.Fatalf("built-in mic default input = false, want true")
	}
}

func TestAnalyzeAcousticWAVPCM16(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tone.wav")
	writeTestPCM16WAV(t, path, 48000, 1, 48000, func(i int) int16 {
		return int16(math.Sin(2*math.Pi*440*float64(i)/48000) * 12000)
	})

	metric, err := analyzeAcousticWAV(path, defaultAcousticSilenceDBFS)
	if err != nil {
		t.Fatalf("analyze wav: %v", err)
	}
	if metric.SampleRateHz != 48000 || metric.Channels != 1 || metric.BitsPerSample != 16 {
		t.Fatalf("bad wav metadata: %+v", metric)
	}
	if metric.DurationMS != 1000 {
		t.Fatalf("duration = %d, want 1000", metric.DurationMS)
	}
	if metric.PeakDBFS >= 0 || metric.RMSDBFS >= 0 {
		t.Fatalf("dbfs should be negative: peak=%f rms=%f", metric.PeakDBFS, metric.RMSDBFS)
	}
	if metric.ClippedPercent != 0 {
		t.Fatalf("clipped = %f, want 0", metric.ClippedPercent)
	}
}

func TestAcousticCaptureCommandAnalyzesInputWAV(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "input.wav")
	writeTestPCM16WAV(t, input, 48000, 1, 24000, func(i int) int16 {
		if i < 1200 {
			return 0
		}
		return 4000
	})
	outputDir := filepath.Join(tempDir, "out")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"acoustic-capture",
		"--input-wav", input,
		"--output-dir", outputDir,
		"--label", "ECM999U",
		"--scenario", "quiet_50cm_front",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	var manifest acousticCaptureManifest
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("parse manifest: %v\n%s", err, stdout.String())
	}
	if manifest.Schema != acousticCaptureSchema {
		t.Fatalf("schema = %q", manifest.Schema)
	}
	if manifest.CaptureTool != "input_wav" {
		t.Fatalf("capture tool = %q", manifest.CaptureTool)
	}
	if manifest.Audio.DurationMS != 500 {
		t.Fatalf("duration = %d, want 500", manifest.Audio.DurationMS)
	}
	manifestPath := filepath.Join(outputDir, filepath.Dir(manifest.AudioFile), "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest file missing: %v", err)
	}
}

func TestAcousticCaptureRejectsTrackedRepositoryOutputDir(t *testing.T) {
	tempDir := t.TempDir()
	input := filepath.Join(tempDir, "input.wav")
	writeTestPCM16WAV(t, input, 48000, 1, 48000, func(i int) int16 {
		return 1000
	})
	outputDir := "./not-ignored-acoustic-output"
	t.Cleanup(func() {
		_ = os.RemoveAll(outputDir)
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{
		"acoustic-capture",
		"--input-wav", input,
		"--output-dir", outputDir,
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatalf("code = 0, want failure for non-ignored repository output")
	}
	if !strings.Contains(stderr.String(), "must be ignored by git") {
		t.Fatalf("stderr = %q, want git ignore guidance", stderr.String())
	}
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("non-ignored output dir was created or stat failed: %v", err)
	}
}

func writeTestPCM16WAV(t *testing.T, path string, sampleRate int, channels int, frames int, sample func(int) int16) {
	t.Helper()
	var pcm bytes.Buffer
	for i := 0; i < frames*channels; i++ {
		if err := binary.Write(&pcm, binary.LittleEndian, sample(i/channels)); err != nil {
			t.Fatalf("write sample: %v", err)
		}
	}
	data := pcm.Bytes()
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(36+len(data)))
	wav.WriteString("WAVE")
	wav.WriteString("fmt ")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(16))
	_ = binary.Write(&wav, binary.LittleEndian, uint16(1))
	_ = binary.Write(&wav, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&wav, binary.LittleEndian, uint32(sampleRate))
	byteRate := sampleRate * channels * 2
	_ = binary.Write(&wav, binary.LittleEndian, uint32(byteRate))
	blockAlign := channels * 2
	_ = binary.Write(&wav, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&wav, binary.LittleEndian, uint16(16))
	wav.WriteString("data")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(len(data)))
	wav.Write(data)
	if err := os.WriteFile(path, wav.Bytes(), 0o600); err != nil {
		t.Fatalf("write wav: %v", err)
	}
}

func TestAcousticRunDirNameIsStableAndSafe(t *testing.T) {
	at := time.Date(2026, 6, 8, 1, 2, 3, 0, time.UTC)
	got := acousticRunDirName(at, "ECM 999U", "Quiet 50cm Front")
	want := "acoustic-20260608T010203Z-ecm-999u-quiet-50cm-front"
	if got != want {
		t.Fatalf("run dir = %q, want %q", got, want)
	}
}
