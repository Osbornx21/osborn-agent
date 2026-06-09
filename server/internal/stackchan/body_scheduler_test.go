package stackchan

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"stackchan-gateway/internal/mcp"
)

func TestBodySchedulerReportsConfiguredMinCommandGap(t *testing.T) {
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		MinCommandGap: 23 * time.Millisecond,
	})
	if got := scheduler.MinCommandGap(); got != 23*time.Millisecond {
		t.Fatalf("MinCommandGap() = %v, want 23ms", got)
	}
}

func TestBodySchedulerClampsUnsafeYaw(t *testing.T) {
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 1,
		Now:               fixedClock(time.Unix(100, 0)),
	})

	command, err := scheduler.ScheduleMotion(MotionCommand{
		Generation: 1,
		Yaw:        120,
		Pitch:      20,
		Speed:      150,
		Reason:     "test",
	})
	if err != nil {
		t.Fatalf("ScheduleMotion() error = %v", err)
	}
	if command.Yaw != 45 {
		t.Fatalf("yaw = %d, want 45", command.Yaw)
	}
}

func TestBodySchedulerClampsUnsafePitch(t *testing.T) {
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 1,
		Now:               fixedClock(time.Unix(100, 0)),
	})

	command, err := scheduler.ScheduleMotion(MotionCommand{
		Generation: 1,
		Yaw:        0,
		Pitch:      -20,
		Speed:      150,
	})
	if err != nil {
		t.Fatalf("ScheduleMotion() error = %v", err)
	}
	if command.Pitch != 0 {
		t.Fatalf("pitch = %d, want 0", command.Pitch)
	}
}

func TestBodySchedulerClampsAdminPriorityMotion(t *testing.T) {
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 1,
		Now:               fixedClock(time.Unix(100, 0)),
	})

	command, err := scheduler.ScheduleMotion(MotionCommand{
		Generation: 1,
		Yaw:        -120,
		Pitch:      90,
		Speed:      5000,
		Priority:   PriorityAdmin,
	})
	if err != nil {
		t.Fatalf("ScheduleMotion() error = %v", err)
	}
	if command.Yaw != -45 || command.Pitch != 45 || command.Speed != 1000 {
		t.Fatalf("admin command = %+v, want clamped motion", command)
	}
}

func TestBodySchedulerClampsSpeedAndAppliesDefault(t *testing.T) {
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 1,
		Now:               fixedClock(time.Unix(100, 0)),
	})

	command, err := scheduler.ScheduleMotion(MotionCommand{Generation: 1, Speed: 0})
	if err != nil {
		t.Fatalf("ScheduleMotion() error = %v", err)
	}
	if command.Speed != DefaultSpeed {
		t.Fatalf("speed = %d, want %d", command.Speed, DefaultSpeed)
	}

	scheduler = NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 1,
		Now:               fixedClock(time.Unix(100, 0)),
	})
	command, err = scheduler.ScheduleMotion(MotionCommand{Generation: 1, Speed: 5000})
	if err != nil {
		t.Fatalf("ScheduleMotion(high speed) error = %v", err)
	}
	if command.Speed != DefaultSpeedMax {
		t.Fatalf("speed = %d, want %d", command.Speed, DefaultSpeedMax)
	}
}

func TestBodySchedulerCoalescesFastRepeatedCommands(t *testing.T) {
	times := []time.Time{
		time.Unix(100, 0),
		time.Unix(100, 0).Add(50 * time.Millisecond),
		time.Unix(100, 0).Add(180 * time.Millisecond),
	}
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 1,
		Now:               sequenceClock(times),
	})

	if _, err := scheduler.ScheduleMotion(MotionCommand{Generation: 1}); err != nil {
		t.Fatalf("first ScheduleMotion() error = %v", err)
	}
	if _, err := scheduler.ScheduleMotion(MotionCommand{Generation: 1, Yaw: 20, Pitch: 70, Speed: 200}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second error = %v, want ErrRateLimited", err)
	}
	coalesced, ok := scheduler.CoalescedMotion(1)
	if !ok {
		t.Fatal("coalesced motion missing")
	}
	if coalesced.Yaw != 20 || coalesced.Pitch != 45 || coalesced.Speed != 200 {
		t.Fatalf("coalesced = %+v, want latest clamped command", coalesced)
	}
	if _, err := scheduler.ScheduleMotion(MotionCommand{Generation: 1}); err != nil {
		t.Fatalf("third ScheduleMotion() error = %v", err)
	}
	if _, ok := scheduler.CoalescedMotion(1); ok {
		t.Fatal("coalesced motion still present after accepted command")
	}
}

func TestBodySchedulerDropsOldGenerationCommand(t *testing.T) {
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 3,
		Now:               fixedClock(time.Unix(100, 0)),
	})

	_, err := scheduler.ScheduleMotion(MotionCommand{Generation: 2})

	if !errors.Is(err, ErrOldGeneration) {
		t.Fatalf("error = %v, want ErrOldGeneration", err)
	}
}

func TestBodySchedulerLimitsCommandsPerTurn(t *testing.T) {
	times := make([]time.Time, 17)
	start := time.Unix(100, 0)
	for i := range times {
		times[i] = start.Add(time.Duration(i) * 200 * time.Millisecond)
	}
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration:  1,
		MaxCommandsPerTurn: 16,
		Now:                sequenceClock(times),
	})

	for i := 0; i < 16; i++ {
		if _, err := scheduler.ScheduleMotion(MotionCommand{Generation: 1}); err != nil {
			t.Fatalf("ScheduleMotion(%d) error = %v", i, err)
		}
	}
	if _, err := scheduler.ScheduleMotion(MotionCommand{Generation: 1}); !errors.Is(err, ErrTurnCommandLimit) {
		t.Fatalf("error = %v, want ErrTurnCommandLimit", err)
	}
}

func TestBodySchedulerCommandLimitResetsPerGeneration(t *testing.T) {
	times := make([]time.Time, 17)
	start := time.Unix(100, 0)
	for i := range times {
		times[i] = start.Add(time.Duration(i) * 200 * time.Millisecond)
	}
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration:  1,
		MaxCommandsPerTurn: 1,
		Now:                sequenceClock(times),
	})

	if _, err := scheduler.ScheduleMotion(MotionCommand{Generation: 1}); err != nil {
		t.Fatalf("gen 1 ScheduleMotion() error = %v", err)
	}
	if _, err := scheduler.ScheduleMotion(MotionCommand{Generation: 1}); !errors.Is(err, ErrTurnCommandLimit) {
		t.Fatalf("gen 1 second error = %v, want ErrTurnCommandLimit", err)
	}
	scheduler.SetGeneration(2)
	if _, err := scheduler.ScheduleMotion(MotionCommand{Generation: 2}); err != nil {
		t.Fatalf("gen 2 ScheduleMotion() error = %v", err)
	}
}

func TestBodySchedulerSetGenerationDoesNotRegressCurrentGeneration(t *testing.T) {
	caller := &recordingToolCaller{}
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 2,
		Now:               fixedClock(time.Unix(100, 0)),
	})

	scheduler.SetGeneration(1)
	_, err := scheduler.DispatchLED(context.Background(), caller, LEDCommand{Generation: 1})

	if !errors.Is(err, ErrOldGeneration) {
		t.Fatalf("DispatchLED() error = %v, want ErrOldGeneration after generation regression attempt", err)
	}
	if caller.tool != "" {
		t.Fatalf("tool = %q, want old generation LED suppressed", caller.tool)
	}
}

func TestMotionCommandBuildsMCPArguments(t *testing.T) {
	args := MotionCommand{Yaw: 15, Pitch: 8, Speed: 150}.MCPArguments()

	if args["yaw"] != 15 || args["pitch"] != 8 || args["speed"] != 150 {
		t.Fatalf("args = %#v, want yaw/pitch/speed", args)
	}
}

func TestBodySchedulerDispatchesClampedMotionToMCP(t *testing.T) {
	caller := &recordingToolCaller{}
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 1,
		Now:               fixedClock(time.Unix(100, 0)),
	})

	if _, err := scheduler.DispatchMotion(context.Background(), caller, MotionCommand{
		Generation: 1,
		Yaw:        120,
		Pitch:      -10,
		Speed:      0,
	}); err != nil {
		t.Fatalf("DispatchMotion() error = %v", err)
	}

	if caller.tool != mcp.ToolSetHeadAngles {
		t.Fatalf("tool = %q, want %q", caller.tool, mcp.ToolSetHeadAngles)
	}
	if caller.arguments["yaw"] != 45 || caller.arguments["pitch"] != 0 || caller.arguments["speed"] != DefaultSpeed {
		t.Fatalf("arguments = %#v, want clamped yaw/pitch/default speed", caller.arguments)
	}
}

func TestBodySchedulerChecksGenerationAgainBeforeDispatch(t *testing.T) {
	checks := 0
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 1,
		Now: sequenceClock([]time.Time{
			time.Unix(100, 0),
			time.Unix(100, 0).Add(200 * time.Millisecond),
		}),
		GenerationIsCurrent: func(int64) bool {
			checks++
			return checks <= 2
		},
	})
	firstCaller := &recordingToolCaller{}
	if _, err := scheduler.DispatchMotion(context.Background(), firstCaller, MotionCommand{Generation: 1}); err != nil {
		t.Fatalf("first DispatchMotion() error = %v", err)
	}
	secondCaller := &recordingToolCaller{}
	if _, err := scheduler.DispatchMotion(context.Background(), secondCaller, MotionCommand{Generation: 1}); !errors.Is(err, ErrOldGeneration) {
		t.Fatalf("second error = %v, want ErrOldGeneration", err)
	}
	if secondCaller.tool != "" {
		t.Fatalf("second tool = %q, want no MCP dispatch", secondCaller.tool)
	}
}

func TestBodySchedulerDispatchesCappedLEDToMCP(t *testing.T) {
	caller := &recordingToolCaller{}
	scheduler := NewBodyScheduler(BodySchedulerOptions{
		CurrentGeneration: 1,
		Now:               fixedClock(time.Unix(100, 0)),
	})

	if _, err := scheduler.DispatchLED(context.Background(), caller, LEDCommand{
		Generation: 1,
		R:          999,
		G:          -1,
		B:          80,
	}); err != nil {
		t.Fatalf("DispatchLED() error = %v", err)
	}

	if caller.tool != mcp.ToolSetLEDColor {
		t.Fatalf("tool = %q, want %q", caller.tool, mcp.ToolSetLEDColor)
	}
	if caller.arguments["red"] != DefaultLEDChannelMax || caller.arguments["green"] != 0 || caller.arguments["blue"] != 80 {
		t.Fatalf("arguments = %#v, want capped official red/green/blue led values", caller.arguments)
	}
}

func fixedClock(value time.Time) func() time.Time {
	return func() time.Time {
		return value
	}
}

func sequenceClock(values []time.Time) func() time.Time {
	index := 0
	return func() time.Time {
		if index >= len(values) {
			return values[len(values)-1]
		}
		value := values[index]
		index++
		return value
	}
}

type recordingToolCaller struct {
	tool      string
	arguments map[string]any
}

func (c *recordingToolCaller) CallTool(_ context.Context, name string, arguments map[string]any) (json.RawMessage, error) {
	c.tool = name
	c.arguments = arguments
	return json.RawMessage(`{}`), nil
}
