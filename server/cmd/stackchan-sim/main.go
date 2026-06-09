package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"stackchan-gateway/internal/simulator"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("stackchan-sim", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	scenario := flags.String("scenario", simulator.ScenarioHappyPath20Turns, "scenario id")
	firmwareProfile := flags.String("firmware-profile", simulator.FirmwareProfileMockGateway, "firmware profile: mock-gateway or official-v1.4.1")
	gatewayURL := flags.String("gateway", "", "xiaozhi websocket gateway URL")
	deviceID := flags.String("device", "stackchan-s3-main", "device id header")
	clientID := flags.String("client", "stackchan-s3-main-client", "client id header")
	authToken := flags.String("auth-token", "", "device auth token; prefer --auth-token-env")
	authTokenEnv := flags.String("auth-token-env", "STACKCHAN_MAIN_AUTH_TOKEN", "device auth token env name")
	protocolVersion := flags.Int("protocol-version", 1, "xiaozhi binary protocol version: 1, 2 or 3")
	turns := flags.Int("turns", 0, "turn count override for happy_path_20_turns")
	asrFixturePath := flags.String("asr-opus-fixture", "", "optional xiaozhi_opus_frames_v1 fixture JSON for real ASR runs")
	maxFirstAudioMS := flags.Int64("max-first-audio-ms", 0, "optional first downlink audio budget in milliseconds; provider_slow_first_audio defaults to 1500")
	timeoutMS := flags.Int("timeout-ms", 5000, "per downlink read timeout in milliseconds")
	traceFile := flags.String("trace-file", "", "optional gateway trace JSONL path to verify after this run")
	requireTraceEvents := flags.String("require-trace-events", "", "comma-separated trace event names required after this run")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	token := strings.TrimSpace(*authToken)
	if token == "" && strings.TrimSpace(*authTokenEnv) != "" {
		token, _ = os.LookupEnv(strings.TrimSpace(*authTokenEnv))
	}
	if token == "" {
		fmt.Fprintf(os.Stderr, "stackchan-sim failed: auth token is required; set %s or pass --auth-token\n", *authTokenEnv)
		return 2
	}

	summary, err := simulator.RunScenario(context.Background(), simulator.ScenarioOptions{
		Scenario:           *scenario,
		FirmwareProfile:    *firmwareProfile,
		GatewayURL:         *gatewayURL,
		DeviceID:           *deviceID,
		ClientID:           *clientID,
		AuthToken:          token,
		ProtocolVersion:    *protocolVersion,
		Turns:              *turns,
		ASRFixturePath:     *asrFixturePath,
		MaxFirstAudioMS:    *maxFirstAudioMS,
		Timeout:            time.Duration(*timeoutMS) * time.Millisecond,
		TraceFile:          *traceFile,
		RequireTraceEvents: parseCSV(*requireTraceEvents),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "stackchan-sim failed: %v\n", err)
		return 1
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "stackchan-sim failed: encode summary: %v\n", err)
		return 1
	}
	fmt.Println(string(data))
	if !summary.Passed {
		return 1
	}
	return 0
}

func parseCSV(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
