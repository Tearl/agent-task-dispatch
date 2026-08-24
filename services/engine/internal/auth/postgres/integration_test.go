//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	secpECDSA "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/example/agent-platform/engine/internal/auth"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/lib/pq"
	"golang.org/x/crypto/sha3"
)

func TestPostgresAuthenticationReplayRoleRefreshAndRevocation(t *testing.T) {
	baseURL := os.Getenv("ENGINE_TEST_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("ENGINE_TEST_POSTGRES_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("engine_t102_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", authSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	service, err := auth.NewService(store, auth.EthereumVerifier{}, auth.Config{Domain: "app.example", ChainID: "11155111", Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := secp256k1.GeneratePrivateKey()
	wallet := testEthereumAddress(key.PubKey())
	challenge, err := service.Issue(ctx, wallet)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := service.Issue(ctx, wallet)
	if err != nil || reused.Nonce != challenge.Nonce {
		t.Fatalf("active challenge was not reused: nonce=%q err=%v", reused.Nonce, err)
	}
	digest := testKeccak([]byte("\x19Ethereum Signed Message:\n"+strconv.Itoa(len([]byte(challenge.Message)))), []byte(challenge.Message))
	compact := secpECDSA.SignCompact(key, digest, false)
	sig := append(append([]byte{}, compact[1:]...), compact[0]-27)
	request := auth.VerifyRequest{Message: challenge.Message, Signature: "0x" + hex.EncodeToString(sig)}
	results := make([]auth.Session, 2)
	errs := make([]error, 2)
	var group sync.WaitGroup
	for i := range results {
		group.Add(1)
		go func(i int) { defer group.Done(); results[i], errs[i] = service.Verify(ctx, request) }(i)
	}
	group.Wait()
	var session auth.Session
	successes, replays := 0, 0
	for i, verifyErr := range errs {
		if verifyErr == nil {
			successes++
			session = results[i]
		} else if errors.Is(verifyErr, auth.ErrNonceConsumed) {
			replays++
		} else {
			t.Fatalf("verify: %v", verifyErr)
		}
	}
	if successes != 1 || replays != 1 {
		t.Fatalf("expected one session and one replay rejection: success=%d replay=%d", successes, replays)
	}
	if len(session.Roles) != 2 || session.Roles[0] != "agent_provider" || session.Roles[1] != "publisher" {
		t.Fatalf("new client roles were not persisted: %#v", session.Roles)
	}
	var plaintextCount int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE token_hash=$1`, session.Token).Scan(&plaintextCount); err != nil || plaintextCount != 0 {
		t.Fatalf("plaintext token persisted: count=%d err=%v", plaintextCount, err)
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=$1`, session.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO user_roles (user_id,role) VALUES ($1,'agent_provider')`, session.UserID); err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.Session(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.Roles) != 1 || refreshed.Roles[0] != "agent_provider" || refreshed.Token != "" {
		t.Fatalf("roles/token not refreshed safely: %#v", refreshed)
	}
	secondChallenge, err := service.Issue(ctx, wallet)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest := testKeccak([]byte("\x19Ethereum Signed Message:\n"+strconv.Itoa(len([]byte(secondChallenge.Message)))), []byte(secondChallenge.Message))
	secondCompact := secpECDSA.SignCompact(key, secondDigest, false)
	secondSignature := append(append([]byte{}, secondCompact[1:]...), secondCompact[0]-27)
	secondSession, err := service.Verify(ctx, auth.VerifyRequest{Message: secondChallenge.Message, Signature: "0x" + hex.EncodeToString(secondSignature)})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondSession.Roles) != 1 || secondSession.Roles[0] != "agent_provider" {
		t.Fatalf("login re-granted a removed role: %#v", secondSession.Roles)
	}
	rateKey, _ := secp256k1.GeneratePrivateKey()
	rateChallenge, err := service.Issue(ctx, testEthereumAddress(rateKey.PubKey()))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		extra := rateChallenge
		extra.Nonce = fmt.Sprintf("extra-%d", index)
		extra.Purpose = fmt.Sprintf("purpose-%d", index)
		extra.Message = auth.FormatMessage(extra)
		if _, err = store.SaveChallenge(ctx, extra); err != nil {
			t.Fatal(err)
		}
	}
	limited := rateChallenge
	limited.Nonce = "rate-limited"
	limited.Purpose = "rate-limited"
	limited.Message = auth.FormatMessage(limited)
	if _, err = store.SaveChallenge(ctx, limited); !errors.Is(err, auth.ErrRateLimited) {
		t.Fatalf("expected distributed issuance limit, got %v", err)
	}
	if err = service.Revoke(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Session(ctx, session.Token); err == nil {
		t.Fatal("revoked session remained valid")
	}
	var retained int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM wallet_nonces WHERE nonce=$1 AND consumed_at IS NOT NULL`, challenge.Nonce).Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("immutable consumed nonce was not retained: count=%d err=%v", retained, err)
	}
}

func testKeccak(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, part := range parts {
		_, _ = h.Write(part)
	}
	return h.Sum(nil)
}
func testEthereumAddress(key *secp256k1.PublicKey) string {
	digest := testKeccak(key.SerializeUncompressed()[1:])
	return "0x" + hex.EncodeToString(digest[len(digest)-20:])
}

func authSearchPath(databaseURL, schema string) string {
	parsed, err := url.Parse(databaseURL)
	if err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	separator := "?"
	if strings.Contains(databaseURL, "?") {
		separator = "&"
	}
	return databaseURL + separator + "search_path=" + url.QueryEscape(schema)
}
