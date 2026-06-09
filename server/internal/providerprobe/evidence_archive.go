package providerprobe

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	maxEvidenceArchiveSize = 32 << 20
	maxEvidenceFiles       = 128
	maxEvidenceFileSize    = 2 << 20
)

type EvidenceArchiveSummary struct {
	Reports        int
	HasSummary     bool
	HasGate        bool
	ValidatedBytes int64
}

type EvidenceArchivePromotion struct {
	Archive string
	SHA256  string
	Summary EvidenceArchiveSummary
	Rows    []ReportSummaryRow
}

type gateEvidence struct {
	Rows        int
	Profiles    int
	Providers   int
	SourceRef   string
	SourceState string
	Options     ReportGateOptions
}

type archiveValidationMode string

const (
	archiveValidationEvidence    archiveValidationMode = "evidence"
	archiveValidationDiagnostics archiveValidationMode = "diagnostics"
)

func ValidateEvidenceArchiveFile(path string) (EvidenceArchiveSummary, error) {
	if strings.TrimSpace(path) == "" {
		return EvidenceArchiveSummary{}, fmt.Errorf("provider probe evidence archive path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return EvidenceArchiveSummary{}, fmt.Errorf("open provider probe evidence archive: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return EvidenceArchiveSummary{}, fmt.Errorf("open provider probe evidence archive gzip: %w", err)
	}
	defer gzipReader.Close()

	summary, _, err := inspectArchive(gzipReader, false, archiveValidationEvidence)
	return summary, err
}

func ValidateEvidenceArchive(reader io.Reader) (EvidenceArchiveSummary, error) {
	summary, _, err := inspectArchive(reader, false, archiveValidationEvidence)
	return summary, err
}

func ValidateDiagnosticsArchiveFile(path string) (EvidenceArchiveSummary, error) {
	if strings.TrimSpace(path) == "" {
		return EvidenceArchiveSummary{}, fmt.Errorf("provider probe diagnostics archive path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return EvidenceArchiveSummary{}, fmt.Errorf("open provider probe diagnostics archive: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return EvidenceArchiveSummary{}, fmt.Errorf("open provider probe diagnostics archive gzip: %w", err)
	}
	defer gzipReader.Close()

	summary, _, err := inspectArchive(gzipReader, false, archiveValidationDiagnostics)
	return summary, err
}

func ValidateDiagnosticsArchive(reader io.Reader) (EvidenceArchiveSummary, error) {
	summary, _, err := inspectArchive(reader, false, archiveValidationDiagnostics)
	return summary, err
}

func LoadEvidenceArchivePromotion(path string) (EvidenceArchivePromotion, error) {
	if strings.TrimSpace(path) == "" {
		return EvidenceArchivePromotion{}, fmt.Errorf("provider probe evidence archive path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return EvidenceArchivePromotion{}, fmt.Errorf("stat provider probe evidence archive: %w", err)
	}
	if info.Size() <= 0 || info.Size() > maxEvidenceArchiveSize {
		return EvidenceArchivePromotion{}, fmt.Errorf("provider probe evidence archive file size is invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return EvidenceArchivePromotion{}, fmt.Errorf("read provider probe evidence archive: %w", err)
	}
	digest := sha256.Sum256(data)

	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return EvidenceArchivePromotion{}, fmt.Errorf("open provider probe evidence archive gzip: %w", err)
	}
	defer gzipReader.Close()

	summary, rows, err := inspectArchive(gzipReader, true, archiveValidationEvidence)
	if err != nil {
		return EvidenceArchivePromotion{}, err
	}
	return EvidenceArchivePromotion{
		Archive: filepath.Base(path),
		SHA256:  hex.EncodeToString(digest[:]),
		Summary: summary,
		Rows:    rows,
	}, nil
}

func FormatEvidenceArchivePromotionMarkdown(promotion EvidenceArchivePromotion) string {
	var builder strings.Builder
	builder.WriteString("# Provider Probe Evidence\n\n")
	builder.WriteString("Archive: `")
	builder.WriteString(markdownCell(promotion.Archive))
	builder.WriteString("`\n\n")
	builder.WriteString("SHA256: `")
	builder.WriteString(markdownCell(promotion.SHA256))
	builder.WriteString("`\n\n")
	builder.WriteString(fmt.Sprintf("Reports: `%d`\n\n", promotion.Summary.Reports))
	builder.WriteString(fmt.Sprintf("Validated bytes: `%d`\n\n", promotion.Summary.ValidatedBytes))
	builder.WriteString(FormatReportSummaryMarkdown(promotion.Rows))
	return builder.String()
}

func inspectArchive(reader io.Reader, collectRows bool, mode archiveValidationMode) (EvidenceArchiveSummary, []ReportSummaryRow, error) {
	if reader == nil {
		return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive reader is required")
	}

	tarReader := tar.NewReader(reader)
	summary := EvidenceArchiveSummary{}
	var rows []ReportSummaryRow
	var gate gateEvidence
	gateOptionsSet := false
	seen := map[string]bool{}

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return EvidenceArchiveSummary{}, nil, fmt.Errorf("read provider probe evidence archive: %w", err)
		}
		if len(seen) >= maxEvidenceFiles {
			return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive has too many files")
		}
		name := strings.TrimSpace(header.Name)
		if err := validateEvidenceArchiveName(name); err != nil {
			return EvidenceArchiveSummary{}, nil, err
		}
		if seen[name] {
			return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive contains duplicate entry %q", name)
		}
		seen[name] = true
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive entry %q must be a regular file", name)
		}
		if header.Size < 0 || header.Size > maxEvidenceFileSize {
			return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive entry %q has invalid size", name)
		}
		data, err := readEvidenceEntry(tarReader, header.Size, name)
		if err != nil {
			return EvidenceArchiveSummary{}, nil, err
		}
		summary.ValidatedBytes += int64(len(data))

		switch {
		case name == "provider-probe-summary.md":
			if err := validateEvidenceText(data, name, false); err != nil {
				return EvidenceArchiveSummary{}, nil, err
			}
			summary.HasSummary = true
		case name == "provider-probe-gate.txt":
			if err := validateEvidenceText(data, name, false); err != nil {
				return EvidenceArchiveSummary{}, nil, err
			}
			if mode == archiveValidationEvidence {
				parsedGate, err := parseGateEvidenceText(string(data))
				if err != nil {
					return EvidenceArchiveSummary{}, nil, err
				}
				gate = parsedGate
				gateOptionsSet = true
			} else if err := validateDiagnosticsGateText(string(data)); err != nil {
				return EvidenceArchiveSummary{}, nil, err
			}
			summary.HasGate = true
		case isEvidenceReportName(name):
			if err := ValidateReportJSON(data); err != nil {
				return EvidenceArchiveSummary{}, nil, fmt.Errorf("validate %s: %w", name, err)
			}
			var report Report
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&report); err != nil {
				return EvidenceArchiveSummary{}, nil, fmt.Errorf("decode %s: %w", name, err)
			}
			rows = append(rows, SummarizeReport(name, report)...)
			summary.Reports++
		default:
			return EvidenceArchiveSummary{}, nil, unexpectedEvidenceEntryError(name)
		}
	}

	if summary.Reports == 0 {
		return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive contains no provider-probe JSON reports")
	}
	if !summary.HasSummary {
		return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive missing provider-probe-summary.md")
	}
	if !summary.HasGate {
		return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive missing provider-probe-gate.txt")
	}
	if mode == archiveValidationDiagnostics {
		if !collectRows {
			rows = nil
		}
		return summary, rows, nil
	}
	if !gateOptionsSet {
		return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive missing provider-probe-gate.txt")
	}
	result, err := EvaluateReportGate(rows, gate.Options)
	if err != nil {
		return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive gate entry does not match reports: %w", err)
	}
	if result.Rows != gate.Rows || len(result.Profiles) != gate.Profiles || len(result.Providers) != gate.Providers {
		return EvidenceArchiveSummary{}, nil, fmt.Errorf("provider probe evidence archive gate entry counts do not match reports: rows=%d/%d profiles=%d/%d providers=%d/%d", gate.Rows, result.Rows, gate.Profiles, len(result.Profiles), gate.Providers, len(result.Providers))
	}
	if !collectRows {
		rows = nil
	}
	return summary, rows, nil
}

func validateEvidenceArchiveName(name string) error {
	if name == "" {
		return fmt.Errorf("provider probe evidence archive contains empty entry name")
	}
	if filepath.IsAbs(name) || filepath.Base(name) != name || strings.Contains(name, `\`) || strings.Contains(name, "..") {
		return fmt.Errorf("provider probe evidence archive contains unsafe entry name %q", name)
	}
	return nil
}

func readEvidenceEntry(reader io.Reader, size int64, name string) ([]byte, error) {
	limited := io.LimitReader(reader, size+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read provider probe evidence archive entry %q: %w", name, err)
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("provider probe evidence archive entry %q size mismatch", name)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("provider probe evidence archive entry %q is empty", name)
	}
	return data, nil
}

func isEvidenceReportName(name string) bool {
	return strings.HasPrefix(name, "provider-probe-") && strings.HasSuffix(name, ".json")
}

func unexpectedEvidenceEntryError(name string) error {
	if strings.HasPrefix(name, "a21-provider-smoke-") && strings.HasSuffix(name, ".json") {
		return fmt.Errorf("provider probe evidence archive contains legacy A21 smoke report %q; legacy reports are reference-only and cannot be promoted, rerun provider-probe-package on 5080lab/ECS to produce provider-probe-*.json evidence", name)
	}
	return fmt.Errorf("provider probe evidence archive contains unexpected entry %q", name)
}

func validateEvidenceText(data []byte, name string, requireGateOK bool) error {
	text := string(data)
	if len(text) > maxEvidenceFileSize {
		return fmt.Errorf("provider probe evidence archive entry %q is too large", name)
	}
	if requireGateOK {
		if _, err := parseGateEvidenceText(text); err != nil {
			return err
		}
	}
	for _, pattern := range secretLikeReportValues {
		if pattern.MatchString(text) {
			return fmt.Errorf("provider probe evidence archive entry %q contains secret-like value", name)
		}
	}
	for _, forbidden := range []string{
		"Authorization:",
		"api_key",
		"access_key",
		"generated_text",
		"payload_base64",
		"payload_hex",
		"provider.env",
		"raw_payload",
		"signed_url",
	} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			return fmt.Errorf("provider probe evidence archive entry %q contains forbidden marker %q", name, forbidden)
		}
	}
	return nil
}

func validateDiagnosticsGateText(text string) error {
	if strings.Contains(text, "provider-probe gate OK:") {
		return fmt.Errorf("provider probe diagnostics archive gate entry unexpectedly shows a passing gate")
	}
	if !strings.Contains(text, "provider-probe-gate failed:") {
		return fmt.Errorf("provider probe diagnostics archive gate entry does not show a gate failure")
	}
	return nil
}

func parseGateEvidenceText(text string) (gateEvidence, error) {
	if !strings.Contains(text, "provider-probe gate OK:") {
		return gateEvidence{}, fmt.Errorf("provider probe evidence archive gate entry does not show a passing gate")
	}
	params := map[string]string{}
	for _, token := range strings.Fields(text) {
		key, value, ok := strings.Cut(token, "=")
		if ok {
			params[key] = value
		}
	}
	required := []string{"rows", "profiles", "providers", "min_runs", "min_success_percent", "required_profiles", "required_modalities", "fallback_modality", "source_ref", "source_state"}
	for _, key := range required {
		value, ok := params[key]
		if !ok {
			return gateEvidence{}, fmt.Errorf("provider probe evidence archive gate entry missing %s", key)
		}
		if key == "required_profiles" || key == "required_modalities" || key == "fallback_modality" || key == "source_ref" || key == "source_state" {
			if strings.TrimSpace(value) == "" {
				return gateEvidence{}, fmt.Errorf("provider probe evidence archive gate entry missing %s", key)
			}
		}
	}
	rows, err := parsePositiveGateInt(params, "rows")
	if err != nil {
		return gateEvidence{}, err
	}
	profiles, err := parsePositiveGateInt(params, "profiles")
	if err != nil {
		return gateEvidence{}, err
	}
	providers, err := parsePositiveGateInt(params, "providers")
	if err != nil {
		return gateEvidence{}, err
	}
	minRuns, err := strconv.Atoi(params["min_runs"])
	if err != nil || minRuns <= 0 {
		return gateEvidence{}, fmt.Errorf("provider probe evidence archive gate entry has invalid min_runs")
	}
	minSuccessPercent, err := strconv.Atoi(params["min_success_percent"])
	if err != nil || minSuccessPercent <= 0 || minSuccessPercent > 100 {
		return gateEvidence{}, fmt.Errorf("provider probe evidence archive gate entry has invalid min_success_percent")
	}
	return gateEvidence{
		Rows:        rows,
		Profiles:    profiles,
		Providers:   providers,
		SourceRef:   strings.TrimSpace(params["source_ref"]),
		SourceState: strings.TrimSpace(params["source_state"]),
		Options: ReportGateOptions{
			MinRuns:            minRuns,
			MinSuccessPct:      minSuccessPercent,
			RequiredProfiles:   normalizedList([]string{params["required_profiles"]}),
			RequiredModalities: normalizedList([]string{params["required_modalities"]}),
			FallbackModality:   strings.TrimSpace(params["fallback_modality"]),
		},
	}, nil
}

func parsePositiveGateInt(params map[string]string, key string) (int, error) {
	value, err := strconv.Atoi(params[key])
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("provider probe evidence archive gate entry has invalid %s", key)
	}
	return value, nil
}
