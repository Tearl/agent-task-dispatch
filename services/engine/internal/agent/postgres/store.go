package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/persistence"
	"github.com/lib/pq"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (s *Store) Create(ctx context.Context, m agent.Mutation, input agent.CreateInput, id string) (result agent.Agent, replay bool, err error) {
	body, replay, err := s.execute(ctx, m, "agents.create:"+m.ActorID, func(tx *sql.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, `INSERT INTO agents (agent_id,owner_id,name,category,tags,capabilities,languages,estimated_duration_seconds,author_bio,controller_address,payout_address,max_concurrency,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,lower($10),lower($11),$12,$13,$13)`, id, m.ActorID, input.Name, input.Category, pq.Array(input.Tags), input.Capabilities, pq.Array(input.Languages), input.EstimatedDurationSeconds, input.AuthorBio, input.ControllerAddress, input.PayoutAddress, input.MaxConcurrency, m.Now)
		if err != nil {
			return nil, fmt.Errorf("create agent: %w", err)
		}
		created, err := scanAgent(tx.QueryRowContext(ctx, agentSelect+` WHERE agent_id=$1`, id))
		if err != nil {
			return nil, err
		}
		if err = recordChange(ctx, tx, m, created, "agent.created"); err != nil {
			return nil, err
		}
		return created, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) UpdateProfile(ctx context.Context, m agent.Mutation, id string, input agent.ProfileInput) (result agent.Agent, replay bool, err error) {
	body, replay, err := s.execute(ctx, m, "agents.profile:"+m.ActorID+":"+id, func(tx *sql.Tx) (any, error) {
		current, err := loadOwned(ctx, tx, m.ActorID, id, input.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		addressesChanged := current.ControllerAddress != lower(input.ControllerAddress) || current.PayoutAddress != lower(input.PayoutAddress)
		if current.Status == agent.StatusRetired {
			return nil, agent.ErrInvalidState
		}
		if addressesChanged && (current.ActivatedAt != nil || (current.Status != agent.StatusDraft && current.Status != agent.StatusPaused)) {
			return nil, agent.ErrInvalidState
		}
		activeCapacity, err := reconcileActiveCapacity(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if input.MaxConcurrency < activeCapacity {
			return nil, agent.ErrInvalidState
		}
		_, err = tx.ExecContext(ctx, `UPDATE agents SET name=$1,category=$2,tags=$3,capabilities=$4,languages=$5,estimated_duration_seconds=$6,author_bio=$7,controller_address=lower($8),payout_address=lower($9),max_concurrency=$10,active_capacity=$11,aggregate_version=aggregate_version+1,updated_at=$12 WHERE agent_id=$13`, input.Name, input.Category, pq.Array(input.Tags), input.Capabilities, pq.Array(input.Languages), input.EstimatedDurationSeconds, input.AuthorBio, input.ControllerAddress, input.PayoutAddress, input.MaxConcurrency, activeCapacity, m.Now, id)
		if err != nil {
			return nil, fmt.Errorf("update agent profile: %w", err)
		}
		updated, err := scanAgent(tx.QueryRowContext(ctx, agentSelect+` WHERE agent_id=$1`, id))
		if err != nil {
			return nil, err
		}
		if err = recordChange(ctx, tx, m, updated, "agent.profile_updated"); err != nil {
			return nil, err
		}
		return updated, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) Transition(ctx context.Context, m agent.Mutation, id string, input agent.LifecycleInput) (result agent.Agent, replay bool, err error) {
	body, replay, err := s.execute(ctx, m, "agents.lifecycle:"+m.ActorID+":"+id, func(tx *sql.Tx) (any, error) {
		current, err := loadOwned(ctx, tx, m.ActorID, id, input.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if !validTransition(current.Status, input.Status) {
			return nil, agent.ErrInvalidState
		}
		var transitionNow time.Time
		if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&transitionNow); err != nil {
			return nil, err
		}
		if input.Status == agent.StatusRetired {
			activeCapacity, cleanupErr := reconcileActiveCapacity(ctx, tx, id)
			if cleanupErr != nil {
				return nil, cleanupErr
			}
			if activeCapacity != 0 {
				return nil, agent.ErrInvalidState
			}
		}
		if input.Status == agent.StatusActive && (current.CurrentPriceVersion == nil || current.Health != agent.HealthHealthy || current.HealthValidUntil == nil || !transitionNow.Before(*current.HealthValidUntil)) {
			return nil, agent.ErrInvalidState
		}
		_, err = tx.ExecContext(ctx, `UPDATE agents SET status=$1,activated_at=CASE WHEN $1='active' THEN COALESCE(activated_at,$2) ELSE activated_at END,active_capacity=CASE WHEN $1='retired' THEN 0 ELSE active_capacity END,aggregate_version=aggregate_version+1,updated_at=$2 WHERE agent_id=$3`, input.Status, transitionNow, id)
		if err != nil {
			return nil, fmt.Errorf("transition agent: %w", err)
		}
		updated, err := scanAgent(tx.QueryRowContext(ctx, agentSelect+` WHERE agent_id=$1`, id))
		if err != nil {
			return nil, err
		}
		change := m
		change.Now = transitionNow
		if err = recordChange(ctx, tx, change, updated, "agent."+input.Status); err != nil {
			return nil, err
		}
		return updated, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) UpdateHealth(ctx context.Context, m agent.Mutation, id string, input agent.HealthInput) (result agent.Agent, replay bool, err error) {
	body, replay, err := s.execute(ctx, m, "agents.health:"+m.ActorID+":"+id, func(tx *sql.Tx) (any, error) {
		current, err := loadOwned(ctx, tx, m.ActorID, id, input.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if current.Status == agent.StatusRetired {
			return nil, agent.ErrInvalidState
		}
		if input.CheckedAt.After(m.Now.Add(agent.HealthFutureTolerance)) || input.CheckedAt.Before(m.Now.Add(-agent.HealthFreshnessTTL)) {
			return nil, agent.ErrInvalidInput
		}
		validUntil := input.CheckedAt.Add(agent.HealthFreshnessTTL)
		if serverDeadline := m.Now.Add(agent.HealthFreshnessTTL); validUntil.After(serverDeadline) {
			validUntil = serverDeadline
		}
		_, err = tx.ExecContext(ctx, `UPDATE agents SET health=$1,health_checked_at=$2,health_valid_until=$3,aggregate_version=aggregate_version+1,updated_at=$4 WHERE agent_id=$5`, input.Health, input.CheckedAt, validUntil, m.Now, id)
		if err != nil {
			return nil, fmt.Errorf("update agent health: %w", err)
		}
		updated, err := scanAgent(tx.QueryRowContext(ctx, agentSelect+` WHERE agent_id=$1`, id))
		if err != nil {
			return nil, err
		}
		if err = recordChange(ctx, tx, m, updated, "agent.health_updated"); err != nil {
			return nil, err
		}
		return updated, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) UpdateCapacity(ctx context.Context, m agent.Mutation, id string, input agent.CapacityInput) (result agent.Agent, replay bool, err error) {
	body, replay, err := s.execute(ctx, m, "agents.capacity:"+m.ActorID+":"+id, func(tx *sql.Tx) (any, error) {
		current, err := loadOwned(ctx, tx, m.ActorID, id, input.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if current.Status == agent.StatusRetired {
			return nil, agent.ErrInvalidState
		}
		activeCapacity, err := reconcileActiveCapacity(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if input.MaxConcurrency < activeCapacity {
			return nil, agent.ErrInvalidState
		}
		_, err = tx.ExecContext(ctx, `UPDATE agents SET max_concurrency=$1,active_capacity=$2,aggregate_version=aggregate_version+1,updated_at=$3 WHERE agent_id=$4`, input.MaxConcurrency, activeCapacity, m.Now, id)
		if err != nil {
			return nil, fmt.Errorf("update agent capacity: %w", err)
		}
		updated, err := scanAgent(tx.QueryRowContext(ctx, agentSelect+` WHERE agent_id=$1`, id))
		if err != nil {
			return nil, err
		}
		if err = recordChange(ctx, tx, m, updated, "agent.capacity_updated"); err != nil {
			return nil, err
		}
		return updated, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) PublishPrice(ctx context.Context, m agent.Mutation, id string, input agent.PriceInput) (result agent.PriceVersion, replay bool, err error) {
	body, replay, err := s.execute(ctx, m, "agents.price:"+m.ActorID+":"+id, func(tx *sql.Tx) (any, error) {
		current, err := loadOwned(ctx, tx, m.ActorID, id, input.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if current.Status == agent.StatusRetired {
			return nil, agent.ErrInvalidState
		}
		var version int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(version_no),0)+1 FROM agent_price_versions WHERE agent_id=$1`, id).Scan(&version); err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_price_versions (agent_id,version_no,overview_price,formal_package_gross_price,additional_version_price,external_cost_cap,included_versions,max_versions,created_at) VALUES ($1,$2,$3,$4,$5,$6,3,5,$7)`, id, version, input.OverviewPrice, input.FormalPackageGrossPrice, input.AdditionalVersionPrice, input.ExternalCostCap, m.Now)
		if err != nil {
			return nil, fmt.Errorf("publish agent price: %w", err)
		}
		_, err = tx.ExecContext(ctx, `UPDATE agents SET current_price_version=$1,aggregate_version=aggregate_version+1,updated_at=$2 WHERE agent_id=$3`, version, m.Now, id)
		if err != nil {
			return nil, err
		}
		price := agent.PriceVersion{AgentID: id, Version: version, AgentAggregateVersion: current.AggregateVersion + 1, OverviewPrice: input.OverviewPrice, FormalPackageGrossPrice: input.FormalPackageGrossPrice, AdditionalVersionPrice: input.AdditionalVersionPrice, ExternalCostCap: input.ExternalCostCap, IncludedVersions: 3, MaxVersions: 5, CreatedAt: m.Now}
		updated := current
		updated.AggregateVersion++
		if err = recordChange(ctx, tx, m, updated, "agent.price_published"); err != nil {
			return nil, err
		}
		outboxPayload, err := json.Marshal(map[string]any{
			"eventType":             "agent.price_published",
			"agentId":               id,
			"priceVersion":          version,
			"agentAggregateVersion": price.AgentAggregateVersion,
		})
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_messages (message_id,dedupe_key,topic,payload,available_at) VALUES ($1,$2,'agent-events',$3,$4)`, m.EventID+"_outbox", fmt.Sprintf("agent:%s:price:%d", id, version), string(outboxPayload), m.Now); err != nil {
			return nil, fmt.Errorf("enqueue agent price publication: %w", err)
		}
		return price, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) Get(ctx context.Context, ownerID, id string) (agent.Agent, error) {
	result, err := scanAgent(s.db.QueryRowContext(ctx, agentSelectWithLiveCapacity+` WHERE a.agent_id=$1 AND a.owner_id=$2`, id, ownerID))
	if errors.Is(err, sql.ErrNoRows) {
		return result, agent.ErrNotFound
	}
	return result, err
}

func (s *Store) ReserveCapacity(ctx context.Context, agentID, reservationID string, expiresAt time.Time) (lease agent.CapacityLease, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return lease, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-capacity-reservation:"+reservationID); err != nil {
		return lease, err
	}
	var existingAgent string
	err = tx.QueryRowContext(ctx, `SELECT agent_id,fencing_token,expires_at FROM agent_capacity_leases WHERE reservation_id=$1`, reservationID).Scan(&existingAgent, &lease.FencingToken, &lease.ExpiresAt)
	if err == nil {
		if existingAgent != agentID {
			return lease, agent.ErrInvalidInput
		}
		lease.AgentID = agentID
		lease.ReservationID = reservationID
		_ = tx.Commit()
		return lease, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return lease, err
	}
	var status, health string
	var healthValid sql.NullTime
	var maxConcurrency, nextToken int64
	var databaseNow time.Time
	if err = tx.QueryRowContext(ctx, `SELECT status,health,health_valid_until,max_concurrency,next_fencing_token,clock_timestamp() FROM agents WHERE agent_id=$1 FOR UPDATE`, agentID).Scan(&status, &health, &healthValid, &maxConcurrency, &nextToken, &databaseNow); errors.Is(err, sql.ErrNoRows) {
		return lease, agent.ErrNotFound
	}
	if err != nil {
		return lease, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_capacity_leases SET released_at=$2 WHERE agent_id=$1 AND released_at IS NULL AND expires_at<=$2`, agentID, databaseNow); err != nil {
		return lease, err
	}
	var active int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_capacity_leases WHERE agent_id=$1 AND released_at IS NULL AND expires_at>$2`, agentID, databaseNow).Scan(&active); err != nil {
		return lease, err
	}
	if status != agent.StatusActive || health != agent.HealthHealthy || !healthValid.Valid || !databaseNow.Before(healthValid.Time) || !expiresAt.After(databaseNow) || active >= maxConcurrency {
		return lease, agent.ErrCapacityUnavailable
	}
	token := nextToken + 1
	if _, err = tx.ExecContext(ctx, `UPDATE agents SET active_capacity=$1,next_fencing_token=$2 WHERE agent_id=$3`, active+1, token, agentID); err != nil {
		return lease, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_capacity_leases (reservation_id,agent_id,fencing_token,expires_at) VALUES ($1,$2,$3,$4)`, reservationID, agentID, token, expiresAt); err != nil {
		return lease, err
	}
	if err = tx.Commit(); err != nil {
		return lease, err
	}
	return agent.CapacityLease{ReservationID: reservationID, AgentID: agentID, FencingToken: token, ExpiresAt: expiresAt}, nil
}
func (s *Store) ReleaseCapacity(ctx context.Context, reservationID string, fencingToken int64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var agentID string
	if err = tx.QueryRowContext(ctx, `SELECT agent_id FROM agent_capacity_leases WHERE reservation_id=$1`, reservationID).Scan(&agentID); errors.Is(err, sql.ErrNoRows) {
		return agent.ErrNotFound
	}
	if err != nil {
		return err
	}
	// Capacity transactions always lock the aggregate before a lease. This
	// prevents release from deadlocking with reservation's expired-lease cleanup.
	if err = tx.QueryRowContext(ctx, `SELECT agent_id FROM agents WHERE agent_id=$1 FOR UPDATE`, agentID).Scan(&agentID); err != nil {
		return err
	}
	var token int64
	var released sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT fencing_token,released_at FROM agent_capacity_leases WHERE reservation_id=$1 FOR UPDATE`, reservationID).Scan(&token, &released); errors.Is(err, sql.ErrNoRows) {
		return agent.ErrNotFound
	}
	if err != nil {
		return err
	}
	if token != fencingToken {
		return agent.ErrStaleVersion
	}
	if released.Valid {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_capacity_leases SET released_at=clock_timestamp() WHERE reservation_id=$1`, reservationID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agents SET active_capacity=GREATEST(active_capacity-1,0) WHERE agent_id=$1`, agentID); err != nil {
		return err
	}
	return tx.Commit()
}

type work func(*sql.Tx) (any, error)

func (s *Store) execute(ctx context.Context, m agent.Mutation, scope string, fn work) (json.RawMessage, bool, error) {
	if m.IdempotencyKey == "" || m.RequestHash == "" {
		return nil, false, agent.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, scope+":"+m.IdempotencyKey); err != nil {
		return nil, false, err
	}
	var previousHash string
	var previous []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_body FROM idempotency_records WHERE scope=$1 AND idempotency_key=$2`, scope, m.IdempotencyKey).Scan(&previousHash, &previous)
	if err == nil {
		if previousHash != m.RequestHash {
			return nil, false, persistence.ErrIdempotencyConflict
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		return previous, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	value, err := fn(tx)
	if err != nil {
		return nil, false, err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_records (scope,idempotency_key,request_hash,response_body) VALUES ($1,$2,$3,$4)`, scope, m.IdempotencyKey, m.RequestHash, body); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return body, false, nil
}

func loadOwned(ctx context.Context, tx *sql.Tx, ownerID, id string, version int64) (agent.Agent, error) {
	current, err := scanAgent(tx.QueryRowContext(ctx, agentSelect+` WHERE agent_id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return current, agent.ErrNotFound
	}
	if err != nil {
		return current, err
	}
	if current.OwnerID != ownerID {
		return current, agent.ErrNotFound
	}
	if current.AggregateVersion != version {
		return current, agent.ErrStaleVersion
	}
	return current, nil
}
func validTransition(from, to string) bool {
	switch from {
	case agent.StatusDraft:
		return to == agent.StatusPaused || to == agent.StatusActive || to == agent.StatusRetired
	case agent.StatusPaused:
		return to == agent.StatusDraft || to == agent.StatusActive || to == agent.StatusRetired
	case agent.StatusActive:
		return to == agent.StatusPaused || to == agent.StatusRetired
	default:
		return false
	}
}

func reconcileActiveCapacity(ctx context.Context, tx *sql.Tx, agentID string) (int, error) {
	var databaseNow time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_capacity_leases SET released_at=$2 WHERE agent_id=$1 AND released_at IS NULL AND expires_at<=$2`, agentID, databaseNow); err != nil {
		return 0, err
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_capacity_leases WHERE agent_id=$1 AND released_at IS NULL AND expires_at>$2`, agentID, databaseNow).Scan(&active); err != nil {
		return 0, err
	}
	return active, nil
}

func recordChange(ctx context.Context, tx *sql.Tx, m agent.Mutation, value agent.Agent, eventType string) error {
	payload, _ := json.Marshal(map[string]any{"status": value.Status, "aggregateVersion": value.AggregateVersion})
	if _, err := tx.ExecContext(ctx, `INSERT INTO domain_events (event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload,occurred_at) VALUES ($1,'agent',$2,$3,$4,$5,$6)`, m.EventID, value.ID, value.AggregateVersion, eventType, string(payload), m.Now); err != nil {
		return fmt.Errorf("record agent domain event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events (event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at) VALUES ($1,$2,$3,'agent',$4,$5,$6)`, m.EventID+"_audit", m.ActorID, eventType, value.ID, string(payload), m.Now); err != nil {
		return fmt.Errorf("record agent audit event: %w", err)
	}
	return nil
}
func lower(value string) string {
	result := []byte(value)
	for i, b := range result {
		if b >= 'A' && b <= 'Z' {
			result[i] = b + ('a' - 'A')
		}
	}
	return string(result)
}

const agentSelect = `SELECT agent_id,owner_id,name,category,tags,capabilities,languages,estimated_duration_seconds,author_bio,controller_address,payout_address,status,health,health_checked_at,health_valid_until,max_concurrency,active_capacity,aggregate_version,activated_at,current_price_version,current_credential_version,created_at,updated_at FROM agents`

const agentSelectWithLiveCapacity = `SELECT a.agent_id,a.owner_id,a.name,a.category,a.tags,a.capabilities,a.languages,a.estimated_duration_seconds,a.author_bio,a.controller_address,a.payout_address,a.status,a.health,a.health_checked_at,a.health_valid_until,a.max_concurrency,(SELECT count(*)::integer FROM agent_capacity_leases l WHERE l.agent_id=a.agent_id AND l.released_at IS NULL AND l.expires_at>now()),a.aggregate_version,a.activated_at,a.current_price_version,a.current_credential_version,a.created_at,a.updated_at FROM agents a`

type scanner interface{ Scan(...any) error }

func scanAgent(row scanner) (value agent.Agent, err error) {
	var tags, languages pq.StringArray
	var healthChecked, healthValid, activated sql.NullTime
	var price, credentialVersion sql.NullInt64
	err = row.Scan(&value.ID, &value.OwnerID, &value.Name, &value.Category, &tags, &value.Capabilities, &languages, &value.EstimatedDurationSeconds, &value.AuthorBio, &value.ControllerAddress, &value.PayoutAddress, &value.Status, &value.Health, &healthChecked, &healthValid, &value.MaxConcurrency, &value.ActiveCapacity, &value.AggregateVersion, &activated, &price, &credentialVersion, &value.CreatedAt, &value.UpdatedAt)
	value.Tags = []string(tags)
	value.Languages = []string(languages)
	if healthChecked.Valid {
		value.HealthCheckedAt = &healthChecked.Time
	}
	if healthValid.Valid {
		value.HealthValidUntil = &healthValid.Time
	}
	if activated.Valid {
		value.ActivatedAt = &activated.Time
	}
	if price.Valid {
		v := int(price.Int64)
		value.CurrentPriceVersion = &v
	}
	if credentialVersion.Valid {
		v := int(credentialVersion.Int64)
		value.CurrentCredentialVersion = &v
	}
	return value, err
}
