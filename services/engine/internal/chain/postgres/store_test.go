package postgres

import (
	"testing"

	chainprojection "github.com/example/agent-platform/engine/internal/chain"
)

func TestProjectedWorkNonceRequiresDecodedMonotonicValue(t *testing.T) {
	value, err := projectedWorkNonce(chainprojection.Event{Type: chainprojection.EventWorkNonce, Payload: map[string]any{"workNonce": uint64(2)}})
	if err != nil || value != uint64(2) {
		t.Fatalf("value=%v err=%v", value, err)
	}
	for _, payload := range []map[string]any{{}, {"workNonce": "2"}, {"workNonce": uint64(1)}} {
		if _, err = projectedWorkNonce(chainprojection.Event{Type: chainprojection.EventWorkNonce, Payload: payload}); err == nil {
			t.Fatalf("invalid payload accepted: %#v", payload)
		}
	}
}
