package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	gatewayconfig "stackchan-gateway/internal/config"
	"stackchan-gateway/internal/feishu"
	servicetools "stackchan-gateway/internal/tools"
)

const defaultFeishuSmokeTimeout = 1500 * time.Millisecond

func runFeishuSmoke(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("feishu-smoke", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "./configs/stackchan-gateway.example.yaml", "path to gateway config")
	targetID := flags.String("target", "", "Feishu target_id from tools.feishu.allowed_targets")
	text := flags.String("text", "", "short smoke message text; never printed")
	timeoutMS := flags.Int("timeout-ms", 0, "optional timeout override in milliseconds")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*targetID) == "" {
		fmt.Fprintln(stderr, "feishu-smoke failed: --target is required")
		return 2
	}
	if strings.TrimSpace(*text) == "" {
		fmt.Fprintln(stderr, "feishu-smoke failed: --text is required")
		return 2
	}
	if *timeoutMS < 0 {
		fmt.Fprintln(stderr, "feishu-smoke failed: --timeout-ms must be >= 0")
		return 2
	}

	cfg, err := gatewayconfig.LoadFile(*configPath, gatewayconfig.OSLookupEnv)
	if err != nil {
		fmt.Fprintf(stderr, "feishu-smoke failed: %v\n", err)
		return 1
	}
	if !cfg.Tools.Feishu.Enabled {
		fmt.Fprintln(stderr, "feishu-smoke failed: tools.feishu.enabled must be true")
		return 1
	}
	targets := feishuSmokeTargetsFromConfig(cfg.Tools.Feishu.AllowedTargets)
	if !feishuSmokeHasTarget(targets, *targetID) {
		fmt.Fprintln(stderr, "feishu-smoke failed: target is not allowlisted")
		return 1
	}

	timeout := feishuSmokeTimeout(cfg, *timeoutMS)
	client, err := feishu.NewClient(feishu.ClientOptions{
		BaseURL:   cfg.Tools.Feishu.BaseURL,
		AppID:     os.Getenv(cfg.Tools.Feishu.AppIDEnv),
		AppSecret: os.Getenv(cfg.Tools.Feishu.AppSecretEnv),
		Client:    &http.Client{Timeout: timeout},
	})
	if err != nil {
		fmt.Fprintf(stderr, "feishu-smoke failed: %v\n", err)
		return 1
	}
	registry := servicetools.NewRegistry(servicetools.RegistryOptions{
		AllowedTools:       []string{feishu.SendTextToolName},
		AllowedPermissions: []string{servicetools.PermissionWrite},
		DefaultTimeout:     timeout,
	})
	if err := feishu.RegisterSendTextTool(registry, feishu.SendTextToolOptions{
		Client:       client,
		Targets:      targets,
		MaxTextRunes: cfg.Tools.Feishu.MaxTextChars,
	}); err != nil {
		fmt.Fprintf(stderr, "feishu-smoke failed: %v\n", err)
		return 1
	}
	result, err := registry.ExecuteTool(context.Background(), servicetools.Call{
		Name: feishu.SendTextToolName,
		Arguments: map[string]any{
			"target_id": strings.TrimSpace(*targetID),
			"text":      strings.TrimSpace(*text),
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "feishu-smoke failed: %s\n", servicetools.ErrorCode(err))
		return 1
	}
	var payload feishu.SendTextPayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		fmt.Fprintf(stderr, "feishu-smoke failed: decode safe result: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "feishu smoke OK: target=%s message_id=%s\n", payload.TargetID, payload.MessageID)
	return 0
}

func feishuSmokeTargetsFromConfig(targets []gatewayconfig.FeishuTargetConfig) []feishu.TargetConfig {
	out := make([]feishu.TargetConfig, 0, len(targets))
	for _, target := range targets {
		out = append(out, feishu.TargetConfig{
			TargetID:      target.TargetID,
			Description:   target.Description,
			ReceiveIDType: target.ReceiveIDType,
			ReceiveID:     os.Getenv(target.ReceiveIDEnv),
		})
	}
	return out
}

func feishuSmokeHasTarget(targets []feishu.TargetConfig, targetID string) bool {
	targetID = strings.TrimSpace(targetID)
	for _, target := range targets {
		if strings.TrimSpace(target.TargetID) == targetID {
			return true
		}
	}
	return false
}

func feishuSmokeTimeout(cfg *gatewayconfig.Config, overrideMS int) time.Duration {
	if overrideMS > 0 {
		return time.Duration(overrideMS) * time.Millisecond
	}
	if cfg != nil && cfg.Tools.Feishu.TimeoutMS > 0 {
		return time.Duration(cfg.Tools.Feishu.TimeoutMS) * time.Millisecond
	}
	return defaultFeishuSmokeTimeout
}
