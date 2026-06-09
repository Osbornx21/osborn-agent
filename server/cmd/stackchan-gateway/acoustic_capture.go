package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"stackchan-gateway/internal/audio"
)

const (
	acousticCaptureSchema      = "a21_air_acoustic_capture_v1"
	defaultAcousticOutputDir   = "./var/runtime/acoustic"
	defaultAcousticDurationMS  = 10000
	defaultAcousticSampleRate  = 48000
	defaultAcousticChannels    = 1
	defaultAcousticDeviceName  = "ECM999U"
	defaultAcousticSilenceDBFS = -50.0
)

type acousticDevice struct {
	Name               string `json:"name"`
	Manufacturer       string `json:"manufacturer,omitempty"`
	Transport          string `json:"transport,omitempty"`
	InputChannels      int    `json:"input_channels,omitempty"`
	OutputChannels     int    `json:"output_channels,omitempty"`
	CurrentSampleRate  int    `json:"current_sample_rate,omitempty"`
	DefaultInputDevice bool   `json:"default_input_device,omitempty"`
	DefaultOutput      bool   `json:"default_output_device,omitempty"`
}

type acousticDeviceCatalog struct {
	Count       int              `json:"count"`
	GeneratedAt string           `json:"generated_at"`
	Devices     []acousticDevice `json:"devices"`
}

type acousticCaptureManifest struct {
	Schema              string              `json:"schema"`
	GeneratedAt         string              `json:"generated_at"`
	Scenario            string              `json:"scenario"`
	Label               string              `json:"label"`
	DeviceName          string              `json:"device_name,omitempty"`
	RequestedDurationMS int                 `json:"requested_duration_ms"`
	CaptureTool         string              `json:"capture_tool"`
	FFmpegInput         string              `json:"ffmpeg_input,omitempty"`
	AudioFile           string              `json:"audio_file"`
	Audio               acousticAudioMetric `json:"audio"`
	Notes               []string            `json:"notes,omitempty"`
}

type acousticAudioMetric struct {
	SampleRateHz     int     `json:"sample_rate_hz"`
	Channels         int     `json:"channels"`
	BitsPerSample    int     `json:"bits_per_sample"`
	DurationMS       int     `json:"duration_ms"`
	FrameCount       int64   `json:"frame_count"`
	PeakDBFS         float64 `json:"peak_dbfs"`
	RMSDBFS          float64 `json:"rms_dbfs"`
	ClippedPercent   float64 `json:"clipped_percent"`
	SilencePercent   float64 `json:"silence_percent"`
	SilenceThreshold float64 `json:"silence_threshold_dbfs"`
	DCOffset         float64 `json:"dc_offset"`
}

type wavInfo struct {
	SampleRateHz  int
	Channels      int
	BitsPerSample int
	FrameCount    int64
	PCM           []byte
}

func runAcousticDevices(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("acoustic-devices", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	devices, err := listMacAudioDevices(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "acoustic-devices failed: %v\n", err)
		return 1
	}
	catalog := acousticDeviceCatalog{
		Count:       len(devices),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Devices:     devices,
	}
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "acoustic-devices failed: marshal catalog: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

func runAcousticCapture(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("acoustic-capture", flag.ContinueOnError)
	flags.SetOutput(stderr)
	outputDir := flags.String("output-dir", defaultAcousticOutputDir, "directory for acoustic capture runs")
	inputWAV := flags.String("input-wav", "", "optional existing wav file to analyze instead of recording")
	durationMS := flags.Int("duration-ms", defaultAcousticDurationMS, "recording duration in milliseconds")
	scenario := flags.String("scenario", "baseline", "short scenario id, such as quiet_1m_front")
	label := flags.String("label", "ecm999u", "short capture label used in the run directory name")
	deviceName := flags.String("device-name", defaultAcousticDeviceName, "expected USB microphone device name")
	ffmpegPath := flags.String("ffmpeg", "ffmpeg", "ffmpeg executable path")
	ffmpegInput := flags.String("ffmpeg-input", "", "ffmpeg avfoundation input, for example :ECM999U or :0")
	sampleRate := flags.Int("sample-rate", defaultAcousticSampleRate, "target capture sample rate")
	channels := flags.Int("channels", defaultAcousticChannels, "target capture channels")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	manifest, err := buildAcousticCapture(acousticCaptureOptions{
		OutputDir:   *outputDir,
		InputWAV:    *inputWAV,
		DurationMS:  *durationMS,
		Scenario:    *scenario,
		Label:       *label,
		DeviceName:  *deviceName,
		FFmpegPath:  *ffmpegPath,
		FFmpegInput: *ffmpegInput,
		SampleRate:  *sampleRate,
		Channels:    *channels,
		Now:         time.Now,
	})
	if err != nil {
		fmt.Fprintf(stderr, "acoustic-capture failed: %v\n", err)
		return 1
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "acoustic-capture failed: marshal manifest: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

type acousticCaptureOptions struct {
	OutputDir   string
	InputWAV    string
	DurationMS  int
	Scenario    string
	Label       string
	DeviceName  string
	FFmpegPath  string
	FFmpegInput string
	SampleRate  int
	Channels    int
	Now         func() time.Time
}

func buildAcousticCapture(options acousticCaptureOptions) (acousticCaptureManifest, error) {
	options = normalizeAcousticCaptureOptions(options)
	if options.DurationMS <= 0 || options.DurationMS > 300000 {
		return acousticCaptureManifest{}, errors.New("--duration-ms must be between 1 and 300000")
	}
	if options.SampleRate <= 0 {
		return acousticCaptureManifest{}, errors.New("--sample-rate must be positive")
	}
	if options.Channels <= 0 || options.Channels > 2 {
		return acousticCaptureManifest{}, errors.New("--channels must be 1 or 2")
	}
	if err := ensureAcousticOutputPathIgnored(options.OutputDir); err != nil {
		return acousticCaptureManifest{}, err
	}
	if err := os.MkdirAll(options.OutputDir, 0o700); err != nil {
		return acousticCaptureManifest{}, fmt.Errorf("create output dir: %w", err)
	}
	runDir := filepath.Join(options.OutputDir, acousticRunDirName(options.Now(), options.Label, options.Scenario))
	if err := os.Mkdir(runDir, 0o700); err != nil {
		return acousticCaptureManifest{}, fmt.Errorf("create run dir: %w", err)
	}
	wavPath := filepath.Join(runDir, "capture.wav")
	var notes []string
	captureTool := "input_wav"
	if strings.TrimSpace(options.InputWAV) != "" {
		if err := copyFile(options.InputWAV, wavPath); err != nil {
			return acousticCaptureManifest{}, fmt.Errorf("copy input wav: %w", err)
		}
		notes = append(notes, "analyzed_existing_wav")
	} else {
		captureTool = "ffmpeg_avfoundation"
		input := strings.TrimSpace(options.FFmpegInput)
		if input == "" {
			input = ":" + strings.TrimSpace(options.DeviceName)
		}
		if err := captureAcousticWAV(options, input, wavPath); err != nil {
			return acousticCaptureManifest{}, err
		}
		options.FFmpegInput = input
	}
	metric, err := analyzeAcousticWAV(wavPath, defaultAcousticSilenceDBFS)
	if err != nil {
		return acousticCaptureManifest{}, err
	}
	manifest := acousticCaptureManifest{
		Schema:              acousticCaptureSchema,
		GeneratedAt:         options.Now().UTC().Format(time.RFC3339),
		Scenario:            sanitizeAcousticToken(options.Scenario, "baseline"),
		Label:               sanitizeAcousticToken(options.Label, "capture"),
		DeviceName:          strings.TrimSpace(options.DeviceName),
		RequestedDurationMS: options.DurationMS,
		CaptureTool:         captureTool,
		FFmpegInput:         strings.TrimSpace(options.FFmpegInput),
		AudioFile:           filepath.ToSlash(filepath.Join(filepath.Base(runDir), "capture.wav")),
		Audio:               metric,
		Notes:               notes,
	}
	if err := writeAcousticManifest(filepath.Join(runDir, "manifest.json"), manifest); err != nil {
		return acousticCaptureManifest{}, err
	}
	return manifest, nil
}

func normalizeAcousticCaptureOptions(options acousticCaptureOptions) acousticCaptureOptions {
	if strings.TrimSpace(options.OutputDir) == "" {
		options.OutputDir = defaultAcousticOutputDir
	}
	if strings.TrimSpace(options.Scenario) == "" {
		options.Scenario = "baseline"
	}
	if strings.TrimSpace(options.Label) == "" {
		options.Label = "capture"
	}
	if strings.TrimSpace(options.DeviceName) == "" {
		options.DeviceName = defaultAcousticDeviceName
	}
	if strings.TrimSpace(options.FFmpegPath) == "" {
		options.FFmpegPath = "ffmpeg"
	}
	if options.DurationMS <= 0 {
		options.DurationMS = defaultAcousticDurationMS
	}
	if options.SampleRate <= 0 {
		options.SampleRate = defaultAcousticSampleRate
	}
	if options.Channels <= 0 {
		options.Channels = defaultAcousticChannels
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return options
}

func captureAcousticWAV(options acousticCaptureOptions, ffmpegInput string, wavPath string) error {
	ffmpegPath, err := exec.LookPath(options.FFmpegPath)
	if err != nil {
		return fmt.Errorf("ffmpeg not found: install ffmpeg or pass --input-wav for analysis-only mode")
	}
	duration := time.Duration(options.DurationMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), duration+15*time.Second)
	defer cancel()
	args := []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-f", "avfoundation",
		"-i", ffmpegInput,
		"-t", fmt.Sprintf("%.3f", duration.Seconds()),
		"-ac", fmt.Sprintf("%d", options.Channels),
		"-ar", fmt.Sprintf("%d", options.SampleRate),
		"-sample_fmt", "s16",
		"-y", wavPath,
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ffmpeg capture timed out using input %q; macOS may be waiting for microphone permission for Codex/Terminal, or the avfoundation input is wrong", ffmpegInput)
		}
		return fmt.Errorf("ffmpeg capture failed using input %q: %s", ffmpegInput, limitAcousticError(stderr.String()))
	}
	return nil
}

func ensureAcousticOutputPathIgnored(outputDir string) error {
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("resolve output dir: %w", err)
	}
	rootData, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil
	}
	root := strings.TrimSpace(string(rootData))
	if root == "" {
		return nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(absRoot, absOutput)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return nil
	}
	if err := exec.Command("git", "-C", absRoot, "check-ignore", "-q", "--", rel).Run(); err == nil {
		return nil
	}
	return fmt.Errorf("--output-dir %q must be ignored by git or outside the repository because acoustic captures contain raw audio", outputDir)
}

func listMacAudioDevices(ctx context.Context) ([]acousticDevice, error) {
	cmd := exec.CommandContext(ctx, "system_profiler", "SPAudioDataType")
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("system_profiler SPAudioDataType: %w", err)
	}
	return parseMacAudioDevices(string(data)), nil
}

func parseMacAudioDevices(output string) []acousticDevice {
	var devices []acousticDevice
	var current *acousticDevice
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(line, "        ") && !strings.HasPrefix(line, "          ") && strings.HasSuffix(trimmed, ":") {
			if current != nil {
				devices = append(devices, *current)
			}
			current = &acousticDevice{Name: strings.TrimSuffix(trimmed, ":")}
			continue
		}
		if current == nil {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "Manufacturer":
			current.Manufacturer = value
		case "Transport":
			current.Transport = value
		case "Input Channels":
			current.InputChannels = atoiSafe(value)
		case "Output Channels":
			current.OutputChannels = atoiSafe(value)
		case "Current SampleRate":
			current.CurrentSampleRate = atoiSafe(value)
		case "Default Input Device":
			current.DefaultInputDevice = strings.EqualFold(value, "Yes")
		case "Default Output Device":
			current.DefaultOutput = strings.EqualFold(value, "Yes")
		}
	}
	if current != nil {
		devices = append(devices, *current)
	}
	return devices
}

func analyzeAcousticWAV(path string, silenceThresholdDBFS float64) (acousticAudioMetric, error) {
	info, err := readPCM16WAV(path)
	if err != nil {
		return acousticAudioMetric{}, err
	}
	if len(info.PCM) == 0 || info.SampleRateHz <= 0 || info.Channels <= 0 {
		return acousticAudioMetric{}, fmt.Errorf("wav has no analyzable samples")
	}
	stats, err := audio.AnalyzePCM16LE(info.PCM, audio.PCM16AnalysisOptions{
		SampleRateHz:         info.SampleRateHz,
		Channels:             info.Channels,
		SilenceThresholdDBFS: silenceThresholdDBFS,
	})
	if err != nil {
		return acousticAudioMetric{}, err
	}
	return acousticAudioMetric{
		SampleRateHz:     info.SampleRateHz,
		Channels:         info.Channels,
		BitsPerSample:    info.BitsPerSample,
		DurationMS:       stats.DurationMS,
		FrameCount:       info.FrameCount,
		PeakDBFS:         stats.PeakDBFS,
		RMSDBFS:          stats.RMSDBFS,
		ClippedPercent:   stats.ClippedPercent,
		SilencePercent:   stats.SilencePercent,
		SilenceThreshold: silenceThresholdDBFS,
		DCOffset:         stats.DCOffset,
	}, nil
}

func readPCM16WAV(path string) (wavInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return wavInfo{}, fmt.Errorf("read wav: %w", err)
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return wavInfo{}, fmt.Errorf("wav must be RIFF/WAVE")
	}
	var (
		channels      int
		sampleRate    int
		bitsPerSample int
		audioFormat   uint16
		pcmData       []byte
	)
	for offset := 12; offset+8 <= len(data); {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(data) {
			return wavInfo{}, fmt.Errorf("wav chunk %q exceeds file size", chunkID)
		}
		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return wavInfo{}, fmt.Errorf("wav fmt chunk too small")
			}
			audioFormat = binary.LittleEndian.Uint16(data[chunkStart : chunkStart+2])
			channels = int(binary.LittleEndian.Uint16(data[chunkStart+2 : chunkStart+4]))
			sampleRate = int(binary.LittleEndian.Uint32(data[chunkStart+4 : chunkStart+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[chunkStart+14 : chunkStart+16]))
		case "data":
			pcmData = data[chunkStart:chunkEnd]
		}
		offset = chunkEnd
		if offset%2 == 1 {
			offset++
		}
	}
	if audioFormat != 1 || bitsPerSample != 16 {
		return wavInfo{}, fmt.Errorf("wav must be PCM 16-bit, got format=%d bits=%d", audioFormat, bitsPerSample)
	}
	if channels <= 0 || sampleRate <= 0 || len(pcmData)%2 != 0 {
		return wavInfo{}, fmt.Errorf("wav has invalid PCM metadata")
	}
	pcm := make([]byte, len(pcmData))
	copy(pcm, pcmData)
	frameCount := int64(len(pcmData) / 2 / channels)
	return wavInfo{
		SampleRateHz:  sampleRate,
		Channels:      channels,
		BitsPerSample: bitsPerSample,
		FrameCount:    frameCount,
		PCM:           pcm,
	}, nil
}

func writeAcousticManifest(path string, manifest acousticCaptureManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func copyFile(from string, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func acousticRunDirName(at time.Time, label string, scenario string) string {
	return "acoustic-" + at.UTC().Format("20060102T150405Z") + "-" + sanitizeAcousticToken(label, "capture") + "-" + sanitizeAcousticToken(scenario, "baseline")
}

var acousticTokenPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sanitizeAcousticToken(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = acousticTokenPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-_")
	if value == "" {
		return fallback
	}
	if len(value) > 48 {
		value = strings.Trim(value[:48], "-_")
	}
	if value == "" {
		return fallback
	}
	return value
}

func atoiSafe(value string) int {
	var out int
	_, _ = fmt.Sscanf(strings.TrimSpace(value), "%d", &out)
	return out
}

func limitAcousticError(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "no ffmpeg error output"
	}
	if len(value) <= 800 {
		return value
	}
	return value[:800] + "..."
}
