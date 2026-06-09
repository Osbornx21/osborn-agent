package main

import (
	"flag"
	"fmt"
	"io"

	"stackchan-gateway/internal/providerprobe"
)

func runProviderProbeEvidenceValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-probe-evidence-validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	archivePath := flags.String("archive", "", "provider probe evidence .tgz path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *archivePath == "" {
		fmt.Fprintln(stderr, "provider-probe-evidence-validate failed: --archive is required")
		return 2
	}
	summary, err := providerprobe.ValidateEvidenceArchiveFile(*archivePath)
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe-evidence-validate failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "provider-probe evidence OK: reports=%d bytes=%d archive=%s\n", summary.Reports, summary.ValidatedBytes, *archivePath)
	return 0
}

func runProviderProbeEvidenceSummary(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-probe-evidence-summary", flag.ContinueOnError)
	flags.SetOutput(stderr)
	archivePath := flags.String("archive", "", "provider probe evidence .tgz path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *archivePath == "" {
		fmt.Fprintln(stderr, "provider-probe-evidence-summary failed: --archive is required")
		return 2
	}
	promotion, err := providerprobe.LoadEvidenceArchivePromotion(*archivePath)
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe-evidence-summary failed: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, providerprobe.FormatEvidenceArchivePromotionMarkdown(promotion))
	return 0
}

func runProviderProbeDiagnosticsValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("provider-probe-diagnostics-validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	archivePath := flags.String("archive", "", "provider probe diagnostics .tgz path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *archivePath == "" {
		fmt.Fprintln(stderr, "provider-probe-diagnostics-validate failed: --archive is required")
		return 2
	}
	summary, err := providerprobe.ValidateDiagnosticsArchiveFile(*archivePath)
	if err != nil {
		fmt.Fprintf(stderr, "provider-probe-diagnostics-validate failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "provider-probe diagnostics OK: reports=%d bytes=%d archive=%s\n", summary.Reports, summary.ValidatedBytes, *archivePath)
	return 0
}
