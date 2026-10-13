package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Regression (2026-10-13): scheduleAt used to block, so only the first job armed in a
// goroutine ever ran. Several jobs armed one after another must ALL fire.
func TestScheduleAtArmsEveryJob(t *testing.T) {
	old := schedAfter
	schedAfter = func(time.Duration) <-chan time.Time { return time.After(5 * time.Millisecond) }
	defer func() { schedAfter = old }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var a, b, c int32
	scheduleAt(ctx, 23, 0, func() { atomic.AddInt32(&a, 1) })
	scheduleAt(ctx, 23, 10, func() { atomic.AddInt32(&b, 1) }) // never reached before the fix
	scheduleAt(ctx, 23, 20, func() { atomic.AddInt32(&c, 1) })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&a) > 0 && atomic.LoadInt32(&b) > 0 && atomic.LoadInt32(&c) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("not every scheduled job fired: a=%d b=%d c=%d", a, b, c)
}

func TestScheduleAtStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var n int32
	scheduleAt(ctx, 3, 0, func() { atomic.AddInt32(&n, 1) }) // fires in hours; must not fire
	cancel()
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&n) != 0 {
		t.Fatal("job fired after cancel")
	}
}
