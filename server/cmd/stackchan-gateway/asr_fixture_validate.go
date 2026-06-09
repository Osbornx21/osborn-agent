package main

import (
	"flag"
	"fmt"
	"io"

	"stackchan-gateway/internal/providerprobe"
)

func runASRFixtureValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("asr-fixture-validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fixturePath := flags.String("fixture", "", "ASR xiaozhi Opus fixture JSON path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *fixturePath == "" {
		fmt.Fprintln(stderr, "asr-fixture-validate failed: --fixture is required")
		return 2
	}
	inspection, err := providerprobe.ValidateASROpusFixtureForSemanticProbe(*fixturePath)
	if err != nil {
		fmt.Fprintf(stderr, "asr-fixture-validate failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "asr fixture OK: frames=%d bytes=%d duration_ms=%d unique_payloads=%d fixture=%s\n", inspection.Frames, inspection.Bytes, inspection.DurationMS, inspection.UniquePayloads, *fixturePath)
	return 0
}
