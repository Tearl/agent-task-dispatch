package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	secpECDSA "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	engineagent "github.com/example/agent-platform/engine/internal/agent"
	engineauth "github.com/example/agent-platform/engine/internal/auth"
	"golang.org/x/crypto/sha3"
)

func TestHealth(t *testing.T) {
	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
}

func TestAuthenticationTransportDoesNotExposeTokenFromSessionEndpoint(t *testing.T) {
	service, err := engineauth.NewService(engineauth.NewMemoryStore(), engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "11155111", Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithAuth(slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	key, _ := secp256k1.GeneratePrivateKey()
	wallet := testAddress(key.PubKey())
	nonceBody, _ := json.Marshal(nonceRequest{WalletAddress: wallet})
	nonceRecorder := httptest.NewRecorder()
	handler.ServeHTTP(nonceRecorder, httptest.NewRequest(http.MethodPost, "/v1/auth/nonce", bytes.NewReader(nonceBody)))
	var challenge engineauth.Challenge
	if err = json.Unmarshal(nonceRecorder.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	compact := secpECDSA.SignCompact(key, testPersonalHash(challenge.Message), false)
	signature := append(append([]byte{}, compact[1:]...), compact[0]-27)
	verifyBody, _ := json.Marshal(verifyRequest{Message: challenge.Message, Signature: "0x" + hex.EncodeToString(signature)})
	verifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(verifyRecorder, httptest.NewRequest(http.MethodPost, "/v1/auth/verify", bytes.NewReader(verifyBody)))
	if verifyRecorder.Code != http.StatusCreated {
		t.Fatalf("verify: %d %s", verifyRecorder.Code, verifyRecorder.Body.String())
	}
	var session engineauth.Session
	if err = json.Unmarshal(verifyRecorder.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatalf("session: %#v %v", session, err)
	}
	sessionRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/session", nil)
	sessionRequest.Header.Set("authorization", "Bearer "+session.Token)
	sessionRecorder := httptest.NewRecorder()
	handler.ServeHTTP(sessionRecorder, sessionRequest)
	if sessionRecorder.Code != http.StatusOK || bytes.Contains(sessionRecorder.Body.Bytes(), []byte(session.Token)) || bytes.Contains(sessionRecorder.Body.Bytes(), []byte(`"token"`)) {
		t.Fatalf("session token exposed: %d %s", sessionRecorder.Code, sessionRecorder.Body.String())
	}
	logoutRequest := httptest.NewRequest(http.MethodDelete, "/v1/auth/session", nil)
	logoutRequest.Header.Set("authorization", "Bearer "+session.Token)
	logoutRecorder := httptest.NewRecorder()
	handler.ServeHTTP(logoutRecorder, logoutRequest)
	if logoutRecorder.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", logoutRecorder.Code)
	}
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, sessionRequest)
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status: %d", rejected.Code)
	}
}

type failingAuthStore struct{ memory *engineauth.MemoryStore }

func (s failingAuthStore) SaveChallenge(ctx context.Context, challenge engineauth.Challenge) (engineauth.Challenge, error) {
	return s.memory.SaveChallenge(ctx, challenge)
}
func (failingAuthStore) ConsumeChallenge(context.Context, engineauth.Challenge, string, engineauth.Session) (engineauth.Session, error) {
	return engineauth.Session{}, errors.New("database unavailable")
}
func (failingAuthStore) ReadSession(context.Context, string, time.Time) (engineauth.Session, error) {
	return engineauth.Session{}, errors.New("database unavailable")
}
func (failingAuthStore) RevokeSession(context.Context, string, time.Time) error {
	return errors.New("database unavailable")
}

func TestAuthenticationStoreFailuresReturnServiceUnavailable(t *testing.T) {
	service, err := engineauth.NewService(failingAuthStore{memory: engineauth.NewMemoryStore()}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "11155111", Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithAuth(slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	key, _ := secp256k1.GeneratePrivateKey()
	challenge, err := service.Issue(context.Background(), testAddress(key.PubKey()))
	if err != nil {
		t.Fatal(err)
	}
	compact := secpECDSA.SignCompact(key, testPersonalHash(challenge.Message), false)
	signature := append(append([]byte{}, compact[1:]...), compact[0]-27)
	body, _ := json.Marshal(verifyRequest{Message: challenge.Message, Signature: "0x" + hex.EncodeToString(signature)})
	verifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(verifyRecorder, httptest.NewRequest(http.MethodPost, "/v1/auth/verify", bytes.NewReader(body)))
	if verifyRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("verify store failure: %d", verifyRecorder.Code)
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		request := httptest.NewRequest(method, "/v1/auth/session", nil)
		request.Header.Set("authorization", "Bearer token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s store failure: %d", method, recorder.Code)
		}
	}
}

func testPersonalHash(message string) []byte {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write([]byte("\x19Ethereum Signed Message:\n" + strconv.Itoa(len([]byte(message)))))
	_, _ = h.Write([]byte(message))
	return h.Sum(nil)
}
func testAddress(key *secp256k1.PublicKey) string {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write(key.SerializeUncompressed()[1:])
	digest := h.Sum(nil)
	return "0x" + hex.EncodeToString(digest[len(digest)-20:])
}

func TestCreateNonce(t *testing.T) {
	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	body, _ := json.Marshal(nonceRequest{WalletAddress: "0x1111111111111111111111111111111111111111"})
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/nonce", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
	}
}

func TestCreateNonceRejectsInvalidWallet(t *testing.T) {
	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/nonce",
		bytes.NewBufferString(`{"walletAddress":"not-a-wallet"}`),
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

type staticSessionStore struct{ session engineauth.Session }

func (s staticSessionStore) SaveChallenge(_ context.Context, challenge engineauth.Challenge) (engineauth.Challenge, error) {
	return challenge, nil
}
func (staticSessionStore) ConsumeChallenge(context.Context, engineauth.Challenge, string, engineauth.Session) (engineauth.Session, error) {
	return engineauth.Session{}, errors.New("not implemented")
}
func (s staticSessionStore) ReadSession(context.Context, string, time.Time) (engineauth.Session, error) {
	return s.session, nil
}
func (staticSessionStore) RevokeSession(context.Context, string, time.Time) error { return nil }

type apiAgentStore struct {
	createCalls int
	mutation    engineagent.Mutation
}

func (s *apiAgentStore) Create(_ context.Context, mutation engineagent.Mutation, input engineagent.CreateInput, id string) (engineagent.Agent, bool, error) {
	s.createCalls++
	s.mutation = mutation
	return engineagent.Agent{ID: id, OwnerID: mutation.ActorID, Name: input.Name, AggregateVersion: 1, Status: engineagent.StatusDraft}, false, nil
}
func (*apiAgentStore) UpdateProfile(context.Context, engineagent.Mutation, string, engineagent.ProfileInput) (engineagent.Agent, bool, error) {
	return engineagent.Agent{}, false, engineagent.ErrNotFound
}
func (*apiAgentStore) Transition(context.Context, engineagent.Mutation, string, engineagent.LifecycleInput) (engineagent.Agent, bool, error) {
	return engineagent.Agent{}, false, engineagent.ErrStaleVersion
}
func (*apiAgentStore) UpdateHealth(context.Context, engineagent.Mutation, string, engineagent.HealthInput) (engineagent.Agent, bool, error) {
	return engineagent.Agent{}, false, nil
}
func (*apiAgentStore) UpdateCapacity(context.Context, engineagent.Mutation, string, engineagent.CapacityInput) (engineagent.Agent, bool, error) {
	return engineagent.Agent{}, false, nil
}
func (*apiAgentStore) PublishPrice(context.Context, engineagent.Mutation, string, engineagent.PriceInput) (engineagent.PriceVersion, bool, error) {
	return engineagent.PriceVersion{}, false, nil
}
func (*apiAgentStore) Get(context.Context, string, string) (engineagent.Agent, error) {
	return engineagent.Agent{}, engineagent.ErrNotFound
}
func (*apiAgentStore) ReserveCapacity(context.Context, string, string, time.Time) (engineagent.CapacityLease, error) {
	return engineagent.CapacityLease{}, nil
}
func (*apiAgentStore) ReleaseCapacity(context.Context, string, int64) error { return nil }

func TestAgentTransportAuthenticatesValidatesAndMapsConflicts(t *testing.T) {
	store := &apiAgentStore{}
	agentService, err := engineagent.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	authService, err := engineauth.NewService(staticSessionStore{session: engineauth.Session{UserID: "owner", Roles: []string{"agent_provider"}}}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "1", Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), authService, agentService)
	body := `{"name":"Research Agent","category":"research","tags":["analysis"],"capabilities":"research","languages":["en"],"estimatedDurationSeconds":300,"authorBio":"provider","controllerAddress":"0x1111111111111111111111111111111111111111","payoutAddress":"0x2222222222222222222222222222222222222222","maxConcurrency":2}`

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(body)))
	if unauthorized.Code != http.StatusUnauthorized || store.createCalls != 0 {
		t.Fatalf("unauthorized create: status=%d calls=%d", unauthorized.Code, store.createCalls)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(body))
	request.Header.Set("authorization", "Bearer session-token")
	request.Header.Set("Idempotency-Key", "create-1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || store.createCalls != 1 || store.mutation.ActorID != "owner" || store.mutation.IdempotencyKey != "create-1" {
		t.Fatalf("create transport: status=%d calls=%d mutation=%#v body=%s", recorder.Code, store.createCalls, store.mutation, recorder.Body.String())
	}
	conflictRequest := httptest.NewRequest(http.MethodPost, "/v1/agents/agent-1/lifecycle", bytes.NewBufferString(`{"status":"paused","expectedVersion":1}`))
	conflictRequest.Header.Set("authorization", "Bearer session-token")
	conflictRequest.Header.Set("Idempotency-Key", "transition-1")
	conflict := httptest.NewRecorder()
	handler.ServeHTTP(conflict, conflictRequest)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale version mapping: status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	unknownRequest := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(strings.TrimSuffix(body, "}")+`,"secret":"must-not-pass"}`))
	unknownRequest.Header.Set("authorization", "Bearer session-token")
	unknownRequest.Header.Set("Idempotency-Key", "create-2")
	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, unknownRequest)
	if unknown.Code != http.StatusBadRequest || store.createCalls != 1 {
		t.Fatalf("unknown field: status=%d calls=%d", unknown.Code, store.createCalls)
	}
}
