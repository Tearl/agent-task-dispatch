package matching

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type recallStub struct {
	channel string
	hits    []RecallHit
	err     error
}

func (stub recallStub) Channel() string { return stub.channel }
func (stub recallStub) Recall(context.Context, Request, []Candidate, int) ([]RecallHit, error) {
	return stub.hits, stub.err
}

type adjusterStub struct {
	delta int
	err   error
}

func (stub adjusterStub) Adjust(context.Context, Request, Candidate, ScoreBreakdown) (int, error) {
	return stub.delta, stub.err
}

func TestMatchDegradesWithoutBypassingHardFiltersOrQualityFloor(t *testing.T) {
	request, eligible := validFixture()
	excluded := eligible
	excluded.AgentID = "agent-excluded"
	excluded.Status = "paused"
	lowQuality := eligible
	lowQuality.AgentID = "agent-low"
	lowQuality.Reputation = Reputation{}
	lowQuality.Tags = nil
	lowQuality.Capabilities = []string{"research", "report"}

	service := NewServiceWithAdapters([]RecallAdapter{
		recallStub{channel: ChannelDense, err: errors.New("qdrant unavailable")},
		LexicalRecall{},
		recallStub{channel: ChannelExact, hits: []RecallHit{
			{AgentID: eligible.AgentID, Relevance: 10_000},
			{AgentID: excluded.AgentID, Relevance: 10_000},
			{AgentID: lowQuality.AgentID, Relevance: 10_000},
		}},
	}, adjusterStub{err: errors.New("model unavailable")})

	result, err := service.Match(context.Background(), request, []Candidate{excluded, lowQuality, eligible})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Excluded) != 1 || result.Excluded[0].AgentID != excluded.AgentID || !hasReason(result.Excluded[0], "status_not_active") {
		t.Fatalf("hard exclusion was lost: %#v", result.Excluded)
	}
	if containsScored(result.Scored, excluded.AgentID) {
		t.Fatalf("recall reintroduced a hard-filtered candidate: %#v", result.Scored)
	}
	if len(result.Qualified) != 1 || result.Qualified[0].Candidate.AgentID != eligible.AgentID {
		t.Fatalf("quality floor was relaxed during degradation: %#v", result.Qualified)
	}
	for _, candidate := range result.Scored {
		if candidate.Score.ModelDelta != 0 {
			t.Fatalf("model failure did not use fixed zero fallback: %#v", candidate.Score)
		}
	}
	for _, expected := range []string{"invalid_recall_hit", "model_unavailable", "recall_unavailable"} {
		if !hasDegradation(result.Degradations, expected) {
			t.Fatalf("missing degradation %q: %#v", expected, result.Degradations)
		}
	}
}

func TestMatchIsDeterministicAndScoresAtMostOneHundredCandidates(t *testing.T) {
	request, base := validFixture()
	candidates := make([]Candidate, 0, 110)
	for index := range 110 {
		candidate := base
		candidate.AgentID = agentID3(index)
		candidates = append(candidates, candidate)
	}
	service := NewService(nil, nil)

	first, err := service.Match(context.Background(), request, candidates)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Match(context.Background(), request, reverseCandidates(candidates))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Scored) != MaxScoredCandidates || len(first.Qualified) != MaxQualifiedPool {
		t.Fatalf("unexpected caps: scored=%d qualified=%d", len(first.Scored), len(first.Qualified))
	}
	if !reflect.DeepEqual(candidateIDs(first.Scored), candidateIDs(second.Scored)) || !reflect.DeepEqual(candidateIDs(first.Qualified), candidateIDs(second.Qualified)) {
		t.Fatalf("same input set produced unstable output\nfirst=%v\nsecond=%v", candidateIDs(first.Scored), candidateIDs(second.Scored))
	}
}

func TestMatchRejectsInvalidRequestBeforeFiltering(t *testing.T) {
	request, candidate := validFixture()
	request.FormalBudget = "01"
	if _, err := NewService(nil, nil).Match(context.Background(), request, []Candidate{candidate}); err == nil {
		t.Fatal("expected invalid task budget to be rejected")
	}
}

func containsScored(candidates []ScoredCandidate, agentID string) bool {
	for _, candidate := range candidates {
		if candidate.Candidate.AgentID == agentID {
			return true
		}
	}
	return false
}

func hasDegradation(degradations []Degradation, code string) bool {
	for _, degradation := range degradations {
		if degradation.Code == code {
			return true
		}
	}
	return false
}

func candidateIDs(candidates []ScoredCandidate) []string {
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.Candidate.AgentID)
	}
	return result
}

func reverseCandidates(source []Candidate) []Candidate {
	result := make([]Candidate, len(source))
	for index := range source {
		result[len(source)-1-index] = source[index]
	}
	return result
}

func agentID3(index int) string {
	const digits = "0123456789"
	return "agent-" + string([]byte{digits[index/100], digits[index/10%10], digits[index%10]})
}
