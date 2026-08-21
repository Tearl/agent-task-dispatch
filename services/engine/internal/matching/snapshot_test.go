package matching

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSnapshotRevisionReplaysSameInputAndPreservesHistory(t *testing.T) {
	repository := NewMemorySnapshotRepository()
	repository.now = func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }
	service, err := NewSnapshotService(repository, testShufflePolicy())
	if err != nil {
		t.Fatal(err)
	}
	draft := validSnapshotDraft("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	first, replay, err := service.CreateRevision(context.Background(), draft)
	if err != nil || replay || first.MatchRevision != 1 || first.CreatedAt.IsZero() {
		t.Fatalf("first revision: snapshot=%#v replay=%v err=%v", first, replay, err)
	}
	replayed, replay, err := service.CreateRevision(context.Background(), draft)
	if err != nil || !replay || !reflect.DeepEqual(first, replayed) {
		t.Fatalf("same effective input was not replayed: replay=%v err=%v", replay, err)
	}

	changed := validSnapshotDraft("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	second, replay, err := service.CreateRevision(context.Background(), changed)
	if err != nil || replay || second.MatchRevision != 2 || second.ID == first.ID || second.SeedDigest == first.SeedDigest {
		t.Fatalf("changed input did not create revision: snapshot=%#v replay=%v err=%v", second, replay, err)
	}
	latest, err := service.Latest(context.Background(), changed.Key.TaskID, changed.Key.TaskSpecHash, changed.Key.AlgorithmVersion)
	if err != nil || !reflect.DeepEqual(second, latest) {
		t.Fatalf("latest snapshot mismatch: latest=%#v err=%v", latest, err)
	}
}

func TestSnapshotRevisionIsAtomicUnderConcurrentRedelivery(t *testing.T) {
	repository := NewMemorySnapshotRepository()
	service, err := NewSnapshotService(repository, testShufflePolicy())
	if err != nil {
		t.Fatal(err)
	}
	draft := validSnapshotDraft("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	const callers = 20
	results := make([]Snapshot, callers)
	replays := make([]bool, callers)
	errorsFound := make([]error, callers)
	var group sync.WaitGroup
	for index := range callers {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			results[index], replays[index], errorsFound[index] = service.CreateRevision(context.Background(), draft)
		}(index)
	}
	group.Wait()
	created := 0
	for index := range callers {
		if errorsFound[index] != nil || results[index].MatchRevision != 1 || results[index].ID != results[0].ID {
			t.Fatalf("concurrent result %d: %#v replay=%v err=%v", index, results[index], replays[index], errorsFound[index])
		}
		if !replays[index] {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected one creation and nineteen replays, created=%d", created)
	}
}

func TestSnapshotCopiesCallerDataAndNeverPersistsSeedSecret(t *testing.T) {
	repository := NewMemorySnapshotRepository()
	policy := testShufflePolicy()
	service, err := NewSnapshotService(repository, policy)
	if err != nil {
		t.Fatal(err)
	}
	draft := validSnapshotDraft("sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	created, _, err := service.CreateRevision(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	draft.Result.Scored[0].Candidate.ProviderID = "mutated"
	latest, err := service.Latest(context.Background(), draft.Key.TaskID, draft.Key.TaskSpecHash, draft.Key.AlgorithmVersion)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Result.Scored[0].Candidate.ProviderID == "mutated" || strings.Contains(string(mustJSON(t, latest)), string(policy.SeedSecret)) {
		t.Fatalf("snapshot was mutable or leaked seed material: %#v", latest)
	}
	if !reflect.DeepEqual(created, latest) {
		t.Fatalf("stored snapshot changed after caller mutation")
	}
}

func TestSnapshotRejectsInvalidCandidateSetsAndLookup(t *testing.T) {
	service, err := NewSnapshotService(NewMemorySnapshotRepository(), testShufflePolicy())
	if err != nil {
		t.Fatal(err)
	}
	draft := validSnapshotDraft("sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	draft.Result.Qualified = append(draft.Result.Qualified, ScoredCandidate{Candidate: Candidate{AgentID: "unknown"}})
	if _, _, err = service.CreateRevision(context.Background(), draft); err == nil {
		t.Fatal("expected inconsistent qualified pool rejection")
	}
	if _, err = service.Latest(context.Background(), "task-snapshot", draft.Key.TaskSpecHash, draft.Key.AlgorithmVersion); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestHashEffectiveInputIsStable(t *testing.T) {
	left, err := HashEffectiveInput(map[string]any{"capacity": 2, "health": "healthy"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := HashEffectiveInput(map[string]any{"health": "healthy", "capacity": 2})
	if err != nil || left != right || !validSHA256(left) {
		t.Fatalf("effective input hash is unstable: left=%q right=%q err=%v", left, right, err)
	}
}

func validSnapshotDraft(effectiveInputHash string) SnapshotDraft {
	pool := shufflePool(4)
	return SnapshotDraft{
		Key: SnapshotKey{
			TaskID:             "task-snapshot",
			TaskSpecHash:       "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			AlgorithmVersion:   FairShuffleAlgorithmVersion,
			EffectiveInputHash: effectiveInputHash,
		},
		RuleVersion:  "matching-rules-v1",
		ModelVersion: "disabled",
		Result: Result{
			Scored:       slicesCloneScored(pool),
			Qualified:    slicesCloneScored(pool),
			Excluded:     []Exclusion{{AgentID: "excluded", Candidate: Candidate{AgentID: "excluded", ProviderID: "provider-excluded"}, Reasons: []Reason{{Code: "status_not_active", Message: "inactive"}}}},
			Degradations: []Degradation{{Dependency: "dense", Code: "recall_unavailable", Message: "fallback"}},
		},
	}
}

func slicesCloneScored(source []ScoredCandidate) []ScoredCandidate {
	result := make([]ScoredCandidate, len(source))
	copy(result, source)
	return result
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
