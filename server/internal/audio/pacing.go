package audio

import (
	"context"
	"sync"
	"time"
)

type SleepFunc func(context.Context, time.Duration) error

type PacerOptions struct {
	Sleep SleepFunc
}

type Pacer struct {
	mu    sync.Mutex
	sleep SleepFunc
	first bool
}

func NewPacer(options PacerOptions) *Pacer {
	sleep := options.Sleep
	if sleep == nil {
		sleep = TimerSleep
	}
	return &Pacer{
		sleep: sleep,
		first: true,
	}
}

func (p *Pacer) Wait(ctx context.Context, frameDuration time.Duration) error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	if p.first {
		p.first = false
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	if frameDuration <= 0 {
		return nil
	}
	return p.sleep(ctx, frameDuration)
}

func (p *Pacer) Reset() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.first = true
}

func TimerSleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func NoopSleep(context.Context, time.Duration) error {
	return nil
}
