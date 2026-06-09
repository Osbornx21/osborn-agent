package providerprobe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"stackchan-gateway/internal/providers"
)

var (
	unsafeReportKeys = map[string]struct{}{
		"api_key":         {},
		"apikey":          {},
		"access_key":      {},
		"audio":           {},
		"audio_payload":   {},
		"authorization":   {},
		"bearer":          {},
		"body":            {},
		"frames":          {},
		"generated_text":  {},
		"headers":         {},
		"input_text":      {},
		"payload":         {},
		"payload_base64":  {},
		"payload_hex":     {},
		"prompt":          {},
		"prompt_text":     {},
		"raw":             {},
		"raw_body":        {},
		"raw_payload":     {},
		"raw_request":     {},
		"raw_response":    {},
		"request":         {},
		"response":        {},
		"secret":          {},
		"secret_key":      {},
		"signed_url":      {},
		"text":            {},
		"token":           {},
		"transcript":      {},
		"transcript_text": {},
		"url":             {},
	}
	secretLikeReportValues = []*regexp.Regexp{
		regexp.MustCompile(`(?i)authorization\s*:\s*bearer\s+[A-Za-z0-9._~+/=-]{8,}`),
		regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`),
		regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		regexp.MustCompile(`LTAI[0-9A-Za-z]{12,}`),
		regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`),
		regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]{20,}`),
		regexp.MustCompile(`-----BEGIN (RSA |OPENSSH |EC )?PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?key|secret[_-]?key|authorization)\s*[:=]`),
		regexp.MustCompile(`(?i)(X-Amz-Signature|OSSAccessKeyId|Signature)=`),
	}
)

func ValidateReportFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("provider probe report path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read provider probe report: %w", err)
	}
	return ValidateReportJSON(data)
}

func ValidateReportJSON(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("provider probe report is empty")
	}

	var raw any
	rawDecoder := json.NewDecoder(bytes.NewReader(data))
	rawDecoder.UseNumber()
	if err := rawDecoder.Decode(&raw); err != nil {
		return fmt.Errorf("decode provider probe report JSON: %w", err)
	}
	if err := ensureNoTrailingJSON(rawDecoder); err != nil {
		return err
	}
	if err := validateReportRaw(raw, "$"); err != nil {
		return err
	}

	var report Report
	typedDecoder := json.NewDecoder(bytes.NewReader(data))
	typedDecoder.DisallowUnknownFields()
	if err := typedDecoder.Decode(&report); err != nil {
		return fmt.Errorf("decode provider probe report schema: %w", err)
	}
	if err := ensureNoTrailingJSON(typedDecoder); err != nil {
		return err
	}
	return validateReport(report)
}

func ensureNoTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("provider probe report contains trailing JSON")
	} else if !errorsIsEOF(err) {
		return fmt.Errorf("decode provider probe report JSON: %w", err)
	}
	return nil
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}

func validateReportRaw(value any, path string) error {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			normalizedKey := strings.ToLower(strings.TrimSpace(key))
			if _, unsafe := unsafeReportKeys[normalizedKey]; unsafe {
				return fmt.Errorf("provider probe report contains unsafe field %q at %s", key, path)
			}
			if err := validateReportRaw(child, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range current {
			if err := validateReportRaw(child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case string:
		if err := validateSafeReportString(current, path); err != nil {
			return err
		}
	}
	return nil
}

func validateSafeReportString(value string, path string) error {
	if len(value) > 2048 {
		return fmt.Errorf("provider probe report string at %s is too long for a sanitized artifact", path)
	}
	for _, pattern := range secretLikeReportValues {
		if pattern.MatchString(value) {
			return fmt.Errorf("provider probe report contains secret-like value at %s", path)
		}
	}
	return nil
}

func validateReport(report Report) error {
	if strings.TrimSpace(report.Profile) == "" {
		return fmt.Errorf("provider probe report profile is required")
	}
	if report.Runs <= 0 {
		return fmt.Errorf("provider probe report runs must be positive")
	}
	if report.TimeoutMS <= 0 {
		return fmt.Errorf("provider probe report timeout_ms must be positive")
	}
	if report.RunDelayMS < 0 {
		return fmt.Errorf("provider probe report run_delay_ms must not be negative")
	}
	if report.PromptTextBytes <= 0 {
		return fmt.Errorf("provider probe report prompt_text_bytes must be positive")
	}
	if report.StartedAtUnixMS <= 0 || report.FinishedUnixMS <= 0 || report.FinishedUnixMS < report.StartedAtUnixMS {
		return fmt.Errorf("provider probe report timestamps are invalid")
	}
	if len(report.Results) == 0 {
		return fmt.Errorf("provider probe report results are required")
	}
	if len(report.Summaries) == 0 {
		return fmt.Errorf("provider probe report summaries are required")
	}
	if report.Successes < 0 || report.Failures < 0 || report.Successes+report.Failures != len(report.Results) {
		return fmt.Errorf("provider probe report success/failure counts do not match results")
	}

	counts := map[string]summaryCount{}
	actualSuccesses := 0
	actualFailures := 0
	for index, result := range report.Results {
		if err := validateRunResult(report, result, index); err != nil {
			return err
		}
		key := summaryKey(result.ProviderID, result.Modality)
		count := counts[key]
		count.runs++
		if result.Result.OK {
			count.successes++
			actualSuccesses++
		} else {
			count.failures++
			actualFailures++
		}
		counts[key] = count
	}
	if actualSuccesses != report.Successes || actualFailures != report.Failures {
		return fmt.Errorf("provider probe report success/failure totals do not match result ok flags")
	}
	for index, summary := range report.Summaries {
		if err := validateSummary(summary, counts, index); err != nil {
			return err
		}
	}
	for index, skipped := range report.Skipped {
		if err := validateSkipped(skipped, index); err != nil {
			return err
		}
	}
	return nil
}

type summaryCount struct {
	runs      int
	successes int
	failures  int
}

func validateRunResult(report Report, result RunResult, index int) error {
	if result.Run <= 0 || result.Run > report.Runs {
		return fmt.Errorf("provider probe result %d has invalid run number", index)
	}
	if err := validateReportID("provider_id", result.ProviderID, index); err != nil {
		return err
	}
	if !isSupportedModality(result.Modality) {
		return fmt.Errorf("provider probe result %d has invalid modality", index)
	}
	if result.Result.ProviderID != result.ProviderID || result.Result.Modality != result.Modality {
		return fmt.Errorf("provider probe result %d nested provider/modality does not match wrapper", index)
	}
	if result.Result.StartedAtUnixMS <= 0 || result.Result.FinishedAtUnixMS <= 0 || result.Result.FinishedAtUnixMS < result.Result.StartedAtUnixMS {
		return fmt.Errorf("provider probe result %d has invalid timestamps", index)
	}
	if result.Result.TotalMS < 0 ||
		result.Result.FirstTranscriptMS < 0 ||
		result.Result.FirstTokenMS < 0 ||
		result.Result.FirstAudioMS < 0 ||
		result.Result.TranscriptTextBytes < 0 ||
		result.Result.InputAudioFrames < 0 ||
		result.Result.InputAudioBytes < 0 ||
		result.Result.OutputTextBytes < 0 ||
		result.Result.AudioFrames < 0 ||
		result.Result.AudioBytes < 0 {
		return fmt.Errorf("provider probe result %d has negative latency or count", index)
	}
	if result.Result.ProviderHTTPStatus != 0 &&
		(result.Result.ProviderHTTPStatus < 100 || result.Result.ProviderHTTPStatus > 599) {
		return fmt.Errorf("provider probe result %d has invalid provider_http_status", index)
	}
	if err := validateProviderErrorCode(result.Result.ProviderErrorCode, index); err != nil {
		return err
	}
	if result.Result.OK {
		if result.ErrorClass != "" ||
			result.Result.ProviderError != "" ||
			result.Result.ProviderHTTPStatus != 0 ||
			result.Result.ProviderErrorCode != "" {
			return fmt.Errorf("provider probe result %d is ok but carries an error", index)
		}
		return validateSuccessfulResult(result, index)
	}
	if result.ErrorClass == "" {
		return fmt.Errorf("provider probe result %d failed without safe error_class", index)
	}
	if !isSafeErrorClass(result.ErrorClass) {
		return fmt.Errorf("provider probe result %d has unsafe error_class", index)
	}
	return nil
}

func validateProviderErrorCode(code string, index int) error {
	if code == "" {
		return nil
	}
	if len(code) > 128 {
		return fmt.Errorf("provider probe result %d has provider_error_code longer than 128 bytes", index)
	}
	for _, char := range code {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '.' ||
			char == '_' ||
			char == ':' ||
			char == '-' {
			continue
		}
		return fmt.Errorf("provider probe result %d has unsafe provider_error_code", index)
	}
	return nil
}

func validateSuccessfulResult(result RunResult, index int) error {
	switch result.Modality {
	case providers.ProbeModalityASR:
		if result.Result.FirstTranscriptMS <= 0 || result.Result.TranscriptTextBytes <= 0 {
			return fmt.Errorf("provider probe result %d successful ASR lacks transcript latency or length", index)
		}
		if result.Result.InputAudioFrames <= 0 || result.Result.InputAudioBytes <= 0 {
			return fmt.Errorf("provider probe result %d successful ASR lacks input audio counts", index)
		}
	case providers.ProbeModalityLLM:
		if result.Result.FirstTokenMS <= 0 || result.Result.OutputTextBytes <= 0 {
			return fmt.Errorf("provider probe result %d successful LLM lacks first token latency or output length", index)
		}
	case providers.ProbeModalityTTS:
		if result.Result.FirstAudioMS <= 0 || result.Result.AudioFrames <= 0 || result.Result.AudioBytes <= 0 {
			return fmt.Errorf("provider probe result %d successful TTS lacks first audio latency or audio counts", index)
		}
	}
	return nil
}

func validateSummary(summary Summary, counts map[string]summaryCount, index int) error {
	if err := validateReportID("summary provider_id", summary.ProviderID, index); err != nil {
		return err
	}
	if !isSupportedModality(summary.Modality) {
		return fmt.Errorf("provider probe summary %d has invalid modality", index)
	}
	if summary.Runs <= 0 || summary.Successes < 0 || summary.Failures < 0 || summary.Successes+summary.Failures != summary.Runs {
		return fmt.Errorf("provider probe summary %d has invalid run counts", index)
	}
	count, ok := counts[summaryKey(summary.ProviderID, summary.Modality)]
	if !ok {
		return fmt.Errorf("provider probe summary %d has no matching results", index)
	}
	if count.runs != summary.Runs || count.successes != summary.Successes || count.failures != summary.Failures {
		return fmt.Errorf("provider probe summary %d counts do not match results", index)
	}
	if summary.TotalP50MS < 0 || summary.TotalP95MS < 0 ||
		summary.FirstTranscriptP50MS < 0 || summary.FirstTranscriptP95MS < 0 ||
		summary.FirstTokenP50MS < 0 || summary.FirstTokenP95MS < 0 ||
		summary.FirstAudioP50MS < 0 || summary.FirstAudioP95MS < 0 {
		return fmt.Errorf("provider probe summary %d has negative latency", index)
	}
	if summary.Successes > 0 {
		if summary.TotalP50MS <= 0 || summary.TotalP95MS <= 0 {
			return fmt.Errorf("provider probe summary %d lacks total latency", index)
		}
		switch summary.Modality {
		case providers.ProbeModalityASR:
			if summary.FirstTranscriptP50MS <= 0 || summary.FirstTranscriptP95MS <= 0 {
				return fmt.Errorf("provider probe summary %d lacks ASR first transcript latency", index)
			}
		case providers.ProbeModalityLLM:
			if summary.FirstTokenP50MS <= 0 || summary.FirstTokenP95MS <= 0 {
				return fmt.Errorf("provider probe summary %d lacks LLM first token latency", index)
			}
		case providers.ProbeModalityTTS:
			if summary.FirstAudioP50MS <= 0 || summary.FirstAudioP95MS <= 0 {
				return fmt.Errorf("provider probe summary %d lacks TTS first audio latency", index)
			}
		}
	}
	return nil
}

func validateSkipped(skipped SkippedProbe, index int) error {
	if err := validateReportID("skipped provider_id", skipped.ProviderID, index); err != nil {
		return err
	}
	if !isSupportedModality(skipped.Modality) {
		return fmt.Errorf("provider probe skipped entry %d has invalid modality", index)
	}
	if strings.TrimSpace(skipped.Reason) == "" {
		return fmt.Errorf("provider probe skipped entry %d reason is required", index)
	}
	return nil
}

func validateReportID(field string, value string, index int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("provider probe %s at index %d is required", field, index)
	}
	if value != strings.TrimSpace(value) || strings.ContainsAny(value, "\r\n\t") || len(value) > 256 {
		return fmt.Errorf("provider probe %s at index %d is not sanitized", field, index)
	}
	return nil
}

func isSupportedModality(modality string) bool {
	switch modality {
	case providers.ProbeModalityASR, providers.ProbeModalityLLM, providers.ProbeModalityTTS:
		return true
	default:
		return false
	}
}

func isSafeErrorClass(errorClass string) bool {
	switch errorClass {
	case "provider_not_found", "provider_config_error", "unsupported_modality", "no_final_transcript", "timeout", "canceled", "network_error", "provider_error":
		return true
	default:
		return false
	}
}

func summaryKey(providerID string, modality string) string {
	return providerID + "\x00" + modality
}
