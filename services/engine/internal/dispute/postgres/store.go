package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/dispute"
	"github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

var addressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

func NewStore(db *sql.DB, disputeResolver ...string) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	resolver := ""
	if len(disputeResolver) > 0 {
		resolver = strings.ToLower(disputeResolver[0])
		if resolver != "" && !addressPattern.MatchString(resolver) {
			return nil, errors.New("invalid dispute resolver address")
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Context(ctx context.Context, taskID string) (dispute.Context, error) {
	var c dispute.Context
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT task.task_id,assignment.assignment_id,package.package_id,task.publisher_id,assignment.provider_id,reservation.chain_id::text,reservation.contract_address,reservation.proof_task_id,reservation.publisher_wallet,reservation.agent_controller,reservation.payout_address,assignment.formal_payable::text,deployment.asset_key,deployment.dispute_resolver_address,task.status,COALESCE((SELECT max(intent.created_at) FROM formal_acceptance_intents intent WHERE intent.task_id=task.task_id),task.updated_at)+interval '7 days'
	FROM tasks task JOIN assignments assignment ON assignment.task_id=task.task_id JOIN active_assignments active ON active.assignment_id=assignment.assignment_id JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id JOIN escrow_deployments deployment ON deployment.chain_id=reservation.chain_id AND deployment.contract_address=reservation.contract_address JOIN formal_packages package ON package.task_id=task.task_id WHERE task.task_id=$1`, taskID).Scan(&c.TaskID, &c.AssignmentID, &c.DeliveryUnitID, &c.PublisherID, &c.AgentProviderID, &c.ChainID, &c.ContractAddress, &c.ChainTaskID, &c.PublisherWallet, &c.AgentController, &c.AgentPayout, &c.FrozenAmount, &c.Asset, &c.DisputeResolver, &status, &c.DisputeDeadline)
	if errors.Is(err, sql.ErrNoRows) {
		return c, dispute.ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.FeeCap = "0"
	c.Eligible = contains([]string{"formal_review", "accepted", "settlement_pending", "settled", "dispute_requested", "disputed"}, status)
	if !c.Eligible {
		c.ReasonCode = "task_state_ineligible"
	}
	return c, nil
}

func (s *Store) VerifyEvidence(ctx context.Context, input dispute.EvidenceInput) error {
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT true FROM dispute_worm_receipts WHERE object_key=$1 AND ciphertext_digest=$2 AND object_version_id=$3 AND retention_mode='COMPLIANCE' AND retain_until>=$4`, input.ObjectKey, input.CiphertextDigest, input.ObjectVersionID, input.RetainUntil).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return dispute.ErrInvalidState
	}
	return err
}

func (s *Store) HasConflict(ctx context.Context, caseID, assigneeID string) (bool, error) {
	var conflicted bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM dispute_conflict_declarations WHERE case_id=$1 AND subject_user_id=$2)`, caseID, assigneeID).Scan(&conflicted)
	return conflicted, err
}

func (s *Store) HasReviewFeeAuthorization(ctx context.Context, caseID, assigneeID string) (bool, error) {
	var authorized bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM dispute_review_fee_authorizations WHERE case_id=$1 AND assignee_id=$2)`, caseID, assigneeID).Scan(&authorized)
	return authorized, err
}

func (s *Store) AuditAccessDenial(ctx context.Context, caseID, actorID, key string, input dispute.AccessInput) error {
	now := time.Now().UTC()
	metadata, _ := json.Marshal(map[string]string{"evidenceId": input.EvidenceID, "purpose": input.Purpose, "reasonCode": "access_forbidden"})
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events (event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at) VALUES ($1,$2,'evidence_access_denied','dispute_case',$3,$4,$5) ON CONFLICT (event_id) DO NOTHING`, stable("audit-access-denied", actorID, key), actorID, caseID, metadata, now)
	return err
}

func (s *Store) Execute(ctx context.Context, m dispute.Mutation, c dispute.Context, caseID string, command dispute.Command) (dispute.View, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return dispute.View{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	lock := caseID
	if lock == "" {
		lock = c.AssignmentID + ":" + c.DeliveryUnitID
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "dispute:"+lock); err != nil {
		return dispute.View{}, false, err
	}
	var priorHash string
	var priorBody []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_body FROM dispute_requests WHERE actor_id=$1 AND idempotency_key=$2`, m.ActorID, m.IdempotencyKey).Scan(&priorHash, &priorBody)
	if err == nil {
		if priorHash != m.RequestHash {
			return dispute.View{}, false, dispute.ErrConflict
		}
		var prior dispute.View
		if decodeErr := json.Unmarshal(priorBody, &prior); decodeErr != nil {
			return prior, false, decodeErr
		}
		return prior, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return dispute.View{}, false, err
	}
	var current *dispute.View
	if caseID != "" {
		var body []byte
		err = tx.QueryRowContext(ctx, `SELECT view_body FROM dispute_case_projections WHERE case_id=$1 FOR UPDATE`, caseID).Scan(&body)
		if errors.Is(err, sql.ErrNoRows) {
			return dispute.View{}, false, dispute.ErrNotFound
		}
		if err != nil {
			return dispute.View{}, false, err
		}
		var value dispute.View
		if err = json.Unmarshal(body, &value); err != nil {
			return value, false, err
		}
		value, err = canonicalizeFreeze(ctx, tx, value)
		if err != nil {
			return value, false, err
		}
		current = &value
		c = value.Context
	}
	var freezeEventID, freezeRoot string
	if command.Kind == "freeze_confirm" {
		input := command.Input.(dispute.FreezeInput)
		var frozenAmount, feeCap string
		err = tx.QueryRowContext(ctx, `SELECT event.event_id,event.payload->>'root',event.payload->>'amount',event.payload->>'feeCap' FROM chain_events event JOIN chain_canonical_blocks canonical ON canonical.chain_id=event.chain_id AND canonical.contract_address=event.contract_address AND canonical.block_hash=event.block_hash WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.transaction_hash=$3 AND event.event_type='dispute_frozen' AND event.task_chain_id=$4`, c.ChainID, c.ContractAddress, strings.ToLower(input.TransactionHash), c.ChainTaskID).Scan(&freezeEventID, &freezeRoot, &frozenAmount, &feeCap)
		if errors.Is(err, sql.ErrNoRows) {
			return dispute.View{}, false, dispute.ErrPending
		}
		if err != nil {
			return dispute.View{}, false, err
		}
		if frozenAmount != c.FrozenAmount || feeCap != c.FeeCap || freezeRoot == "" {
			return dispute.View{}, false, dispute.ErrConflict
		}
	}
	if command.Kind == "assign" || command.Kind == "decision" || command.Kind == "review" {
		assigneeID := m.ActorID
		if command.Kind == "assign" {
			assigneeID = command.Input.(dispute.AssignInput).AssigneeID
		}
		var conflicted bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM dispute_conflict_declarations WHERE case_id=$1 AND subject_user_id=$2)`, caseID, assigneeID).Scan(&conflicted)
		if err != nil {
			return dispute.View{}, false, err
		}
		if conflicted {
			return dispute.View{}, false, dispute.ErrConflict
		}
	}
	if command.Kind == "review" {
		var authorized bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM dispute_review_fee_authorizations WHERE case_id=$1 AND assignee_id=$2)`, caseID, m.ActorID).Scan(&authorized)
		if err != nil {
			return dispute.View{}, false, err
		}
		if !authorized {
			return dispute.View{}, false, dispute.ErrForbidden
		}
		input := command.Input.(dispute.ReviewInput)
		input.FeeAuthorized = true
		command.Input = input
	}
	now := time.Now().UTC()
	var next dispute.View
	if command.Kind == "access" {
		next = clone(*current)
		input := command.Input.(dispute.AccessInput)
		found := false
		for _, e := range next.Case.Evidence {
			if e.ID == input.EvidenceID {
				found = true
			}
		}
		if !found {
			return dispute.View{}, false, dispute.ErrNotFound
		}
		grant := dispute.AccessGrant{ID: stable("access", caseID, input.EvidenceID, m.ActorID, m.IdempotencyKey), EvidenceID: input.EvidenceID, PrincipalID: m.ActorID, Purpose: input.Purpose, CreatedAt: now, ExpiresAt: now.Add(time.Duration(input.TTLSeconds) * time.Second)}
		next.AccessGrants = append(next.AccessGrants, grant)
		next.Case.AggregateVersion++
		next.Case.UpdatedAt = now
		_, err = tx.ExecContext(ctx, `INSERT INTO dispute_evidence_access_grants (grant_id,case_id,evidence_id,principal_id,purpose,expires_at,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, grant.ID, caseID, grant.EvidenceID, grant.PrincipalID, grant.Purpose, grant.ExpiresAt, now)
	} else if command.Kind == "admin" {
		input := command.Input.(dispute.AdminInput)
		if current != nil {
			next = clone(*current)
		}
		op := dispute.AdminOperation{ID: stable("admin", m.ActorID, m.IdempotencyKey), Kind: input.Kind, ResourceType: input.ResourceType, ResourceID: input.ResourceID, ReasonCode: input.ReasonCode, PayloadHash: m.RequestHash, ActorID: m.ActorID, Status: "recorded", CreatedAt: now}
		next.AdminOperations = append(next.AdminOperations, op)
		_, err = tx.ExecContext(ctx, `INSERT INTO dispute_admin_operations (operation_id,operation_kind,resource_type,resource_id,reason_code,payload_hash,actor_id,status,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,'recorded',$8)`, op.ID, op.Kind, op.ResourceType, op.ResourceID, op.ReasonCode, op.PayloadHash, op.ActorID, now)
		if err == nil {
			payload, _ := json.Marshal(map[string]any{"operationId": op.ID, "kind": op.Kind, "resourceType": op.ResourceType, "resourceId": op.ResourceID, "reasonCode": op.ReasonCode, "payloadHash": op.PayloadHash})
			_, err = tx.ExecContext(ctx, `INSERT INTO outbox_messages (message_id,dedupe_key,topic,payload,available_at,created_at) VALUES ($1,$2,'admin.operation.requested',$3,$4,$4)`, stable("outbox", op.ID), op.ID, payload, now)
			if err == nil {
				_, err = tx.ExecContext(ctx, `INSERT INTO audit_events (event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, stable("audit", op.ID), op.ActorID, op.Kind, op.ResourceType, op.ResourceID, payload, now)
			}
		}
	} else {
		next, err = dispute.Apply(current, c, m.ActorID, command, now)
	}
	if err != nil {
		return dispute.View{}, false, err
	}
	if command.Kind == "freeze_confirm" {
		next.Case.FreezeEventID, next.Case.FreezeRoot = freezeEventID, freezeRoot
	}
	body, err := json.Marshal(next)
	if err != nil {
		return next, false, err
	}
	if current == nil && command.Kind == "open" {
		_, err = tx.ExecContext(ctx, `INSERT INTO dispute_cases (case_id,task_id,assignment_id,delivery_unit_id,policy_version,publisher_id,agent_provider_id,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, next.Case.ID, next.Case.TaskID, next.Case.AssignmentID, next.Case.DeliveryUnitID, next.Case.PolicyVersion, next.Case.PublisherID, next.Case.AgentProviderID, now)
		caseID = next.Case.ID
	}
	if err == nil && caseID != "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO dispute_case_projections (case_id,state,aggregate_version,view_body,updated_at) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (case_id) DO UPDATE SET state=EXCLUDED.state,aggregate_version=EXCLUDED.aggregate_version,view_body=EXCLUDED.view_body,updated_at=EXCLUDED.updated_at`, caseID, next.Case.State, next.Case.AggregateVersion, body, now)
	}
	if err == nil && command.Kind == "evidence" {
		e := next.Case.Evidence[len(next.Case.Evidence)-1]
		_, err = tx.ExecContext(ctx, `INSERT INTO dispute_evidence_manifest (evidence_id,case_id,claim_id,category,object_key,ciphertext_digest,envelope_key_reference,object_version_id,retention_mode,retain_until,submitted_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, e.ID, caseID, e.ClaimID, e.Category, e.ObjectKey, e.CiphertextDigest, e.EnvelopeKeyReference, e.ObjectVersionID, e.RetentionMode, e.RetainUntil, e.SubmittedBy, e.CreatedAt)
	}
	if err == nil {
		eventBody := disputeEventBody(command)
		eventCaseID := caseID
		if command.Kind == "admin" && current == nil {
			eventCaseID = ""
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO dispute_events (event_id,case_id,event_type,actor_id,payload,occurred_at) VALUES ($1,$2,$3,$4,$5,$6)`, stable("event", caseID, m.ActorID, m.IdempotencyKey), nullable(eventCaseID), command.Kind, m.ActorID, eventBody, now)
	}
	if err != nil {
		return dispute.View{}, false, mapError(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO dispute_requests (actor_id,idempotency_key,request_hash,operation,case_id,response_body,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, m.ActorID, m.IdempotencyKey, m.RequestHash, command.Kind, nullable(caseID), body, now)
	if err != nil {
		return dispute.View{}, false, mapError(err)
	}
	if err = tx.Commit(); err != nil {
		return dispute.View{}, false, mapError(err)
	}
	return next, false, nil
}

func disputeEventBody(command dispute.Command) []byte {
	if command.Kind == "settlement" {
		input := command.Input.(dispute.SettlementInput)
		body, _ := json.Marshal(map[string]any{
			"publisherBps":  input.PublisherBPS,
			"reasonCode":    input.ReasonCode,
			"evidenceRoot":  input.EvidenceRoot,
			"agreementHash": input.AgreementHash,
			"verified":      input.Verified,
		})
		return body
	}
	body, _ := json.Marshal(command.Input)
	return body
}

func (s *Store) Get(ctx context.Context, _ string, caseID string) (dispute.View, error) {
	var body []byte
	err := s.db.QueryRowContext(ctx, `SELECT view_body FROM dispute_case_projections WHERE case_id=$1`, caseID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return dispute.View{}, dispute.ErrNotFound
	}
	if err != nil {
		return dispute.View{}, err
	}
	var value dispute.View
	if err = json.Unmarshal(body, &value); err != nil {
		return value, err
	}
	return canonicalizeFreeze(ctx, s.db, value)
}
func (s *Store) List(ctx context.Context, actor string, roles []string) ([]dispute.View, error) {
	admin := contains(roles, "admin")
	arbitrator := contains(roles, "arbitrator")
	rows, err := s.db.QueryContext(ctx, `SELECT projection.view_body FROM dispute_case_projections projection JOIN dispute_cases case_record ON case_record.case_id=projection.case_id WHERE $1 OR case_record.publisher_id=$2 OR case_record.agent_provider_id=$2 OR ($3 AND EXISTS (SELECT 1 FROM jsonb_array_elements(projection.view_body->'case'->'assignments') assignment WHERE assignment->>'assigneeId'=$2)) ORDER BY projection.updated_at DESC`, admin, actor, arbitrator)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []dispute.View{}
	for rows.Next() {
		var body []byte
		var value dispute.View
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &value); err != nil {
			return nil, err
		}
		value, err = canonicalizeFreeze(ctx, s.db, value)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func canonicalizeFreeze(ctx context.Context, query rowQuerier, value dispute.View) (dispute.View, error) {
	if value.Case.FreezeEventID == "" || value.Case.State == dispute.StateSoftLock || value.Case.State == dispute.StateOrphaned {
		return value, nil
	}
	var canonical bool
	err := query.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM chain_events event JOIN chain_canonical_blocks canonical ON canonical.chain_id=event.chain_id AND canonical.contract_address=event.contract_address AND canonical.block_hash=event.block_hash WHERE event.event_id=$1 AND event.event_type='dispute_frozen')`, value.Case.FreezeEventID).Scan(&canonical)
	if err != nil {
		return value, err
	}
	if !canonical {
		value.Case.State = dispute.StateOrphaned
		value.Case.FrozenAt = nil
		value.Case.FreezeRoot = ""
		value.Case.ReputationPending = true
	}
	return value, nil
}
func clone(v dispute.View) dispute.View {
	body, _ := json.Marshal(v)
	var result dispute.View
	_ = json.Unmarshal(body, &result)
	return result
}
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func contains(v []string, target string) bool {
	for _, item := range v {
		if item == target {
			return true
		}
	}
	return false
}
func stable(parts ...string) string { return disputeStable(parts...) }
func disputeStable(parts ...string) string {
	body := strings.Join(parts, "\x00")
	return fmt.Sprintf("sha256:%x", sha256Sum(body))
}
func sha256Sum(v string) [32]byte { return sha256.Sum256([]byte(v)) }
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if code, ok := err.(*pq.Error); ok && code.Code == "23505" {
		return dispute.ErrConflict
	}
	return err
}
