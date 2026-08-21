package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (s *Store) SaveChallenge(ctx context.Context, c auth.Challenge) (result auth.Challenge, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, fmt.Errorf("begin challenge issuance: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Only requests for the same wallet serialize; unrelated wallets and
	// Engine replicas continue concurrently.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "auth-nonce:"+c.WalletAddress); err != nil {
		return result, fmt.Errorf("lock challenge issuance: %w", err)
	}
	err = tx.QueryRowContext(ctx, `SELECT wallet_address,domain,chain_id,purpose,version,nonce,issued_at,expires_at,message FROM wallet_nonces WHERE wallet_address=$1 AND domain=$2 AND chain_id=$3 AND purpose=$4 AND version=$5 AND consumed_at IS NULL AND expires_at>now() ORDER BY issued_at DESC LIMIT 1`, c.WalletAddress, c.Domain, c.ChainID, c.Purpose, c.Version).Scan(&result.WalletAddress, &result.Domain, &result.ChainID, &result.Purpose, &result.Version, &result.Nonce, &result.IssuedAt, &result.ExpiresAt, &result.Message)
	if err == nil {
		if err = tx.Commit(); err != nil {
			return auth.Challenge{}, fmt.Errorf("commit reused challenge: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("read active challenge: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM auth_rate_limit_buckets WHERE expires_at<now()`); err != nil {
		return result, fmt.Errorf("prune rate buckets: %w", err)
	}
	// Sixteen shared shards cap aggregate issuance at 608/minute without a
	// single global row lock becoming the horizontal scaling ceiling.
	if err = incrementRateBucket(ctx, tx, "nonce-global", rateShard(c.WalletAddress), 38); err != nil {
		return result, err
	}
	if err = incrementRateBucket(ctx, tx, "nonce-wallet", c.WalletAddress, 5); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wallet_nonces (nonce,wallet_address,domain,chain_id,purpose,version,message,issued_at,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, c.Nonce, c.WalletAddress, c.Domain, c.ChainID, c.Purpose, c.Version, c.Message, c.IssuedAt, c.ExpiresAt); err != nil {
		return result, fmt.Errorf("store authentication challenge: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return result, fmt.Errorf("commit challenge issuance: %w", err)
	}
	return c, nil
}

func rateShard(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return fmt.Sprintf("%02d", sum[0]%16)
}

func incrementRateBucket(ctx context.Context, tx *sql.Tx, scope, subject string, limit int) error {
	var count int
	err := tx.QueryRowContext(ctx, `INSERT INTO auth_rate_limit_buckets (scope,subject,bucket_start,request_count,expires_at) VALUES ($1,$2,date_trunc('minute',now()),1,date_trunc('minute',now())+interval '2 minutes') ON CONFLICT (scope,subject,bucket_start) DO UPDATE SET request_count=auth_rate_limit_buckets.request_count+1 WHERE auth_rate_limit_buckets.request_count<$3 RETURNING request_count`, scope, subject, limit).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.ErrRateLimited
	}
	if err != nil {
		return fmt.Errorf("update authentication rate bucket: %w", err)
	}
	return nil
}

func (s *Store) ConsumeChallenge(ctx context.Context, c auth.Challenge, tokenHash string, session auth.Session) (result auth.Session, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, fmt.Errorf("begin authentication transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var wallet, domain, chainID, purpose, version, message string
	var expiresAt time.Time
	var consumedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT wallet_address,domain,chain_id,purpose,version,message,expires_at,consumed_at FROM wallet_nonces WHERE nonce=$1 FOR UPDATE`, c.Nonce).Scan(&wallet, &domain, &chainID, &purpose, &version, &message, &expiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) || consumedAt.Valid {
		return result, auth.ErrNonceConsumed
	}
	if err != nil {
		return result, fmt.Errorf("lock authentication challenge: %w", err)
	}
	if wallet != c.WalletAddress || domain != c.Domain || chainID != c.ChainID || purpose != c.Purpose || version != c.Version || message != c.Message || !time.Now().UTC().Before(expiresAt) {
		return result, auth.ErrInvalidChallenge
	}
	userResult, err := tx.ExecContext(ctx, `INSERT INTO users (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, session.UserID)
	if err != nil {
		return result, fmt.Errorf("ensure user: %w", err)
	}
	created, err := userResult.RowsAffected()
	if err != nil {
		return result, fmt.Errorf("read user creation result: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wallets (wallet_address,user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, session.Wallet, session.UserID); err != nil {
		return result, fmt.Errorf("bind wallet: %w", err)
	}
	var ownerID string
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM wallets WHERE wallet_address=$1`, session.Wallet).Scan(&ownerID); err != nil {
		return result, fmt.Errorf("read wallet owner: %w", err)
	}
	if ownerID != session.UserID {
		return result, auth.ErrInvalidChallenge
	}
	if created == 1 {
		for _, role := range session.Roles {
			if _, err = tx.ExecContext(ctx, `INSERT INTO user_roles (user_id,role) VALUES ($1,$2) ON CONFLICT DO NOTHING`, session.UserID, role); err != nil {
				return result, fmt.Errorf("ensure role: %w", err)
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE wallet_nonces SET consumed_at=now() WHERE nonce=$1 AND consumed_at IS NULL`, c.Nonce)
	if err != nil {
		return result, fmt.Errorf("consume challenge: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return result, auth.ErrNonceConsumed
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sessions (session_id,token_hash,user_id,wallet_address,expires_at) VALUES ($1,$2,$3,$4,$5)`, session.SessionID, tokenHash, session.UserID, session.Wallet, session.ExpiresAt); err != nil {
		return result, fmt.Errorf("create session: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT role FROM user_roles WHERE user_id=$1 ORDER BY role`, session.UserID)
	if err != nil {
		return result, fmt.Errorf("read login roles: %w", err)
	}
	session.Roles = nil
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			_ = rows.Close()
			return result, err
		}
		session.Roles = append(session.Roles, role)
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, fmt.Errorf("commit authentication: %w", err)
	}
	return session, nil
}

func (s *Store) ReadSession(ctx context.Context, tokenHash string, now time.Time) (auth.Session, error) {
	var session auth.Session
	err := s.db.QueryRowContext(ctx, `SELECT session_id,user_id,wallet_address,expires_at FROM sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>$2`, tokenHash, now).Scan(&session.SessionID, &session.UserID, &session.Wallet, &session.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return session, auth.ErrInvalidChallenge
	}
	if err != nil {
		return session, fmt.Errorf("read session: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT role FROM user_roles WHERE user_id=$1 ORDER BY role`, session.UserID)
	if err != nil {
		return session, fmt.Errorf("read session roles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		if err = rows.Scan(&role); err != nil {
			return session, err
		}
		session.Roles = append(session.Roles, role)
	}
	return session, rows.Err()
}

func (s *Store) RevokeSession(ctx context.Context, tokenHash string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2 WHERE token_hash=$1 AND revoked_at IS NULL`, tokenHash, now)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return auth.ErrInvalidChallenge
	}
	return nil
}
