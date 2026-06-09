package providerprobe

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"stackchan-gateway/internal/providers"
)

func TestValidateEvidenceArchiveAcceptsSafePackageOutput(t *testing.T) {
	report := validEvidenceReport()
	reportData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	summaryMarkdown := FormatReportSummaryMarkdown(SummarizeReport("provider-probe-20260606-120000.json", report))
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-20260606-120000.json": string(reportData),
		"provider-probe-summary.md":           summaryMarkdown,
		"provider-probe-gate.txt":             validEvidenceGateText(),
	})

	summary, err := validateEvidenceArchiveGzipBytes(t, archive)
	if err != nil {
		t.Fatalf("ValidateEvidenceArchive() error = %v", err)
	}
	if summary.Reports != 1 || !summary.HasSummary || !summary.HasGate || summary.ValidatedBytes == 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestValidateEvidenceArchiveRejectsUnexpectedEnvFile(t *testing.T) {
	archive := buildEvidenceArchive(t, map[string]string{
		"provider.env":              "DASHSCOPE_API_KEY=secret-ish\n",
		"provider-probe-summary.md": "safe\n",
		"provider-probe-gate.txt":   validEvidenceGateText(),
	})

	_, err := validateEvidenceArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "unexpected entry") {
		t.Fatalf("ValidateEvidenceArchive() error = %v, want unexpected entry", err)
	}
}

func TestValidateEvidenceArchiveRejectsLegacyA21SmokeReportWithActionableError(t *testing.T) {
	archive := buildEvidenceArchive(t, map[string]string{
		"a21-provider-smoke-20260605-225111-334580400.json": `{"provider":"StepFun","status":"passed"}`,
		"provider-probe-summary.md":                         "safe\n",
		"provider-probe-gate.txt":                           validEvidenceGateText(),
	})

	_, err := validateEvidenceArchiveGzipBytes(t, archive)
	if err == nil {
		t.Fatal("ValidateEvidenceArchive() error = nil, want legacy report rejection")
	}
	for _, want := range []string{"legacy A21 smoke report", "reference-only", "provider-probe-package", "provider-probe-*.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateEvidenceArchive() error = %v, want %q", err, want)
		}
	}
}

func TestValidateEvidenceArchiveRejectsUnsafeReport(t *testing.T) {
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-unsafe.json": `{
			"profile": "unsafe",
			"runs": 1,
			"timeout_ms": 5000,
			"prompt_text_bytes": 12,
			"started_at_unix_ms": 1,
			"finished_at_unix_ms": 2,
			"successes": 1,
			"failures": 0,
			"results": [{
				"run": 1,
				"provider_id": "mock",
				"modality": "llm",
				"result": {
					"provider_id": "mock",
					"modality": "llm",
					"ok": true,
					"first_token_ms": 1,
					"total_ms": 2,
					"output_text_bytes": 4,
					"started_at_unix_ms": 1,
					"finished_at_unix_ms": 2,
					"raw_payload": "leaked"
				}
			}],
			"summaries": [{
				"provider_id": "mock",
				"modality": "llm",
				"runs": 1,
				"successes": 1,
				"failures": 0,
				"first_token_p50_ms": 1,
				"first_token_p95_ms": 1,
				"total_p50_ms": 2,
				"total_p95_ms": 2
			}]
		}`,
		"provider-probe-summary.md": "safe\n",
		"provider-probe-gate.txt":   validEvidenceGateText(),
	})

	_, err := validateEvidenceArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "unsafe field") {
		t.Fatalf("ValidateEvidenceArchive() error = %v, want unsafe report", err)
	}
}

func TestValidateEvidenceArchiveRejectsUnsafeEntryName(t *testing.T) {
	archive := buildEvidenceArchive(t, map[string]string{
		"../provider-probe-escape.json": "{}",
		"provider-probe-summary.md":     "safe\n",
		"provider-probe-gate.txt":       validEvidenceGateText(),
	})

	_, err := validateEvidenceArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "unsafe entry name") {
		t.Fatalf("ValidateEvidenceArchive() error = %v, want unsafe name", err)
	}
}

func TestValidateEvidenceArchiveRejectsMissingGateOK(t *testing.T) {
	report := validEvidenceReport()
	reportData, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-20260606-120000.json": string(reportData),
		"provider-probe-summary.md":           "safe\n",
		"provider-probe-gate.txt":             "provider-probe-gate failed: missing fallback\n",
	})

	_, err = validateEvidenceArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "passing gate") {
		t.Fatalf("ValidateEvidenceArchive() error = %v, want gate failure", err)
	}
}

func TestValidateEvidenceArchiveRejectsGateWithoutThresholds(t *testing.T) {
	report := validEvidenceReport()
	reportData, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-20260606-120000.json": string(reportData),
		"provider-probe-summary.md":           "safe\n",
		"provider-probe-gate.txt":             "provider-probe gate OK: rows=1 profiles=1 providers=1\n",
	})

	_, err = validateEvidenceArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "missing min_runs") {
		t.Fatalf("ValidateEvidenceArchive() error = %v, want missing gate threshold", err)
	}
}

func TestValidateEvidenceArchiveRejectsGateWithEmptyRequiredCoverage(t *testing.T) {
	report := validEvidenceReport()
	reportData, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-20260606-120000.json": string(reportData),
		"provider-probe-summary.md":           "safe\n",
		"provider-probe-gate.txt":             "provider-probe gate OK: rows=1 profiles=1 providers=1 min_runs=20 min_success_percent=80 required_profiles= required_modalities= fallback_modality=\n",
	})

	_, err = validateEvidenceArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "required_profiles") {
		t.Fatalf("ValidateEvidenceArchive() error = %v, want empty required coverage failure", err)
	}
}

func TestValidateEvidenceArchiveRejectsGateThatDoesNotMatchReports(t *testing.T) {
	report := validEvidenceReport()
	reportData, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-20260606-120000.json": string(reportData),
		"provider-probe-summary.md":           "safe\n",
		"provider-probe-gate.txt":             "provider-probe gate OK: rows=2 profiles=1 providers=2 min_runs=1 min_success_percent=80 required_profiles=missing-profile required_modalities=llm fallback_modality=llm source_ref=test-source source_state=clean\n",
	})

	_, err = validateEvidenceArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "does not match reports") || !strings.Contains(err.Error(), "required profile missing-profile is missing") {
		t.Fatalf("ValidateEvidenceArchive() error = %v, want mismatched gate failure", err)
	}
}

func TestValidateEvidenceArchiveRejectsGateCountMismatch(t *testing.T) {
	report := validEvidenceReport()
	reportData, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-20260606-120000.json": string(reportData),
		"provider-probe-summary.md":           "safe\n",
		"provider-probe-gate.txt":             "provider-probe gate OK: rows=99 profiles=1 providers=2 min_runs=1 min_success_percent=80 required_profiles=stepfun-llm required_modalities=llm fallback_modality=llm source_ref=test-source source_state=clean\n",
	})

	_, err = validateEvidenceArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "counts do not match reports") {
		t.Fatalf("ValidateEvidenceArchive() error = %v, want count mismatch failure", err)
	}
}

func TestValidateDiagnosticsArchiveAcceptsFailedGatePackage(t *testing.T) {
	report := diagnosticsFailureReport()
	reportData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	summaryMarkdown := FormatReportSummaryMarkdown(SummarizeReport("provider-probe-20260606-120000.json", report))
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-20260606-120000.json": string(reportData),
		"provider-probe-summary.md":           summaryMarkdown,
		"provider-probe-gate.txt":             "provider-probe-gate failed: provider probe gate failed: deepseek-llm/llm in profile deepseek-llm has no successful probes; errors=provider_config_error\n",
	})

	summary, err := validateDiagnosticsArchiveGzipBytes(t, archive)
	if err != nil {
		t.Fatalf("ValidateDiagnosticsArchive() error = %v", err)
	}
	if summary.Reports != 1 || !summary.HasSummary || !summary.HasGate || summary.ValidatedBytes == 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestValidateDiagnosticsArchiveRejectsPassingGate(t *testing.T) {
	report := validEvidenceReport()
	reportData, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-20260606-120000.json": string(reportData),
		"provider-probe-summary.md":           "safe\n",
		"provider-probe-gate.txt":             validEvidenceGateText(),
	})

	_, err = validateDiagnosticsArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "passing gate") {
		t.Fatalf("ValidateDiagnosticsArchive() error = %v, want passing gate rejection", err)
	}
}

func TestValidateDiagnosticsArchiveRejectsUnsafeReport(t *testing.T) {
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-unsafe.json": `{
			"profile": "unsafe",
			"runs": 1,
			"timeout_ms": 5000,
			"prompt_text_bytes": 12,
			"started_at_unix_ms": 1,
			"finished_at_unix_ms": 2,
			"successes": 1,
			"failures": 0,
			"results": [{
				"run": 1,
				"provider_id": "mock",
				"modality": "llm",
				"result": {
					"provider_id": "mock",
					"modality": "llm",
					"ok": true,
					"first_token_ms": 1,
					"total_ms": 2,
					"output_text_bytes": 4,
					"started_at_unix_ms": 1,
					"finished_at_unix_ms": 2,
					"raw_payload": "leaked"
				}
			}],
			"summaries": [{
				"provider_id": "mock",
				"modality": "llm",
				"runs": 1,
				"successes": 1,
				"failures": 0,
				"first_token_p50_ms": 1,
				"first_token_p95_ms": 1,
				"total_p50_ms": 2,
				"total_p95_ms": 2
			}]
		}`,
		"provider-probe-summary.md": "safe\n",
		"provider-probe-gate.txt":   "provider-probe-gate failed: provider probe gate failed: mock/llm in profile unsafe has no successful probes; errors=provider_config_error\n",
	})

	_, err := validateDiagnosticsArchiveGzipBytes(t, archive)
	if err == nil || !strings.Contains(err.Error(), "unsafe field") {
		t.Fatalf("ValidateDiagnosticsArchive() error = %v, want unsafe report", err)
	}
}

func TestLoadEvidenceArchivePromotionRegeneratesSummaryFromReports(t *testing.T) {
	report := validEvidenceReport()
	reportData, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	archive := buildEvidenceArchive(t, map[string]string{
		"provider-probe-20260606-120000.json": string(reportData),
		"provider-probe-summary.md":           "remote summary should not be promoted\n",
		"provider-probe-gate.txt":             validEvidenceGateText(),
	})
	archivePath := writeEvidenceArchiveFile(t, archive)

	promotion, err := LoadEvidenceArchivePromotion(archivePath)
	if err != nil {
		t.Fatalf("LoadEvidenceArchivePromotion() error = %v", err)
	}
	if promotion.Archive == "" || len(promotion.SHA256) != 64 || promotion.Summary.Reports != 1 {
		t.Fatalf("promotion = %+v", promotion)
	}
	if len(promotion.Rows) != 2 || promotion.Rows[0].Profile != "stepfun-llm" || promotion.Rows[0].ProviderID != "stepfun-llm" {
		t.Fatalf("rows = %+v", promotion.Rows)
	}

	markdown := FormatEvidenceArchivePromotionMarkdown(promotion)
	for _, want := range []string{"# Provider Probe Evidence", "SHA256:", "stepfun-llm", "80 / 80 ms"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "remote summary should not be promoted") {
		t.Fatalf("markdown trusted remote summary:\n%s", markdown)
	}
}

func validEvidenceGateText() string {
	return "provider-probe gate OK: rows=2 profiles=1 providers=2 min_runs=1 min_success_percent=80 required_profiles=stepfun-llm required_modalities=llm fallback_modality=llm source_ref=test-source source_state=clean\n"
}

func validEvidenceReport() Report {
	return Report{
		Profile:         "stepfun-llm",
		Runs:            1,
		TimeoutMS:       5000,
		PromptTextBytes: 12,
		StartedAtUnixMS: 1000,
		FinishedUnixMS:  1200,
		Successes:       2,
		Results: []RunResult{{
			Run:        1,
			ProviderID: "stepfun-llm",
			Modality:   providers.ProbeModalityLLM,
			Result: providers.ProbeResult{
				ProviderID:       "stepfun-llm",
				Modality:         providers.ProbeModalityLLM,
				OK:               true,
				ProviderModelID:  "step-1v-mini",
				FirstTokenMS:     80,
				TotalMS:          180,
				OutputTextBytes:  16,
				StartedAtUnixMS:  1000,
				FinishedAtUnixMS: 1180,
			},
		}, {
			Run:        1,
			ProviderID: "doubao-llm",
			Modality:   providers.ProbeModalityLLM,
			Result: providers.ProbeResult{
				ProviderID:       "doubao-llm",
				Modality:         providers.ProbeModalityLLM,
				OK:               true,
				ProviderModelID:  "doubao-seed-fixture",
				FirstTokenMS:     100,
				TotalMS:          220,
				OutputTextBytes:  18,
				StartedAtUnixMS:  1000,
				FinishedAtUnixMS: 1220,
			},
		}},
		Summaries: []Summary{{
			ProviderID:      "stepfun-llm",
			Modality:        providers.ProbeModalityLLM,
			Runs:            1,
			Successes:       1,
			FirstTokenP50MS: 80,
			FirstTokenP95MS: 80,
			TotalP50MS:      180,
			TotalP95MS:      180,
		}, {
			ProviderID:      "doubao-llm",
			Modality:        providers.ProbeModalityLLM,
			Runs:            1,
			Successes:       1,
			FirstTokenP50MS: 100,
			FirstTokenP95MS: 100,
			TotalP50MS:      220,
			TotalP95MS:      220,
		}},
	}
}

func diagnosticsFailureReport() Report {
	return Report{
		Profile:         "deepseek-llm",
		Runs:            1,
		TimeoutMS:       5000,
		PromptTextBytes: 12,
		StartedAtUnixMS: 1000,
		FinishedUnixMS:  1200,
		Failures:        1,
		Results: []RunResult{{
			Run:        1,
			ProviderID: "deepseek-llm",
			Modality:   providers.ProbeModalityLLM,
			ErrorClass: "provider_config_error",
			Result: providers.ProbeResult{
				ProviderID:       "deepseek-llm",
				Modality:         providers.ProbeModalityLLM,
				OK:               false,
				ProviderError:    "provider_config_error",
				StartedAtUnixMS:  1000,
				FinishedAtUnixMS: 1100,
			},
		}},
		Summaries: []Summary{{
			ProviderID: "deepseek-llm",
			Modality:   providers.ProbeModalityLLM,
			Runs:       1,
			Failures:   1,
		}},
	}
}

func buildEvidenceArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range entries {
		data := []byte(content)
		if err := tarWriter.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(data)),
		}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tarWriter.Write(data); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buffer.Bytes()
}

func validateEvidenceArchiveGzipBytes(t *testing.T, archive []byte) (EvidenceArchiveSummary, error) {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer gzipReader.Close()
	return ValidateEvidenceArchive(gzipReader)
}

func validateDiagnosticsArchiveGzipBytes(t *testing.T, archive []byte) (EvidenceArchiveSummary, error) {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer gzipReader.Close()
	return ValidateDiagnosticsArchive(gzipReader)
}

func writeEvidenceArchiveFile(t *testing.T, archive []byte) string {
	t.Helper()
	path := t.TempDir() + "/provider-probe-evidence-test.tgz"
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	return path
}
