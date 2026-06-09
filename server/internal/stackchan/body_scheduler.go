package stackchan

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"stackchan-gateway/internal/mcp"
)

const (
	DefaultMinCommandGap      = 160 * time.Millisecond
	DefaultMaxCommandsPerTurn = 16
)

var (
	ErrOldGeneration      = errors.New("stackchan motion command belongs to an old generation")
	ErrRateLimited        = errors.New("stackchan motion command rate limited")
	ErrTurnCommandLimit   = errors.New("stackchan motion command limit reached for generation")
	ErrSchedulerNotActive = errors.New("stackchan body scheduler generation is not active")
)

type BodySchedulerOptions struct {
	CurrentGeneration   int64
	Limits              MotionLimits
	MinCommandGap       time.Duration
	MaxCommandsPerTurn  int
	Now                 func() time.Time
	GenerationIsCurrent func(int64) bool
}

type BodyScheduler struct {
	mu                  sync.Mutex
	currentGeneration   int64
	limits              MotionLimits
	minCommandGap       time.Duration
	maxCommandsPerTurn  int
	now                 func() time.Time
	generationIsCurrent func(int64) bool
	lastServoCommandAt  time.Time
	sentByGeneration    map[int64]int
	coalesced           map[int64]MotionCommand
}

type ToolCaller interface {
	CallTool(ctx context.Context, name string, arguments map[string]any) (json.RawMessage, error)
}

func NewBodyScheduler(options BodySchedulerOptions) *BodyScheduler {
	minGap := options.MinCommandGap
	if minGap <= 0 {
		minGap = DefaultMinCommandGap
	}
	maxPerTurn := options.MaxCommandsPerTurn
	if maxPerTurn <= 0 {
		maxPerTurn = DefaultMaxCommandsPerTurn
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &BodyScheduler{
		currentGeneration:   options.CurrentGeneration,
		limits:              options.Limits.withDefaults(),
		minCommandGap:       minGap,
		maxCommandsPerTurn:  maxPerTurn,
		now:                 now,
		generationIsCurrent: options.GenerationIsCurrent,
		sentByGeneration:    make(map[int64]int),
		coalesced:           make(map[int64]MotionCommand),
	}
}

func (s *BodyScheduler) SetGeneration(generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if generation > s.currentGeneration {
		s.currentGeneration = generation
	}
}

func (s *BodyScheduler) MinCommandGap() time.Duration {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.minCommandGap
}

func (s *BodyScheduler) ScheduleMotion(command MotionCommand) (MotionCommand, error) {
	if !s.isGenerationCurrent(command.Generation) {
		return MotionCommand{}, ErrOldGeneration
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentGeneration == 0 {
		return MotionCommand{}, ErrSchedulerNotActive
	}
	if command.Generation < s.currentGeneration {
		return MotionCommand{}, ErrOldGeneration
	}
	if command.Generation > s.currentGeneration {
		s.currentGeneration = command.Generation
	}

	count := s.sentByGeneration[command.Generation]
	if count >= s.maxCommandsPerTurn {
		return MotionCommand{}, ErrTurnCommandLimit
	}

	now := s.now()
	if !s.lastServoCommandAt.IsZero() && now.Sub(s.lastServoCommandAt) < s.minCommandGap {
		s.coalesced[command.Generation] = ClampMotion(command, s.limits)
		return MotionCommand{}, ErrRateLimited
	}

	clamped := ClampMotion(command, s.limits)
	s.lastServoCommandAt = now
	s.sentByGeneration[command.Generation] = count + 1
	delete(s.coalesced, command.Generation)
	return clamped, nil
}

func (s *BodyScheduler) DispatchMotion(ctx context.Context, caller ToolCaller, command MotionCommand) (json.RawMessage, error) {
	clamped, err := s.ScheduleMotion(command)
	if err != nil {
		return nil, err
	}
	if !s.isGenerationCurrent(clamped.Generation) {
		return nil, ErrOldGeneration
	}
	return caller.CallTool(ctx, mcp.ToolSetHeadAngles, clamped.MCPArguments())
}

func (s *BodyScheduler) DispatchLED(ctx context.Context, caller ToolCaller, command LEDCommand) (json.RawMessage, error) {
	if command.Generation < s.currentGenerationValue() {
		return nil, ErrOldGeneration
	}
	if !s.isGenerationCurrent(command.Generation) {
		return nil, ErrOldGeneration
	}
	clamped := ClampLED(command)
	return caller.CallTool(ctx, mcp.ToolSetLEDColor, clamped.MCPArguments())
}

func (s *BodyScheduler) CoalescedMotion(generation int64) (MotionCommand, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	command, ok := s.coalesced[generation]
	return command, ok
}

func (s *BodyScheduler) isGenerationCurrent(generation int64) bool {
	if s.generationIsCurrent == nil {
		return true
	}
	return s.generationIsCurrent(generation)
}

func (s *BodyScheduler) currentGenerationValue() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentGeneration
}
