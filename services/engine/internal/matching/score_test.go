package matching

import "testing"

func TestRuleScoreUsesFrozenSixtyTwentyFiveTenFiveWeights(t *testing.T) {
	request, candidate := validFixture()
	candidate.FormalPrice = "0"
	candidate.EstimatedDuration = 1
	evidence := map[string]RecallEvidence{
		ChannelDense:   {Rank: 1, Relevance: 10_000},
		ChannelLexical: {Rank: 1, Relevance: 10_000},
		ChannelExact:   {Rank: 1, Relevance: 10_000},
	}

	score := RuleScore(request, candidate, evidence)
	if score.TaskMatch != 60 || score.Reputation != 25 || score.PriceTime != 10 || score.Availability != 5 || score.RuleScore != 100 {
		t.Fatalf("unexpected score breakdown: %#v", score)
	}
}

func TestRuleScoreClampsUntrustedSignalsAndModelAdjustment(t *testing.T) {
	request, candidate := validFixture()
	candidate.Reputation = Reputation{Quality: 200, Speed: -1, Reliability: 100, Communication: 100, Compliance: 100}
	evidence := map[string]RecallEvidence{ChannelDense: {Rank: 1, Relevance: 20_000}}
	score := RuleScore(request, candidate, evidence)
	if score.TaskMatch != denseTaskWeight || score.Reputation != 20 {
		t.Fatalf("signals were not clamped: %#v", score)
	}
	if adjusted := ApplyModelDelta(score, 99); adjusted.ModelDelta != 5 || adjusted.RankingScore > 100 {
		t.Fatalf("positive model delta was not bounded: %#v", adjusted)
	}
	if adjusted := ApplyModelDelta(ScoreBreakdown{RuleScore: 2}, -99); adjusted.ModelDelta != -5 || adjusted.RankingScore != 0 {
		t.Fatalf("negative model delta was not bounded: %#v", adjusted)
	}
}

func TestQualificationAppliesEveryThresholdAndTwentyCandidateLimit(t *testing.T) {
	scored := make([]ScoredCandidate, 0, 25)
	for index := range 25 {
		scored = append(scored, ScoredCandidate{
			Candidate: Candidate{AgentID: agentID(index)},
			Score:     ScoreBreakdown{RuleScore: 90, RankingScore: 90},
		})
	}
	scored = append(scored,
		ScoredCandidate{Candidate: Candidate{AgentID: "low-rule"}, Score: ScoreBreakdown{RuleScore: 59, RankingScore: 90}},
		ScoredCandidate{Candidate: Candidate{AgentID: "low-ranking"}, Score: ScoreBreakdown{RuleScore: 90, RankingScore: 59}},
		ScoredCandidate{Candidate: Candidate{AgentID: "outside-gap"}, Score: ScoreBreakdown{RuleScore: 79, RankingScore: 79}},
	)
	scored[0].Score = ScoreBreakdown{RuleScore: 90, RankingScore: 90}
	sortScored(scored)

	qualified := qualify(scored)
	if len(qualified) != MaxQualifiedPool {
		t.Fatalf("qualified pool was not capped at %d: %d", MaxQualifiedPool, len(qualified))
	}
	for _, candidate := range qualified {
		if candidate.Score.RuleScore < 60 || candidate.Score.RankingScore < 80 {
			t.Fatalf("unqualified candidate escaped threshold: %#v", candidate)
		}
	}
}

func agentID(index int) string {
	const digits = "0123456789"
	return "agent-" + string([]byte{digits[index/10], digits[index%10]})
}
