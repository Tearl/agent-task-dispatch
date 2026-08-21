package matching

import (
	"fmt"
	"reflect"
	"testing"
)

func TestFairShuffleIsStableAcrossInputOrder(t *testing.T) {
	pool := shufflePool(8)
	context := validSeedContext("task-stable", 1)
	policy := testShufflePolicy()

	first, err := FairShuffle(pool, context, policy)
	if err != nil {
		t.Fatal(err)
	}
	reversed := slicesReverse(pool)
	second, err := FairShuffle(reversed, context, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("input order changed deterministic result:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if len(first.Selections) != 3 || !validSHA256(first.SeedDigest) || first.SeedKeyVersion != policy.SeedKeyVersion {
		t.Fatalf("incomplete shuffle result: %#v", first)
	}
}

func TestFairShuffleIsWeightedWithoutReplacementAndEnforcesProviderCap(t *testing.T) {
	pool := shufflePool(5)
	pool[1].Candidate.ProviderID = pool[0].Candidate.ProviderID
	result, err := FairShuffle(pool, validSeedContext("task-provider-cap", 1), testShufflePolicy())
	if err != nil {
		t.Fatal(err)
	}
	seenAgents := make(map[string]struct{})
	seenProviders := make(map[string]struct{})
	for index, selection := range result.Selections {
		if selection.Position != index+1 || selection.Weight != selectionWeight(selection.Candidate.Score.RankingScore) || selection.ProbabilityNumerator != selection.Weight || selection.ProbabilityDenominator < selection.Weight {
			t.Fatalf("invalid selection audit data: %#v", selection)
		}
		if _, duplicate := seenAgents[selection.Candidate.Candidate.AgentID]; duplicate {
			t.Fatalf("agent selected twice: %#v", result.Selections)
		}
		if _, duplicate := seenProviders[selection.Candidate.Candidate.ProviderID]; duplicate {
			t.Fatalf("provider cap bypassed: %#v", result.Selections)
		}
		seenAgents[selection.Candidate.Candidate.AgentID] = struct{}{}
		seenProviders[selection.Candidate.Candidate.ProviderID] = struct{}{}
	}
}

func TestFairShuffleExplorationRateAndPositionAreDeterministic(t *testing.T) {
	pool := shufflePool(5)
	for index := range pool {
		pool[index].Candidate.ExposureCount = ExplorationExposureLimit
		pool[index].Candidate.EffectiveSamples = ExplorationSampleLimit
	}
	pool[4].Candidate.ExposureCount = 0
	triggered := 0
	applied := 0
	for index := range 10_000 {
		result, err := FairShuffle(pool, validSeedContext(fmt.Sprintf("task-explore-%d", index), 1), testShufflePolicy())
		if err != nil {
			t.Fatal(err)
		}
		if result.ExplorationTriggered {
			triggered++
		}
		for _, selection := range result.Selections {
			if selection.Exploration {
				applied++
				if selection.Position != 3 || selection.Candidate.Candidate.AgentID != pool[4].Candidate.AgentID {
					t.Fatalf("exploration escaped third position or exploration pool: %#v", selection)
				}
			}
		}
	}
	if triggered < 1_400 || triggered > 1_600 {
		t.Fatalf("15%% deterministic exploration out of tolerance: %d/10000", triggered)
	}
	if applied > triggered {
		t.Fatalf("exploration applied more often than triggered: triggered=%d applied=%d", triggered, applied)
	}
}

func TestFairShuffleGivesHigherScoreMoreExposure(t *testing.T) {
	pool := shufflePool(2)
	pool[0].Score.RankingScore = 70
	pool[0].Score.RuleScore = 70
	pool[1].Score.RankingScore = 60
	pool[1].Score.RuleScore = 60
	policy := testShufflePolicy()
	policy.SelectionLimit = 1
	highSelections := 0
	for index := range 2_000 {
		result, err := FairShuffle(pool, validSeedContext(fmt.Sprintf("task-weight-%d", index), 1), policy)
		if err != nil {
			t.Fatal(err)
		}
		if result.Selections[0].Candidate.Candidate.AgentID == pool[0].Candidate.AgentID {
			highSelections++
		}
	}
	if highSelections < 1_750 {
		t.Fatalf("weighted draw did not materially favor the higher score: %d/2000", highSelections)
	}
}

func TestFairShuffleReturnsActualCountAndRevisionChangesSeed(t *testing.T) {
	pool := shufflePool(2)
	pool[1].Candidate.ProviderID = pool[0].Candidate.ProviderID
	first, err := FairShuffle(pool, validSeedContext("task-small", 1), testShufflePolicy())
	if err != nil {
		t.Fatal(err)
	}
	second, err := FairShuffle(pool, validSeedContext("task-small", 2), testShufflePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Selections) != 1 || first.SeedDigest == second.SeedDigest {
		t.Fatalf("actual count or revision seed semantics invalid: first=%#v second=%#v", first, second)
	}
}

func TestFairShuffleRejectsUnqualifiedOrAmbiguousInput(t *testing.T) {
	pool := shufflePool(2)
	pool[1].Score.RuleScore = 59
	if _, err := FairShuffle(pool, validSeedContext("task-low", 1), testShufflePolicy()); err == nil {
		t.Fatal("expected low-quality candidate rejection")
	}
	pool = shufflePool(2)
	pool[1].Candidate.AgentID = pool[0].Candidate.AgentID
	if _, err := FairShuffle(pool, validSeedContext("task-duplicate", 1), testShufflePolicy()); err == nil {
		t.Fatal("expected duplicate agent rejection")
	}
	weakPolicy := testShufflePolicy()
	weakPolicy.SeedSecret = []byte("short")
	if _, err := FairShuffle(shufflePool(1), validSeedContext("task-secret", 1), weakPolicy); err == nil {
		t.Fatal("expected weak seed secret rejection")
	}
}

func shufflePool(count int) []ScoredCandidate {
	pool := make([]ScoredCandidate, 0, count)
	for index := range count {
		score := 90 - index
		pool = append(pool, ScoredCandidate{
			Candidate: Candidate{
				AgentID:          fmt.Sprintf("agent-%02d", index),
				ProviderID:       fmt.Sprintf("provider-%02d", index),
				PriceVersion:     1,
				ExposureCount:    200,
				EffectiveSamples: 100,
			},
			Score: ScoreBreakdown{RuleScore: score, RankingScore: score},
		})
	}
	return pool
}

func validSeedContext(taskID string, revision int) SeedContext {
	return SeedContext{
		TaskID:           taskID,
		TaskSpecHash:     "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		MatchRevision:    revision,
		AlgorithmVersion: FairShuffleAlgorithmVersion,
	}
}

func testShufflePolicy() ShufflePolicy {
	return DefaultShufflePolicy("shuffle-key-v1", []byte("0123456789abcdef0123456789abcdef"))
}

func slicesReverse(source []ScoredCandidate) []ScoredCandidate {
	result := make([]ScoredCandidate, len(source))
	for index := range source {
		result[len(source)-1-index] = source[index]
	}
	return result
}
