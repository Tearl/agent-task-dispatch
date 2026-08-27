package matching

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestMatchCategoryTagsFiltersCategoryAndRanksByTagOverlap(t *testing.T) {
	request, base := validFixture()
	request.Tags = []string{"Market", "report", "analysis", "report"}

	most := base
	most.AgentID = "agent-most"
	most.Tags = []string{"market", "REPORT", "analysis", "unrelated"}
	one := base
	one.AgentID = "agent-one"
	one.Tags = []string{"market"}
	none := base
	none.AgentID = "agent-none"
	none.Tags = nil
	wrongCategory := base
	wrongCategory.AgentID = "agent-wrong-category"
	wrongCategory.Category = "translation"

	result, err := NewService(nil, nil).MatchCategoryTags(context.Background(), request, []Candidate{none, wrongCategory, one, most})
	if err != nil {
		t.Fatal(err)
	}
	if result.Strategy != CategoryTagsStrategy || !reflect.DeepEqual(candidateIDs(result.Scored), []string{"agent-most", "agent-one", "agent-none"}) {
		t.Fatalf("unexpected category-tags ranking: %#v", result)
	}
	if result.Scored[0].Score.TagOverlap != 3 || result.Scored[1].Score.TagOverlap != 1 || result.Scored[2].Score.TagOverlap != 0 {
		t.Fatalf("unexpected tag overlaps: %#v", result.Scored)
	}
	if len(result.Excluded) != 1 || result.Excluded[0].AgentID != wrongCategory.AgentID || !hasReason(result.Excluded[0], "category_mismatch") {
		t.Fatalf("category mismatch was not excluded: %#v", result.Excluded)
	}
}

func TestMatchCategoryTagsUsesStableAgentIDFallback(t *testing.T) {
	request, base := validFixture()
	request.Tags = nil
	first := base
	first.AgentID = "agent-b"
	first.Tags = []string{"anything"}
	second := base
	second.AgentID = "agent-a"
	second.Tags = nil

	result, err := NewService(nil, nil).MatchCategoryTags(context.Background(), request, []Candidate{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidateIDs(result.Qualified), []string{"agent-a", "agent-b"}) {
		t.Fatalf("empty task tags did not fall back to stable category order: %v", candidateIDs(result.Qualified))
	}
}

func TestMatchCategoryTagsOmitsLegacyMatchGatesButKeepsOperationalGates(t *testing.T) {
	request, candidate := validFixture()
	request.Tags = []string{"market"}
	candidate.Languages = []string{"fr"}
	candidate.Capabilities = nil
	candidate.VectorVersion = "legacy"

	result, err := NewService(nil, nil).MatchCategoryTags(context.Background(), request, []Candidate{candidate})
	if err != nil || len(result.Qualified) != 1 {
		t.Fatalf("legacy match gates affected category-tags strategy: result=%#v err=%v", result, err)
	}
	candidate.Status = "paused"
	result, err = NewService(nil, nil).MatchCategoryTags(context.Background(), request, []Candidate{candidate})
	if err != nil || len(result.Qualified) != 0 || len(result.Excluded) != 1 || !hasReason(result.Excluded[0], "status_not_active") {
		t.Fatalf("operational gate was bypassed: result=%#v err=%v", result, err)
	}
}

func TestCategoryTagsSnapshotIsStableAndDoesNotShuffle(t *testing.T) {
	request, base := validFixture()
	request.Tags = []string{"market", "report"}
	candidates := make([]Candidate, 4)
	for index, id := range []string{"agent-d", "agent-c", "agent-b", "agent-a"} {
		candidates[index] = base
		candidates[index].AgentID = id
		candidates[index].ProviderID = "provider-" + id
		candidates[index].Tags = []string{"market"}
	}
	result, err := NewService(nil, nil).MatchCategoryTags(context.Background(), request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewSnapshotService(NewMemorySnapshotRepository(), testShufflePolicy())
	if err != nil {
		t.Fatal(err)
	}
	draft := SnapshotDraft{
		Key: SnapshotKey{
			TaskID:             request.TaskID,
			TaskSpecHash:       "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			AlgorithmVersion:   CategoryTagsAlgorithmVersion,
			EffectiveInputHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		RuleVersion:  "category-tags-rules-v1",
		ModelVersion: "ranking-model-not-used-v1",
		Result:       result,
	}
	created, replay, err := service.CreateRevision(context.Background(), draft)
	if err != nil || replay {
		t.Fatalf("create category-tags snapshot: replay=%v err=%v", replay, err)
	}
	replayed, replay, err := service.CreateRevision(context.Background(), draft)
	if err != nil || !replay || !reflect.DeepEqual(created, replayed) {
		t.Fatalf("category-tags snapshot was not idempotent: replay=%v err=%v", replay, err)
	}
	if created.AlgorithmVersion != CategoryTagsAlgorithmVersion || created.RuleVersion != draft.RuleVersion || created.ModelVersion != draft.ModelVersion || created.SeedKeyVersion != "not-used" || created.ExplorationTriggered {
		t.Fatalf("unexpected category-tags snapshot metadata: %#v", created)
	}
	selected := make([]string, 0, len(created.Selections))
	for _, selection := range created.Selections {
		selected = append(selected, selection.Candidate.Candidate.AgentID)
		if selection.RandomDraw != 0 || selection.Exploration || selection.ProbabilityNumerator != 1 || selection.ProbabilityDenominator != 1 {
			t.Fatalf("category-tags selection used shuffle metadata: %#v", selection)
		}
	}
	if !reflect.DeepEqual(selected, []string{"agent-a", "agent-b", "agent-c"}) {
		t.Fatalf("category-tags snapshot was shuffled: %v", selected)
	}
	if created.Result.Degradations == nil {
		t.Fatal("category-tags snapshot degradations must be an empty array, not null")
	}
	for _, candidate := range created.Result.Scored {
		if candidate.Recall == nil {
			t.Fatalf("category-tags recall evidence for %q must be an empty object, not null", candidate.Candidate.AgentID)
		}
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Result struct {
			Degradations []Degradation
			Scored       []struct {
				Recall map[string]RecallEvidence
			}
		}
	}
	if err = json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Result.Degradations == nil || len(persisted.Result.Scored) == 0 || persisted.Result.Scored[0].Recall == nil {
		t.Fatalf("category-tags persisted JSON collections are not canonical: %s", encoded)
	}
}
