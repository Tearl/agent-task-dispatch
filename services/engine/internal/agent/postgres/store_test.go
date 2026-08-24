package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/example/agent-platform/engine/internal/agent"
)

func TestHealthProbeAdmissionRejectsImmediatelyBeforeDatabaseWork(t *testing.T) {
	store := &Store{healthProbeSlots: make(chan struct{}, healthProbeConcurrency)}
	releaseFirst, err := store.acquireHealthProbeSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.acquireHealthProbeSlot(context.Background()); !errors.Is(err, agent.ErrHealthCheckUnavailable) {
		t.Fatalf("second health probe was not rejected immediately: %v", err)
	}

	releaseFirst()
	releaseSecond, err := store.acquireHealthProbeSlot(context.Background())
	if err != nil {
		t.Fatalf("second health probe failed after release: %v", err)
	}
	releaseSecond()
}

func TestHealthProbeAdmissionHonorsCancellation(t *testing.T) {
	store := &Store{healthProbeSlots: make(chan struct{}, healthProbeConcurrency)}
	release, err := store.acquireHealthProbeSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = store.acquireHealthProbeSlot(ctx); err == nil {
		t.Fatal("cancelled health probe waited for admission")
	}
}
