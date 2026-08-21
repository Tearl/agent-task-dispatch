package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/example/agent-platform/engine/internal/matching"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (store *Store) Latest(ctx context.Context, taskID, taskSpecHash, algorithmVersion string) (matching.Snapshot, error) {
	var body []byte
	err := store.db.QueryRowContext(ctx, `SELECT snapshot_body FROM match_snapshots WHERE task_id=$1 AND task_spec_hash=$2 AND algorithm_version=$3 AND sealed_at IS NOT NULL ORDER BY match_revision DESC LIMIT 1`, taskID, taskSpecHash, algorithmVersion).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return matching.Snapshot{}, matching.ErrSnapshotNotFound
	}
	if err != nil {
		return matching.Snapshot{}, fmt.Errorf("read latest matching snapshot: %w", err)
	}
	return decodeSnapshot(body)
}

func (store *Store) CreateRevision(ctx context.Context, key matching.SnapshotKey, builder matching.SnapshotBuilder) (matching.Snapshot, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return matching.Snapshot{}, false, fmt.Errorf("begin matching snapshot transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "matching-snapshot:"+key.TaskID); err != nil {
		return matching.Snapshot{}, false, fmt.Errorf("lock matching revision: %w", err)
	}

	var existingBody []byte
	err = tx.QueryRowContext(ctx, `SELECT snapshot_body FROM match_snapshots WHERE task_id=$1 AND task_spec_hash=$2 AND algorithm_version=$3 AND effective_input_hash=$4 AND sealed_at IS NOT NULL`, key.TaskID, key.TaskSpecHash, key.AlgorithmVersion, key.EffectiveInputHash).Scan(&existingBody)
	if err == nil {
		existing, decodeErr := decodeSnapshot(existingBody)
		if decodeErr != nil {
			return matching.Snapshot{}, false, decodeErr
		}
		if err = tx.Commit(); err != nil {
			return matching.Snapshot{}, false, fmt.Errorf("commit matching snapshot replay: %w", err)
		}
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return matching.Snapshot{}, false, fmt.Errorf("read matching snapshot identity: %w", err)
	}

	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(match_revision),0)+1 FROM match_snapshots WHERE task_id=$1`, key.TaskID).Scan(&revision); err != nil {
		return matching.Snapshot{}, false, fmt.Errorf("allocate matching revision: %w", err)
	}
	snapshot, err := builder(revision)
	if err != nil {
		return matching.Snapshot{}, false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT statement_timestamp()`).Scan(&snapshot.CreatedAt); err != nil {
		return matching.Snapshot{}, false, fmt.Errorf("read matching snapshot database time: %w", err)
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return matching.Snapshot{}, false, fmt.Errorf("encode matching snapshot: %w", err)
	}
	degradations, err := json.Marshal(snapshot.Result.Degradations)
	if err != nil {
		return matching.Snapshot{}, false, fmt.Errorf("encode matching degradations: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO match_snapshots (snapshot_id,task_id,task_spec_hash,match_revision,effective_input_hash,algorithm_version,rule_version,model_version,seed_digest,seed_key_version,policy_hash,exploration_triggered,degradations,snapshot_body,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, snapshot.ID, snapshot.TaskID, snapshot.TaskSpecHash, snapshot.MatchRevision, snapshot.EffectiveInputHash, snapshot.AlgorithmVersion, snapshot.RuleVersion, snapshot.ModelVersion, snapshot.SeedDigest, snapshot.SeedKeyVersion, snapshot.PolicyHash, snapshot.ExplorationTriggered, string(degradations), string(body), snapshot.CreatedAt); err != nil {
		return matching.Snapshot{}, false, fmt.Errorf("insert matching snapshot: %w", err)
	}
	if err = insertCandidates(ctx, tx, snapshot); err != nil {
		return matching.Snapshot{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE match_snapshots SET sealed_at=created_at WHERE snapshot_id=$1 AND sealed_at IS NULL`, snapshot.ID); err != nil {
		return matching.Snapshot{}, false, fmt.Errorf("seal matching snapshot: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return matching.Snapshot{}, false, fmt.Errorf("commit matching snapshot: %w", err)
	}
	return snapshot, false, nil
}

func insertCandidates(ctx context.Context, tx *sql.Tx, snapshot matching.Snapshot) error {
	index := 0
	for _, exclusion := range snapshot.Result.Excluded {
		index++
		reasons, err := json.Marshal(exclusion.Reasons)
		if err != nil {
			return fmt.Errorf("encode matching exclusion: %w", err)
		}
		candidate := exclusion.Candidate
		if _, err = tx.ExecContext(ctx, candidateInsertSQL,
			snapshot.ID, index, exclusion.AgentID, candidate.ProviderID, candidate.PriceVersion,
			candidate.OverviewPrice, candidate.FormalPrice, candidate.ExternalCostCap, "excluded", string(reasons), `{}`,
			nil, nil, nil, nil, nil, nil, nil, false, `["hard_filter"]`, nil, nil, nil, nil, nil, false,
		); err != nil {
			return fmt.Errorf("insert excluded matching candidate %q: %w", exclusion.AgentID, err)
		}
	}

	qualified := make(map[string]struct{}, len(snapshot.Result.Qualified))
	for _, candidate := range snapshot.Result.Qualified {
		qualified[candidate.Candidate.AgentID] = struct{}{}
	}
	selected := make(map[string]matching.Selection, len(snapshot.Selections))
	for _, selection := range snapshot.Selections {
		selected[selection.Candidate.Candidate.AgentID] = selection
	}
	best := 0
	if len(snapshot.Result.Scored) > 0 {
		best = snapshot.Result.Scored[0].Score.RankingScore
	}
	for _, scored := range snapshot.Result.Scored {
		index++
		recall, err := json.Marshal(scored.Recall)
		if err != nil {
			return fmt.Errorf("encode matching recall evidence: %w", err)
		}
		_, isQualified := qualified[scored.Candidate.AgentID]
		qualificationReasons, err := json.Marshal(qualificationReasons(scored, isQualified, best))
		if err != nil {
			return fmt.Errorf("encode matching qualification reasons: %w", err)
		}
		var weight, numerator, denominator, draw, position any
		exploration := false
		if isQualified {
			weight = max(1, scored.Score.RankingScore-matching.QualificationFloor+1)
		}
		if selection, ok := selected[scored.Candidate.AgentID]; ok {
			numerator = selection.ProbabilityNumerator
			denominator = selection.ProbabilityDenominator
			draw = strconv.FormatUint(selection.RandomDraw, 10)
			position = selection.Position
			exploration = selection.Exploration
		}
		if _, err = tx.ExecContext(ctx, candidateInsertSQL,
			snapshot.ID, index, scored.Candidate.AgentID, scored.Candidate.ProviderID, scored.Candidate.PriceVersion,
			scored.Candidate.OverviewPrice, scored.Candidate.FormalPrice, scored.Candidate.ExternalCostCap, "scored", `[]`, string(recall),
			scored.Score.TaskMatch, scored.Score.Reputation, scored.Score.PriceTime, scored.Score.Availability,
			scored.Score.RuleScore, scored.Score.ModelDelta, scored.Score.RankingScore, isQualified, string(qualificationReasons),
			weight, numerator, denominator, draw, position, exploration,
		); err != nil {
			return fmt.Errorf("insert scored matching candidate %q: %w", scored.Candidate.AgentID, err)
		}
	}
	return nil
}

func qualificationReasons(candidate matching.ScoredCandidate, qualified bool, best int) []string {
	if qualified {
		return []string{}
	}
	reasons := make([]string, 0, 3)
	if candidate.Score.RuleScore < matching.QualificationFloor {
		reasons = append(reasons, "rule_score_below_floor")
	}
	if candidate.Score.RankingScore < matching.QualificationFloor {
		reasons = append(reasons, "ranking_score_below_floor")
	}
	if candidate.Score.RankingScore < best-matching.MaximumScoreGap {
		reasons = append(reasons, "ranking_score_outside_gap")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "qualified_pool_limit")
	}
	return reasons
}

func decodeSnapshot(body []byte) (matching.Snapshot, error) {
	var snapshot matching.Snapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode matching snapshot: %w", err)
	}
	return snapshot, nil
}

const candidateInsertSQL = `INSERT INTO match_snapshot_candidates (
    snapshot_id,candidate_index,agent_id,provider_id,price_version,overview_price,formal_price,external_cost_cap,
    evaluation_status,exclusion_reasons,recall_evidence,task_match_score,reputation_score,price_time_score,
    availability_score,rule_score,model_delta,ranking_score,qualified,qualification_reasons,selection_weight,
    probability_numerator,probability_denominator,random_draw,final_position,exploration
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)`
