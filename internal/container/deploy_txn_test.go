// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package container

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openrundev/openrun/internal/types"
)

func TestDeployTxnRollbackAllReverseOrder(t *testing.T) {
	ctx := context.Background()
	d := NewDeployTxn()

	var order []string
	d.Register(types.AppId("app1"), ContainerName("clc-app1"), func(context.Context) error { order = append(order, "app1"); return nil }, nil)
	d.Register(types.AppId("app2"), ContainerName("clc-app2"), func(context.Context) error { order = append(order, "app2"); return nil }, nil)
	d.Register(types.AppId("app3"), ContainerName("clc-app3"), func(context.Context) error { order = append(order, "app3"); return nil }, nil)

	if err := d.RollbackAll(ctx); err != nil {
		t.Fatalf("RollbackAll error: %v", err)
	}
	want := []string{"app3", "app2", "app1"}
	if len(order) != len(want) {
		t.Fatalf("order=%v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order=%v, want %v", order, want)
		}
	}

	order = nil
	if err := d.RollbackAll(ctx); err != nil {
		t.Fatalf("second RollbackAll error: %v", err)
	}
	if len(order) != 0 {
		t.Fatalf("second RollbackAll ran %v, want none", order)
	}
}

func TestDeployTxnRollbackAllJoinsErrors(t *testing.T) {
	ctx := context.Background()
	d := NewDeployTxn()
	errA := errors.New("boom-a")
	errB := errors.New("boom-b")
	d.Register(types.AppId("app1"), ContainerName("clc-app1"), func(context.Context) error { return errA }, nil)
	d.Register(types.AppId("app2"), ContainerName("clc-app2"), func(context.Context) error { return errB }, nil)

	err := d.RollbackAll(ctx)
	if err == nil {
		t.Fatal("expected joined error")
	}
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Fatalf("joined error missing parts: %v", err)
	}
}

func TestDeployTxnCommitRunsCommitActions(t *testing.T) {
	ctx := context.Background()
	d := NewDeployTxn()

	var committed []string
	rolledBack := false
	d.Register(types.AppId("app1"), ContainerName("clc-app1"),
		func(context.Context) error { rolledBack = true; return nil },
		func(context.Context) error { committed = append(committed, "app1"); return nil })
	d.Register(types.AppId("app2"), ContainerName("clc-app2"), nil,
		func(context.Context) error { committed = append(committed, "app2"); return nil })

	if err := d.CommitAll(ctx); err != nil {
		t.Fatalf("CommitAll error: %v", err)
	}
	if len(committed) != 2 {
		t.Fatalf("committed=%v, want both apps", committed)
	}
	if rolledBack {
		t.Fatal("commit must not run rollback actions")
	}
	if err := d.RollbackAll(ctx); err != nil {
		t.Fatalf("RollbackAll after commit: %v", err)
	}
	if rolledBack {
		t.Fatal("rollback ran after the stack was drained by commit")
	}
}

// Container names must survive CommitAll/RollbackAll draining the entries:
// they protect in-flight containers (e.g. from the stale sweeper) until the
// owning scope unregisters the whole transaction.
func TestDeployTxnContainerNamesSurviveDrain(t *testing.T) {
	ctx := context.Background()
	d := NewDeployTxn()
	d.Register(types.AppId("app1"), ContainerName("clc-app1"), nil, nil)
	d.Register(types.AppId("app2"), "", nil, nil) // no container name (e.g. command lifetime)

	if got := d.Len(); got != 2 {
		t.Fatalf("Len=%d, want 2", got)
	}
	names := d.ContainerNames()
	if len(names) != 1 || names[0] != "clc-app1" {
		t.Fatalf("names=%v, want [clc-app1]", names)
	}

	if err := d.CommitAll(ctx); err != nil {
		t.Fatalf("CommitAll error: %v", err)
	}
	names = d.ContainerNames()
	if len(names) != 1 || names[0] != "clc-app1" {
		t.Fatalf("names after drain=%v, want [clc-app1]", names)
	}
}

func TestClusterRollbackClean(t *testing.T) {
	base := errors.New("verify failed")

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"plain error is clean (db rollback reverts)", base, true},
		{"clean cluster rollback", &DeployRollbackError{Err: base, Available: true}, true},
		{"snapshot unavailable", &DeployRollbackError{Err: base, Available: false}, false},
		{"rollback failed", &DeployRollbackError{Err: base, Available: true, RollbackErr: errors.New("restore failed")}, false},
		{"wrapped clean", &DeployRollbackError{Err: base, Available: true}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClusterRollbackClean(tc.err); got != tc.want {
				t.Fatalf("ClusterRollbackClean=%v, want %v", got, tc.want)
			}
		})
	}

	de := &DeployRollbackError{Err: base, Available: false}
	if !errors.Is(de, base) {
		t.Fatal("DeployRollbackError should unwrap to the base error")
	}
}

func TestDeployTxnCommitBudgetAccumulatesAndSurvivesDrain(t *testing.T) {
	d := NewDeployTxn()
	if d.CommitBudget() != 0 {
		t.Fatalf("new txn budget = %v, want 0", d.CommitBudget())
	}
	d.AddCommitBudget(30 * time.Second)
	d.AddCommitBudget(45 * time.Second)
	if got := d.CommitBudget(); got != 75*time.Second {
		t.Fatalf("budget = %v, want 75s", got)
	}
	d.Register("app1", "clc-app1", nil, func(context.Context) error { return nil })
	if err := d.CommitAll(context.Background()); err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	if got := d.CommitBudget(); got != 75*time.Second {
		t.Fatalf("budget after CommitAll = %v, want 75s (not drained)", got)
	}
}

func TestDeployTxnRegisterImageDedupesAndSkipsEmpty(t *testing.T) {
	d := NewDeployTxn()
	d.RegisterImage("")
	d.RegisterImage("cli-a:1")
	d.RegisterImage("cli-b:1")
	d.RegisterImage("cli-a:1")

	got := d.ImageNames()
	want := []ImageName{"cli-a:1", "cli-b:1"}
	if len(got) != len(want) {
		t.Fatalf("ImageNames=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ImageNames=%v, want %v", got, want)
		}
	}
	// Images are not drained by CommitAll: the owning scope keeps them
	// protected until it unregisters the transaction
	if err := d.CommitAll(context.Background()); err != nil {
		t.Fatalf("CommitAll error: %v", err)
	}
	if len(d.ImageNames()) != 2 {
		t.Fatalf("ImageNames after CommitAll=%v, want 2 entries", d.ImageNames())
	}
	// Registered images are pinned until the owner releases them
	if !imagePinned("cli-a:1") || !imagePinned("localhost/cli-b:1") {
		t.Fatalf("registered images are not pinned")
	}
	d.ReleaseImages()
	if imagePinned("cli-a:1") || imagePinned("cli-b:1") {
		t.Fatalf("images still pinned after ReleaseImages")
	}
}

func TestPinImageAndCheckReleasesOnErrorAndCountsPins(t *testing.T) {
	ctx := context.Background()
	failing := &pinTestManager{err: errors.New("daemon down")}
	if _, _, err := PinImageAndCheck(ctx, failing, "cli-p:1"); err == nil {
		t.Fatalf("PinImageAndCheck with a failing manager returned no error")
	}
	if imagePinned("cli-p:1") {
		t.Fatalf("image pinned after a failed check")
	}

	ok := &pinTestManager{exists: true}
	exists, release1, err := PinImageAndCheck(ctx, ok, "cli-p:1")
	if err != nil || !exists {
		t.Fatalf("PinImageAndCheck = %v, %v; want exists", exists, err)
	}
	release2 := PinImage("cli-p:1")
	release1()
	release1() // idempotent
	if !imagePinned("cli-p:1") {
		t.Fatalf("image unpinned while a pin is still held")
	}
	release2()
	if imagePinned("cli-p:1") {
		t.Fatalf("image still pinned after the last release")
	}
}

type pinTestManager struct {
	ContainerManager
	exists bool
	err    error
}

func (m *pinTestManager) ImageExists(context.Context, ImageName) (bool, error) {
	return m.exists, m.err
}

func TestImagePinsFromContextOutliveTheStartAndBumpTheGeneration(t *testing.T) {
	ctx, pins := ContextWithImagePins(context.Background())
	generation := ImageGeneration()

	RegisterImage(ctx, "cli-lazy:a")
	RegisterImage(ctx, "cli-lazy:a")
	RegisterImage(ctx, "")
	pins.Pin("cli-lazy:b")
	if names := pins.Names(); len(names) != 2 || names[0] != "cli-lazy:a" || names[1] != "cli-lazy:b" {
		t.Fatalf("pinned names = %v, want [cli-lazy:a cli-lazy:b]", names)
	}
	if !imagePinned("cli-lazy:a") || !imagePinned("localhost/cli-lazy:b") {
		t.Fatal("registered images are not pinned")
	}
	if ImageGeneration() != generation {
		t.Fatal("pinning moved the image generation")
	}

	// With a deploy transaction in the context, the transaction takes the pin
	txn := NewDeployTxn()
	RegisterImage(ContextWithDeployTxn(ctx, txn), "cli-lazy:txn")
	if names := txn.ImageNames(); len(names) != 1 || names[0] != "cli-lazy:txn" {
		t.Fatalf("txn images = %v, want [cli-lazy:txn]", names)
	}
	if len(pins.Names()) != 2 {
		t.Fatal("the pin set took the deploy transaction's image")
	}
	txn.ReleaseImages()

	pins.Release()
	pins.Release()
	if imagePinned("cli-lazy:a") || imagePinned("cli-lazy:b") || imagePinned("cli-lazy:txn") {
		t.Fatal("images still pinned after release")
	}
	if got := ImageGeneration(); got != generation+3 {
		t.Fatalf("image generation = %d, want %d (one per released pin)", got, generation+3)
	}
	if names := pins.Names(); len(names) != 2 {
		t.Fatalf("pinned names after release = %v, want the two names kept", names)
	}
	if RegisterImage(context.Background(), "cli-lazy:none"); imagePinned("cli-lazy:none") {
		t.Fatal("RegisterImage pinned outside an operation")
	}
}
