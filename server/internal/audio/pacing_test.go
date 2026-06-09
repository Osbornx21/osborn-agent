package audio

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPacerSkipsFirstFrameAndSleepsForLaterFrames(t *testing.T) {
	var slept []time.Duration
	pacer := NewPacer(PacerOptions{
		Sleep: func(_ context.Context, delay time.Duration) error {
			slept = append(slept, delay)
			return nil
		},
	})

	if err := pacer.Wait(context.Background(), 60*time.Millisecond); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}
	if len(slept) != 0 {
		t.Fatalf("first Wait() slept %v, want no sleep", slept)
	}

	if err := pacer.Wait(context.Background(), 60*time.Millisecond); err != nil {
		t.Fatalf("second Wait() error = %v", err)
	}
	if len(slept) != 1 || slept[0] != 60*time.Millisecond {
		t.Fatalf("slept = %v, want one 60ms sleep", slept)
	}
}

func TestPacerResetMakesNextFrameImmediate(t *testing.T) {
	var sleepCount int
	pacer := NewPacer(PacerOptions{
		Sleep: func(context.Context, time.Duration) error {
			sleepCount++
			return nil
		},
	})

	if err := pacer.Wait(context.Background(), 60*time.Millisecond); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}
	if err := pacer.Wait(context.Background(), 60*time.Millisecond); err != nil {
		t.Fatalf("second Wait() error = %v", err)
	}
	pacer.Reset()
	if err := pacer.Wait(context.Background(), 60*time.Millisecond); err != nil {
		t.Fatalf("post-reset Wait() error = %v", err)
	}

	if sleepCount != 1 {
		t.Fatalf("sleep count = %d, want 1", sleepCount)
	}
}

func TestTimerSleepHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := TimerSleep(ctx, time.Second)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("TimerSleep() error = %v, want context.Canceled", err)
	}
}
