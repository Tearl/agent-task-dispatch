package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/example/agent-platform/engine/internal/matching"
	matchingpostgres "github.com/example/agent-platform/engine/internal/matching/postgres"
	"github.com/example/agent-platform/engine/internal/matchingview"
	"github.com/example/agent-platform/engine/internal/overview"
	overviewpostgres "github.com/example/agent-platform/engine/internal/overview/postgres"
	"github.com/lib/pq"
)

type Store struct {
	db        *sql.DB
	snapshots *matchingpostgres.Store
	overviews *overviewpostgres.Store
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	snapshots, err := matchingpostgres.NewStore(db)
	if err != nil {
		return nil, err
	}
	overviews, err := overviewpostgres.NewStore(db)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, snapshots: snapshots, overviews: overviews}, nil
}

func (store *Store) Get(ctx context.Context, publisherID, taskID string) (matchingview.View, error) {
	var view matchingview.View
	if err := store.db.QueryRowContext(ctx, `SELECT clock_timestamp(),task.task_id,task.title,task.status,spec.content_hash,task.deletion_requested_at IS NOT NULL
FROM tasks task JOIN task_spec_versions spec ON spec.task_id=task.task_id AND spec.version_no=task.current_spec_version
WHERE task.task_id=$1 AND task.publisher_id=$2 AND task.deleted_at IS NULL`, taskID, publisherID).Scan(&view.AsOf, &view.Task.ID, &view.Task.Title, &view.Task.Status, &view.Task.SpecHash, &view.Task.DeletionPending); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return view, matchingview.ErrNotFound
		}
		return view, err
	}
	snapshot, err := store.snapshots.Latest(ctx, taskID, view.Task.SpecHash, matching.CategoryTagsAlgorithmVersion)
	if errors.Is(err, matching.ErrSnapshotNotFound) {
		return view, nil
	}
	if err != nil {
		return view, err
	}
	names, err := store.agentNames(ctx, snapshot)
	if err != nil {
		return view, err
	}
	projected := &matchingview.Snapshot{ID: snapshot.ID, Revision: snapshot.MatchRevision, AlgorithmVersion: snapshot.AlgorithmVersion, RuleVersion: snapshot.RuleVersion, ModelVersion: snapshot.ModelVersion, SeedDigest: snapshot.SeedDigest, ExplorationTriggered: snapshot.ExplorationTriggered, CreatedAt: snapshot.CreatedAt, Candidates: []matchingview.Candidate{}, Degradations: []matchingview.Degradation{}}
	for _, degradation := range snapshot.Result.Degradations {
		projected.Degradations = append(projected.Degradations, matchingview.Degradation{Dependency: degradation.Dependency, Code: degradation.Code, Message: degradation.Message})
	}
	for _, selected := range snapshot.Selections {
		candidate := selected.Candidate.Candidate
		projected.Candidates = append(projected.Candidates, matchingview.Candidate{AgentID: candidate.AgentID, Name: names[candidate.AgentID], Category: candidate.Category, Tags: candidate.Tags, EstimatedDurationSecond: int64(candidate.EstimatedDuration.Seconds()), Position: selected.Position, Exploration: selected.Exploration, OverviewPrice: candidate.OverviewPrice, FormalPrice: candidate.FormalPrice, ExternalCostCap: candidate.ExternalCostCap, Score: matchingview.Score{TaskMatch: selected.Candidate.Score.TaskMatch, Reputation: selected.Candidate.Score.Reputation, PriceTime: selected.Candidate.Score.PriceTime, Availability: selected.Candidate.Score.Availability, Rule: selected.Candidate.Score.RuleScore, ModelDelta: selected.Candidate.Score.ModelDelta, Ranking: selected.Candidate.Score.RankingScore}})
	}
	view.Snapshot = projected
	var batchID string
	err = store.db.QueryRowContext(ctx, `SELECT batch_id FROM overview_batches WHERE snapshot_id=$1 ORDER BY created_at DESC LIMIT 1`, snapshot.ID).Scan(&batchID)
	if err == nil {
		batch, getErr := store.overviews.Get(ctx, batchID)
		if getErr != nil {
			return view, getErr
		}
		view.Batch = &matchingview.Batch{ID: batch.ID, Status: batch.Status, Deadline: batch.Deadline, ReplacementUsed: batch.ReplacementUsed, ReplacementExhausted: batch.ReplacementExhausted}
		attachOverviews(projected.Candidates, batch.Slots)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return view, err
	}
	var reservation matchingview.Reservation
	err = store.db.QueryRowContext(ctx, `SELECT reservation_id,agent_id,slot_id,status,COALESCE(transaction_hash,'') FROM selection_reservations WHERE task_id=$1 AND publisher_id=$2 ORDER BY created_at DESC LIMIT 1`, taskID, publisherID).Scan(&reservation.ID, &reservation.AgentID, &reservation.SlotID, &reservation.Status, &reservation.TransactionHash)
	if err == nil {
		view.Reservation = &reservation
	} else if !errors.Is(err, sql.ErrNoRows) {
		return view, err
	}
	return view, nil
}

func (store *Store) agentNames(ctx context.Context, snapshot matching.Snapshot) (map[string]string, error) {
	ids := make([]string, 0, len(snapshot.Selections))
	for _, item := range snapshot.Selections {
		ids = append(ids, item.Candidate.Candidate.AgentID)
	}
	result := map[string]string{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := store.db.QueryContext(ctx, `SELECT agent_id,name FROM agents WHERE agent_id=ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result[id] = name
	}
	return result, rows.Err()
}

func attachOverviews(candidates []matchingview.Candidate, slots []overview.Slot) {
	byAgent := map[string]overview.Slot{}
	for _, slot := range slots {
		current, exists := byAgent[slot.AgentID]
		if !exists || slot.Ordinal > current.Ordinal {
			byAgent[slot.AgentID] = slot
		}
	}
	for index := range candidates {
		if slot, ok := byAgent[candidates[index].AgentID]; ok {
			candidates[index].Overview = &matchingview.Overview{SlotID: slot.ID, LogicalExecutionID: slot.LogicalExecutionID, Status: slot.Status, BillingStatus: slot.BillingStatus, ValidationCodes: slot.Validation.Codes, ContentHash: slot.ContentHash, Replacement: slot.Replacement}
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool { return candidates[left].Position < candidates[right].Position })
}
