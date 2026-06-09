package providerprobe

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	asrOpusFixtureFormat          = "xiaozhi_opus_frames_v1"
	asrOpusFixtureSampleRateHz    = 16000
	asrOpusFixtureFrameDurationMS = 60

	minSemanticASRFixtureFrames         = 10
	minSemanticASRFixtureBytes          = 120
	minSemanticASRFixtureUniquePayloads = 4
)

type ASROpusFixtureInspection struct {
	Frames         int
	Bytes          int
	DurationMS     int
	UniquePayloads int
}

type asrOpusFixture struct {
	Format          string                `json:"format"`
	SampleRateHz    int                   `json:"sample_rate_hz"`
	FrameDurationMS int                   `json:"frame_duration_ms"`
	Frames          []asrOpusFixtureFrame `json:"frames"`
}

type asrOpusFixtureFrame struct {
	PayloadHex    string `json:"payload_hex"`
	PayloadBase64 string `json:"payload_base64"`
}

func LoadASROpusFixture(path string) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ASR Opus fixture: %w", err)
	}

	var fixture asrOpusFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return nil, fmt.Errorf("parse ASR Opus fixture JSON: %w", err)
	}
	if strings.TrimSpace(fixture.Format) != asrOpusFixtureFormat {
		return nil, fmt.Errorf("ASR Opus fixture format must be %s", asrOpusFixtureFormat)
	}
	if fixture.SampleRateHz != asrOpusFixtureSampleRateHz {
		return nil, fmt.Errorf("ASR Opus fixture sample_rate_hz must be %d", asrOpusFixtureSampleRateHz)
	}
	if fixture.FrameDurationMS != asrOpusFixtureFrameDurationMS {
		return nil, fmt.Errorf("ASR Opus fixture frame_duration_ms must be %d", asrOpusFixtureFrameDurationMS)
	}
	if len(fixture.Frames) == 0 {
		return nil, fmt.Errorf("ASR Opus fixture must include at least one frame")
	}

	frames := make([][]byte, 0, len(fixture.Frames))
	for index, frame := range fixture.Frames {
		payload, err := decodeASROpusFixtureFrame(frame)
		if err != nil {
			return nil, fmt.Errorf("decode ASR Opus fixture frame %d: %w", index, err)
		}
		if len(payload) == 0 {
			return nil, fmt.Errorf("ASR Opus fixture frame %d is empty", index)
		}
		frames = append(frames, payload)
	}
	return frames, nil
}

func InspectASROpusFrames(frames [][]byte) ASROpusFixtureInspection {
	uniquePayloads := map[string]struct{}{}
	totalBytes := 0
	for _, frame := range frames {
		totalBytes += len(frame)
		if len(frame) > 0 {
			uniquePayloads[hex.EncodeToString(frame)] = struct{}{}
		}
	}
	return ASROpusFixtureInspection{
		Frames:         len(frames),
		Bytes:          totalBytes,
		DurationMS:     len(frames) * asrOpusFixtureFrameDurationMS,
		UniquePayloads: len(uniquePayloads),
	}
}

func ValidateASROpusFixtureForSemanticProbe(path string) (ASROpusFixtureInspection, error) {
	frames, err := LoadASROpusFixture(path)
	if err != nil {
		return ASROpusFixtureInspection{}, err
	}
	return ValidateASROpusFramesForSemanticProbe(frames)
}

func ValidateASROpusFramesForSemanticProbe(frames [][]byte) (ASROpusFixtureInspection, error) {
	inspection := InspectASROpusFrames(frames)
	if inspection.Frames < minSemanticASRFixtureFrames {
		return inspection, fmt.Errorf("ASR Opus fixture is too short for semantic provider probes: frames=%d minimum=%d", inspection.Frames, minSemanticASRFixtureFrames)
	}
	if inspection.Bytes < minSemanticASRFixtureBytes {
		return inspection, fmt.Errorf("ASR Opus fixture is too small for semantic provider probes: bytes=%d minimum=%d", inspection.Bytes, minSemanticASRFixtureBytes)
	}
	if inspection.UniquePayloads < minSemanticASRFixtureUniquePayloads {
		return inspection, fmt.Errorf("ASR Opus fixture lacks payload diversity for semantic provider probes: unique_payloads=%d minimum=%d", inspection.UniquePayloads, minSemanticASRFixtureUniquePayloads)
	}
	if maxRepeatedASRFixturePayloadCount(frames)*100 > len(frames)*80 {
		return inspection, fmt.Errorf("ASR Opus fixture is dominated by repeated payloads and cannot prove spoken input")
	}
	return inspection, nil
}

func WriteASROpusFixture(path string, frames [][]byte) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("ASR Opus fixture path is required")
	}
	if len(frames) == 0 {
		return fmt.Errorf("ASR Opus fixture must include at least one frame")
	}

	fixture := asrOpusFixture{
		Format:          asrOpusFixtureFormat,
		SampleRateHz:    asrOpusFixtureSampleRateHz,
		FrameDurationMS: asrOpusFixtureFrameDurationMS,
		Frames:          make([]asrOpusFixtureFrame, 0, len(frames)),
	}
	for index, frame := range frames {
		if len(frame) == 0 {
			return fmt.Errorf("ASR Opus fixture frame %d is empty", index)
		}
		fixture.Frames = append(fixture.Frames, asrOpusFixtureFrame{
			PayloadBase64: base64.StdEncoding.EncodeToString(frame),
		})
	}

	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ASR Opus fixture JSON: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create ASR Opus fixture directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write ASR Opus fixture: %w", err)
	}
	return nil
}

func maxRepeatedASRFixturePayloadCount(frames [][]byte) int {
	counts := map[string]int{}
	maxCount := 0
	for _, frame := range frames {
		key := hex.EncodeToString(frame)
		counts[key]++
		if counts[key] > maxCount {
			maxCount = counts[key]
		}
	}
	return maxCount
}

func decodeASROpusFixtureFrame(frame asrOpusFixtureFrame) ([]byte, error) {
	payloadHex := strings.TrimSpace(frame.PayloadHex)
	payloadBase64 := strings.TrimSpace(frame.PayloadBase64)
	if payloadHex != "" && payloadBase64 != "" {
		return nil, fmt.Errorf("use payload_hex or payload_base64, not both")
	}
	if payloadHex == "" && payloadBase64 == "" {
		return nil, fmt.Errorf("payload_hex or payload_base64 is required")
	}
	if payloadHex != "" {
		payload, err := hex.DecodeString(payloadHex)
		if err != nil {
			return nil, err
		}
		return payload, nil
	}
	payload, err := base64.StdEncoding.DecodeString(payloadBase64)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
