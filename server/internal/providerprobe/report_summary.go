package providerprobe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ReportSummaryRow struct {
	Source               string
	Profile              string
	ProviderID           string
	Modality             string
	Runs                 int
	Successes            int
	Failures             int
	FirstTranscriptP50MS int64
	FirstTranscriptP95MS int64
	FirstTokenP50MS      int64
	FirstTokenP95MS      int64
	FirstAudioP50MS      int64
	FirstAudioP95MS      int64
	TotalP50MS           int64
	TotalP95MS           int64
	ErrorClasses         []string
}

func LoadValidatedReportSummaries(paths []string) ([]ReportSummaryRow, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one provider probe report path is required")
	}

	var rows []ReportSummaryRow
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read provider probe report: %w", err)
		}
		if err := ValidateReportJSON(data); err != nil {
			return nil, err
		}
		var report Report
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&report); err != nil {
			return nil, fmt.Errorf("decode provider probe report schema: %w", err)
		}
		rows = append(rows, SummarizeReport(filepath.Base(path), report)...)
	}
	return rows, nil
}

func SummarizeReport(source string, report Report) []ReportSummaryRow {
	errorClasses := map[string]map[string]struct{}{}
	for _, result := range report.Results {
		label := summaryErrorLabel(result)
		if label == "" {
			continue
		}
		key := summaryKey(result.ProviderID, result.Modality)
		current := errorClasses[key]
		if current == nil {
			current = map[string]struct{}{}
			errorClasses[key] = current
		}
		current[label] = struct{}{}
	}

	rows := make([]ReportSummaryRow, 0, len(report.Summaries))
	for _, summary := range report.Summaries {
		row := ReportSummaryRow{
			Source:               source,
			Profile:              report.Profile,
			ProviderID:           summary.ProviderID,
			Modality:             summary.Modality,
			Runs:                 summary.Runs,
			Successes:            summary.Successes,
			Failures:             summary.Failures,
			FirstTranscriptP50MS: summary.FirstTranscriptP50MS,
			FirstTranscriptP95MS: summary.FirstTranscriptP95MS,
			FirstTokenP50MS:      summary.FirstTokenP50MS,
			FirstTokenP95MS:      summary.FirstTokenP95MS,
			FirstAudioP50MS:      summary.FirstAudioP50MS,
			FirstAudioP95MS:      summary.FirstAudioP95MS,
			TotalP50MS:           summary.TotalP50MS,
			TotalP95MS:           summary.TotalP95MS,
			ErrorClasses:         sortedErrorClasses(errorClasses[summaryKey(summary.ProviderID, summary.Modality)]),
		}
		rows = append(rows, row)
	}
	return rows
}

func FormatReportSummaryMarkdown(rows []ReportSummaryRow) string {
	var builder strings.Builder
	builder.WriteString("| Source | Profile | Provider | Modality | Runs | Successes | Failures | First transcript p50/p95 | First token p50/p95 | First audio p50/p95 | Total p50/p95 | Error classes / codes |\n")
	builder.WriteString("|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, row := range rows {
		builder.WriteString("| ")
		builder.WriteString(markdownCell(row.Source))
		builder.WriteString(" | ")
		builder.WriteString(markdownCell(row.Profile))
		builder.WriteString(" | ")
		builder.WriteString(markdownCell(row.ProviderID))
		builder.WriteString(" | ")
		builder.WriteString(markdownCell(row.Modality))
		builder.WriteString(" | ")
		builder.WriteString(strconv.Itoa(row.Runs))
		builder.WriteString(" | ")
		builder.WriteString(strconv.Itoa(row.Successes))
		builder.WriteString(" | ")
		builder.WriteString(strconv.Itoa(row.Failures))
		builder.WriteString(" | ")
		builder.WriteString(formatLatencyPair(row.FirstTranscriptP50MS, row.FirstTranscriptP95MS))
		builder.WriteString(" | ")
		builder.WriteString(formatLatencyPair(row.FirstTokenP50MS, row.FirstTokenP95MS))
		builder.WriteString(" | ")
		builder.WriteString(formatLatencyPair(row.FirstAudioP50MS, row.FirstAudioP95MS))
		builder.WriteString(" | ")
		builder.WriteString(formatLatencyPair(row.TotalP50MS, row.TotalP95MS))
		builder.WriteString(" | ")
		builder.WriteString(markdownCell(strings.Join(row.ErrorClasses, ",")))
		builder.WriteString(" |\n")
	}
	return builder.String()
}

func summaryErrorLabel(result RunResult) string {
	label := strings.TrimSpace(result.ErrorClass)
	if label == "" {
		return ""
	}
	if result.Result.ProviderHTTPStatus > 0 {
		label += ":http_" + strconv.Itoa(result.Result.ProviderHTTPStatus)
	}
	if result.Result.ProviderErrorCode != "" {
		label += ":" + result.Result.ProviderErrorCode
	}
	return label
}

func sortedErrorClasses(classes map[string]struct{}) []string {
	if len(classes) == 0 {
		return nil
	}
	values := make([]string, 0, len(classes))
	for value := range classes {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func formatLatencyPair(p50 int64, p95 int64) string {
	if p50 <= 0 && p95 <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d / %d ms", p50, p95)
}

func markdownCell(value string) string {
	if strings.TrimSpace(value) == "" {
		return "n/a"
	}
	return strings.ReplaceAll(value, "|", "\\|")
}
