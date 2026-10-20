// Copyright (c) OpenRun Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openrundev/openrun/internal/types"
)

func TestSwapGatesHoldRequestsUntilTheLastOwnerEnds(t *testing.T) {
	t.Parallel()

	g := &swapGates{}
	pd := types.AppPathDomain{Path: "/app"}
	if err := g.wait(context.Background(), pd); err != nil {
		t.Fatalf("wait on a closed gate: %v", err)
	}
	if held, _ := g.state(pd); held {
		t.Fatal("state reports a hold before begin")
	}

	mustBegin(t, g, pd, "op")
	mustBegin(t, g, pd, "op") // the owner's second swap joins its hold
	if held, _ := g.state(pd); !held {
		t.Fatal("state does not report the hold")
	}
	released := make(chan error, 1)
	go func() { released <- g.wait(context.Background(), pd) }()
	if g.end(pd) {
		t.Fatal("the first end released a gate with another owner")
	}
	select {
	case err := <-released:
		t.Fatalf("wait returned while an owner remains: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if !g.end(pd) {
		t.Fatal("the last end did not release the gate")
	}
	select {
	case err := <-released:
		if err != nil {
			t.Fatalf("wait after end: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after the last end")
	}
	if g.end(pd) {
		t.Fatal("end of a closed gate returned true")
	}
	// Another app's gate does not hold this app
	mustBegin(t, g, types.AppPathDomain{Path: "/other"}, "op")
	if err := g.wait(context.Background(), pd); err != nil {
		t.Fatalf("wait held by another app's gate: %v", err)
	}
}

func TestSwapGatesEpochMovesOnEveryBeginAndEnd(t *testing.T) {
	t.Parallel()

	g := &swapGates{}
	pd := types.AppPathDomain{Path: "/app"}
	_, e0 := g.state(pd)
	mustBegin(t, g, pd, "op")
	_, e1 := g.state(pd)
	g.end(pd)
	held, e2 := g.state(pd)
	if held || e0 == e1 || e1 == e2 {
		t.Fatalf("epochs %d %d %d held=%v, want distinct epochs and no hold", e0, e1, e2, held)
	}
}

func TestSwapGatesRefuseAfterTheDeadlineAndKeepTheHold(t *testing.T) {
	t.Parallel()

	g := &swapGates{}
	pd := types.AppPathDomain{Path: "/app"}
	if err := g.begin(context.Background(), pd, "op", 20*time.Millisecond, true); err != nil {
		t.Fatalf("begin: %v", err)
	}
	start := time.Now()
	if err := g.wait(context.Background(), pd); !errors.Is(err, ErrDeployInProgress) {
		t.Fatalf("wait at the deadline = %v, want ErrDeployInProgress", err)
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Fatal("wait returned before the deadline")
	}
	// The hold stays with the deploy: a later request is refused at once
	// rather than initializing the app, until the deploy ends the swap
	if err := g.wait(context.Background(), pd); !errors.Is(err, ErrDeployInProgress) {
		t.Fatalf("wait after the deadline = %v, want ErrDeployInProgress", err)
	}
	// The owner's next swap extends the deadline
	mustBegin(t, g, pd, "op")
	g.end(pd)
	released := make(chan error, 1)
	go func() { released <- g.wait(context.Background(), pd) }()
	select {
	case err := <-released:
		t.Fatalf("wait returned %v while the extended hold remains", err)
	case <-time.After(50 * time.Millisecond):
	}
	g.end(pd)
	if err := <-released; err != nil {
		t.Fatalf("wait after the release: %v", err)
	}

	mustBegin(t, g, pd, "op")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.wait(ctx, pd); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait with an ended context = %v, want context.Canceled", err)
	}
	g.end(pd)
}

func TestSwapGatesWithoutSwapSerializesWithBegin(t *testing.T) {
	t.Parallel()

	g := &swapGates{}
	pd := types.AppPathDomain{Path: "/app"}
	_, epoch := g.state(pd)

	// A stale epoch or an active swap skips fn
	mustBegin(t, g, pd, "op")
	if g.withoutSwap(pd, epoch, func() { t.Fatal("fn ran during a swap") }) {
		t.Fatal("withoutSwap reported fn ran during a swap")
	}
	g.end(pd)
	if g.withoutSwap(pd, epoch, func() { t.Fatal("fn ran with a stale epoch") }) {
		t.Fatal("withoutSwap reported fn ran with a stale epoch")
	}

	// fn runs with the current epoch, and a begin waits for it to return
	_, epoch = g.state(pd)
	entered := make(chan struct{})
	release := make(chan struct{})
	ran := make(chan bool, 1)
	go func() {
		ran <- g.withoutSwap(pd, epoch, func() {
			close(entered)
			<-release
		})
	}()
	<-entered
	began := make(chan struct{})
	go func() {
		mustBegin(t, g, pd, "op")
		close(began)
	}()
	select {
	case <-began:
		t.Fatal("begin returned while fn was running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if !<-ran {
		t.Fatal("withoutSwap reported fn did not run")
	}
	select {
	case <-began:
	case <-time.After(5 * time.Second):
		t.Fatal("begin did not return after fn")
	}
	g.end(pd)
}

func mustBegin(t *testing.T, g *swapGates, pd types.AppPathDomain, owner any) {
	t.Helper()
	if err := g.begin(context.Background(), pd, owner, time.Minute, true); err != nil {
		t.Fatalf("begin for %v: %v", owner, err)
	}
}

func TestSwapGatesSerializeDeploysOfOneApp(t *testing.T) {
	t.Parallel()

	g := &swapGates{}
	pd := types.AppPathDomain{Path: "/app"}
	mustBegin(t, g, pd, "op1")

	// A lazy start does not wait for another owner's deploy: refused
	err := g.begin(context.Background(), pd, "lazy", time.Minute, false)
	if !errors.Is(err, ErrDeployInProgress) {
		t.Fatalf("begin of a lazy start during a deploy = %v, want ErrDeployInProgress", err)
	}
	// Another operation waits for the release, at most its budget
	start := time.Now()
	err = g.begin(context.Background(), pd, "op2", 30*time.Millisecond, true)
	if !errors.Is(err, ErrDeployInProgress) || time.Since(start) < 30*time.Millisecond {
		t.Fatalf("begin of a second deploy = %v after %s, want ErrDeployInProgress at the budget", err, time.Since(start))
	}
	if held, _ := g.state(pd); !held {
		t.Fatal("the refused begin dropped the first owner's hold")
	}
	began := make(chan error, 1)
	go func() { began <- g.begin(context.Background(), pd, "op2", time.Minute, true) }()
	select {
	case err := <-began:
		t.Fatalf("second deploy began (%v) while the first holds the gate", err)
	case <-time.After(50 * time.Millisecond):
	}
	// The first owner's second swap still joins; the gate is released when
	// both of its swaps end, and the waiting deploy then takes it
	mustBegin(t, g, pd, "op1")
	g.end(pd)
	g.end(pd)
	select {
	case err := <-began:
		if err != nil {
			t.Fatalf("second deploy after the release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second deploy did not begin after the release")
	}
	if held, _ := g.state(pd); !held {
		t.Fatal("the second deploy holds no gate")
	}
	// Its hold is its own: the first owner now waits
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.begin(ctx, pd, "op1", time.Minute, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("begin with an ended context = %v, want context.Canceled", err)
	}
	if !g.end(pd) {
		t.Fatal("the second deploy's end did not release the gate")
	}
}

func TestSwapGatesWaitLooksAgainAfterARelease(t *testing.T) {
	t.Parallel()

	g := &swapGates{}
	pd := types.AppPathDomain{Path: "/app"}
	mustBegin(t, g, pd, "op1")
	released := make(chan error, 1)
	go func() { released <- g.wait(context.Background(), pd) }()
	// The first deploy ends and a second one takes the gate before the
	// waiter looks again (the two happen under the gate's lock here, as a
	// begin waiting for the release may win the race with the waiter)
	g.mu.Lock()
	first := g.holds[pd]
	g.holds[pd] = &swapHold{done: make(chan struct{}), deadline: time.Now().Add(time.Minute), owner: "op2", swaps: 1}
	close(first.done)
	g.mu.Unlock()
	select {
	case err := <-released:
		t.Fatalf("wait returned %v while the second deploy holds the gate", err)
	case <-time.After(50 * time.Millisecond):
	}
	if !g.end(pd) {
		t.Fatal("the second deploy's end did not release the gate")
	}
	select {
	case err := <-released:
		if err != nil {
			t.Fatalf("wait after the second release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after the second release")
	}
}
