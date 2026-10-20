// Copyright (c) OpenRun Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/openrundev/openrun/internal/types"
)

// ErrDeployInProgress refuses a request to an app whose stop-first deploy
// has held requests for longer than its wait budget (the deploy still owns
// the app: initializing it now would start the previous version again),
// and a second deploy of the app which overlaps a stop-first deploy
var ErrDeployInProgress = errors.New("app deployment in progress, retry the request")

// swapHold is one app's request gate during a stop-first deploy
// (container.blue_green_deploy, see app.DeploySwapGate): the previous
// version's container is stopped and the new one not serving yet. Requests
// wait on done until the deadline, after which they are refused. The hold
// belongs to one owner (an operation's deploy transaction, a lazy start);
// the owner's further swaps of the app (the pre-pass of a reload and its
// in-transaction reload) share it, and it is released when the last ends.
// Another owner's swap waits for the release: a deploy never replaces the
// containers another deploy is replacing
type swapHold struct {
	done     chan struct{}
	deadline time.Time
	owner    any
	swaps    int
}

// swapGates holds the open request gates by app
type swapGates struct {
	mu     sync.Mutex
	holds  map[types.AppPathDomain]*swapHold
	epochs map[types.AppPathDomain]uint64 // bumped on every begin and end
	// appLocks serialize a begin with a live instance's container stop
	// (withoutSwap), per app; taken before mu, never while holding it
	appLocks map[types.AppPathDomain]*sync.Mutex
}

func (g *swapGates) appLock(pathDomain types.AppPathDomain) *sync.Mutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.appLocks == nil {
		g.appLocks = map[types.AppPathDomain]*sync.Mutex{}
	}
	lock, ok := g.appLocks[pathDomain]
	if !ok {
		lock = &sync.Mutex{}
		g.appLocks[pathDomain] = lock
	}
	return lock
}

// withoutSwap runs fn if no swap of the app is in progress and its epoch is
// still epoch, holding the app's lock so a begin waits for fn to return.
// Reports whether fn ran
func (g *swapGates) withoutSwap(pathDomain types.AppPathDomain, epoch uint64, fn func()) bool {
	lock := g.appLock(pathDomain)
	lock.Lock()
	defer lock.Unlock()
	if held, current := g.state(pathDomain); held || current != epoch {
		return false
	}
	fn()
	return true
}

// begin opens the app's gate for owner, or joins the gate owner already
// holds (extending its deadline). A gate held by another owner is waited
// for until it is released, at most maxWait, when waitForOthers is set;
// otherwise, and past maxWait, ErrDeployInProgress is returned and the
// gate is left as it is
func (g *swapGates) begin(ctx context.Context, pathDomain types.AppPathDomain, owner any, maxWait time.Duration, waitForOthers bool) error {
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	for {
		done := g.tryBegin(pathDomain, owner, maxWait)
		if done == nil {
			return nil
		}
		if !waitForOthers {
			return fmt.Errorf("another deployment of app %s is in progress: %w", pathDomain, ErrDeployInProgress)
		}
		select {
		case <-done:
		case <-timer.C:
			return fmt.Errorf("another deployment of app %s is still in progress after %s: %w", pathDomain, maxWait, ErrDeployInProgress)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// tryBegin opens or joins the app's gate for owner and returns nil, or
// returns the release channel of the gate another owner holds
func (g *swapGates) tryBegin(pathDomain types.AppPathDomain, owner any, maxWait time.Duration) <-chan struct{} {
	lock := g.appLock(pathDomain)
	lock.Lock()
	defer lock.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.holds == nil {
		g.holds = map[types.AppPathDomain]*swapHold{}
		g.epochs = map[types.AppPathDomain]uint64{}
	}
	deadline := time.Now().Add(maxWait)
	if hold, ok := g.holds[pathDomain]; ok {
		if hold.owner != owner {
			return hold.done
		}
		g.epochs[pathDomain]++
		hold.swaps++
		if deadline.After(hold.deadline) {
			hold.deadline = deadline
		}
		return nil
	}
	g.epochs[pathDomain]++
	g.holds[pathDomain] = &swapHold{done: make(chan struct{}), deadline: deadline, owner: owner, swaps: 1}
	return nil
}

// end releases one swap of the app's gate; the gate opens when the owner's
// last swap ends. Returns whether the gate was released
func (g *swapGates) end(pathDomain types.AppPathDomain) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	hold, ok := g.holds[pathDomain]
	if !ok {
		return false
	}
	g.epochs[pathDomain]++
	hold.swaps--
	if hold.swaps > 0 {
		return false
	}
	close(hold.done)
	delete(g.holds, pathDomain)
	return true
}

// state reports whether the app's gate is held, and the app's swap epoch
func (g *swapGates) state(pathDomain types.AppPathDomain) (bool, uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, held := g.holds[pathDomain]
	return held, g.epochs[pathDomain]
}

// wait blocks while the app's gate is held, until it is released (nil), the
// hold's deadline passes (ErrDeployInProgress, the hold stays) or ctx is
// done. A release is followed by a new look at the gate: a deploy waiting
// for the release may have taken it, and the request then waits for that
// deploy too
func (g *swapGates) wait(ctx context.Context, pathDomain types.AppPathDomain) error {
	for {
		g.mu.Lock()
		hold, ok := g.holds[pathDomain]
		var deadline time.Time
		if ok {
			deadline = hold.deadline // extended under mu by the owner's later swaps
		}
		g.mu.Unlock()
		if !ok {
			return nil
		}
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-hold.done:
			timer.Stop()
		case <-timer.C:
			return ErrDeployInProgress
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

// BeginSwap implements app.DeploySwapGate: requests initializing the app
// are held from here until the matching EndSwap, refused after maxWait. An
// overlapping deploy of the app by another owner is waited for first
func (s *Server) BeginSwap(ctx context.Context, pathDomain types.AppPathDomain, owner any, maxWait time.Duration, waitForOthers bool) error {
	if err := s.swapGates.begin(ctx, pathDomain, owner, maxWait, waitForOthers); err != nil {
		return err
	}
	s.Info().Msgf("Holding requests to app %s while its container is replaced", pathDomain)
	return nil
}

// EndSwap implements app.DeploySwapGate: held requests proceed once every
// swap of the app ended (to the new version after a committed deploy, as
// the app store is refreshed before the deploy's commit actions run, or to
// the restored previous version)
func (s *Server) EndSwap(pathDomain types.AppPathDomain) {
	if s.swapGates.end(pathDomain) {
		s.Info().Msgf("Releasing requests to app %s", pathDomain)
	}
}

// SwapState implements app.DeploySwapGate
func (s *Server) SwapState(pathDomain types.AppPathDomain) (bool, uint64) {
	return s.swapGates.state(pathDomain)
}

// WithoutSwap implements app.DeploySwapGate
func (s *Server) WithoutSwap(pathDomain types.AppPathDomain, epoch uint64, fn func()) bool {
	return s.swapGates.withoutSwap(pathDomain, epoch, fn)
}

// PreviousVersionStopped implements app.DeploySwapGate: the cached
// instance proxies to the previous version's container, which a rolled
// back deploy stopped. Dropping the instance has the next request rebuild
// it: that reload finds the version's container stopped and starts it
// again (on a new host port), waiting for its health check
func (s *Server) PreviousVersionStopped(pathDomain types.AppPathDomain) {
	s.Info().Msgf("Deploy of app %s rolled back, its previous version starts again on the next request", pathDomain)
	s.apps.ClearApps([]types.AppPathDomain{pathDomain})
}

// waitSwap holds a request initializing the app while a stop-first deploy
// of it is in progress, see swapGates.wait
func (s *Server) waitSwap(ctx context.Context, pathDomain types.AppPathDomain) error {
	return s.swapGates.wait(ctx, pathDomain)
}
