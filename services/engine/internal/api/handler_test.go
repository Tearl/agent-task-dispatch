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
	enginecredential "github.com/example/agent-platform/engine/internal/credential"
	enginedelivery "github.com/example/agent-platform/engine/internal/delivery"
	enginefinance "github.com/example/agent-platform/engine/internal/financeview"
	enginematchingview "github.com/example/agent-platform/engine/internal/matchingview"
	enginetask "github.com/example/agent-platform/engine/internal/task"
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

type apiFinanceRepository struct{ publisherCalls, agentCalls, reconciliationCalls int }

func (repo *apiFinanceRepository) Publisher(context.Context, string) (enginefinance.PublisherView, error) {
	repo.publisherCalls++
	return enginefinance.PublisherView{Tasks: []enginefinance.TaskFunds{}, Ledger: []enginefinance.LedgerRecord{}}, nil
}
func (repo *apiFinanceRepository) Agent(context.Context, string) (enginefinance.AgentView, error) {
	repo.agentCalls++
	return enginefinance.AgentView{Positions: []enginefinance.EarningPosition{}, Records: []enginefinance.LedgerRecord{}}, nil
}
func (repo *apiFinanceRepository) Reconciliation(context.Context) (enginefinance.ReconciliationView, error) {
	repo.reconciliationCalls++
	return enginefinance.ReconciliationView{Runs: []enginefinance.ReconciliationRun{}}, nil
}

func TestFinanceTransportEnforcesRoleBeforeRepository(t *testing.T) {
	repo := &apiFinanceRepository{}
	financeService, _ := enginefinance.NewService(repo)
	publisherAuth, _ := engineauth.NewService(staticSessionStore{session: engineauth.Session{UserID: "publisher", Roles: []string{"publisher"}}}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "1", Purpose: "login"})
	handler := NewHandlerWithFinance(slog.New(slog.NewTextHandler(io.Discard, nil)), publisherAuth, nil, nil, nil, nil, financeService)
	for _, test := range []struct {
		path   string
		status int
	}{{"/v1/finance/publisher", 200}, {"/v1/finance/agent", 403}, {"/v1/finance/reconciliation", 403}} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("authorization", "Bearer session")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != test.status {
			t.Fatalf("%s: %d %s", test.path, recorder.Code, recorder.Body.String())
		}
	}
	if repo.publisherCalls != 1 || repo.agentCalls != 0 || repo.reconciliationCalls != 0 {
		t.Fatalf("role boundary calls: %#v", repo)
	}
}

type apiMatchingViewRepository struct{ calls int }

func (repo *apiMatchingViewRepository) Get(context.Context, string, string) (enginematchingview.View, error) {
	repo.calls++
	return enginematchingview.View{Task: enginematchingview.Task{ID: "task-1"}}, nil
}

func TestMatchingViewTransportUsesAuthoritativePublisherSession(t *testing.T) {
	repo := &apiMatchingViewRepository{}
	service, _ := enginematchingview.NewService(repo)
	authService, _ := engineauth.NewService(staticSessionStore{session: engineauth.Session{UserID: "publisher", Roles: []string{"publisher"}}}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "1", Purpose: "login"})
	handler := NewHandlerWithMatchingView(slog.New(slog.NewTextHandler(io.Discard, nil)), authService, nil, nil, nil, nil, nil, service)
	request := httptest.NewRequest(http.MethodGet, "/v1/tasks/task-1/matching-view", nil)
	request.Header.Set("authorization", "Bearer session")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || repo.calls != 1 {
		t.Fatalf("unexpected matching response: %d %s calls=%d", recorder.Code, recorder.Body.String(), repo.calls)
	}
}

type apiDeliveryRepository struct {
	startCalls      int
	getCalls        int
	feedbackCalls   int
	acceptanceCalls int
}

func (repo *apiDeliveryRepository) Start(context.Context, enginedelivery.Mutation, string, enginedelivery.StartInput) (enginedelivery.StartResult, bool, error) {
	repo.startCalls++
	return enginedelivery.StartResult{Version: enginedelivery.Version{Number: 1, Status: enginedelivery.VersionAllocated}}, false, nil
}
func (repo *apiDeliveryRepository) Get(context.Context, string, string) (enginedelivery.View, error) {
	repo.getCalls++
	return enginedelivery.View{Package: enginedelivery.Package{ID: "package"}}, nil
}
func (repo *apiDeliveryRepository) SubmitFeedback(context.Context, enginedelivery.Mutation, string, enginedelivery.FeedbackInput, enginedelivery.FeedbackSet) (enginedelivery.FeedbackSet, bool, error) {
	repo.feedbackCalls++
	return enginedelivery.FeedbackSet{ID: "sha256:" + strings.Repeat("a", 64)}, false, nil
}
func (repo *apiDeliveryRepository) ProofContext(context.Context, string) (enginedelivery.ProofContext, error) {
	return enginedelivery.ProofContext{}, nil
}
func (repo *apiDeliveryRepository) RecordDispatched(context.Context, string) (enginedelivery.Version, bool, error) {
	return enginedelivery.Version{}, false, nil
}
func (repo *apiDeliveryRepository) RecordResult(context.Context, enginedelivery.ExecutionResult, *enginedelivery.ProofRecord) (enginedelivery.Version, bool, error) {
	return enginedelivery.Version{}, false, nil
}
func (repo *apiDeliveryRepository) ProposeChangeOrder(context.Context, enginedelivery.Mutation, string, enginedelivery.ProposeChangeOrderInput, enginedelivery.ChangeOrder) (enginedelivery.ChangeOrder, bool, error) {
	return enginedelivery.ChangeOrder{}, false, nil
}
func (repo *apiDeliveryRepository) DecideChangeOrder(context.Context, enginedelivery.Mutation, bool, string, string, enginedelivery.DecideChangeOrderInput) (enginedelivery.ChangeOrder, bool, error) {
	return enginedelivery.ChangeOrder{}, false, nil
}
func (repo *apiDeliveryRepository) AcceptChangeOrder(context.Context, enginedelivery.Mutation, string, string, enginedelivery.ChangeOrderVersionInput) (enginedelivery.ChangeOrder, bool, error) {
	return enginedelivery.ChangeOrder{}, false, nil
}
func (repo *apiDeliveryRepository) ActivateChangeOrder(context.Context, enginedelivery.Mutation, bool, string, string, enginedelivery.ChangeOrderVersionInput) (enginedelivery.ChangeOrder, bool, error) {
	return enginedelivery.ChangeOrder{}, false, nil
}
func (repo *apiDeliveryRepository) CreateAcceptanceIntent(context.Context, enginedelivery.Mutation, string, enginedelivery.AcceptanceIntentInput, enginedelivery.AcceptanceIntent) (enginedelivery.AcceptanceIntent, bool, error) {
	repo.acceptanceCalls++
	return enginedelivery.AcceptanceIntent{ID: "sha256:" + strings.Repeat("a", 64), State: enginedelivery.AcceptanceIntentRecorded}, false, nil
}
func (repo *apiDeliveryRepository) SubmitAcceptance(context.Context, enginedelivery.Mutation, string, string, enginedelivery.AcceptanceTransitionInput) (enginedelivery.AcceptanceIntent, bool, error) {
	return enginedelivery.AcceptanceIntent{}, false, nil
}
func (repo *apiDeliveryRepository) ReconcileAcceptance(context.Context, enginedelivery.Mutation, string, string, enginedelivery.AcceptanceTransitionInput) (enginedelivery.AcceptanceIntent, bool, error) {
	return enginedelivery.AcceptanceIntent{}, false, nil
}

func TestFormalDeliveryTransportUsesPublisherSessionAndIdempotencyKey(t *testing.T) {
	repo := &apiDeliveryRepository{}
	deliveryService, _ := enginedelivery.NewService(repo)
	authService, _ := engineauth.NewService(staticSessionStore{session: engineauth.Session{UserID: "publisher", Roles: []string{"publisher"}}}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "1", Purpose: "login"})
	handler := NewHandlerWithDelivery(slog.New(slog.NewTextHandler(io.Discard, nil)), authService, nil, nil, nil, nil, nil, nil, deliveryService)
	body := strings.NewReader(`{"expectedPackageVersion":0,"workNonce":1}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/tasks/task-1/formal-packages/start", body)
	request.Header.Set("authorization", "Bearer session")
	request.Header.Set("Idempotency-Key", "formal-operation")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || repo.startCalls != 1 {
		t.Fatalf("unexpected formal start response: %d %s calls=%d", recorder.Code, recorder.Body.String(), repo.startCalls)
	}
	read := httptest.NewRequest(http.MethodGet, "/v1/tasks/task-1/formal-package", nil)
	read.Header.Set("authorization", "Bearer session")
	readRecorder := httptest.NewRecorder()
	handler.ServeHTTP(readRecorder, read)
	if readRecorder.Code != http.StatusOK || repo.getCalls != 1 {
		t.Fatalf("unexpected formal read response: %d %s calls=%d", readRecorder.Code, readRecorder.Body.String(), repo.getCalls)
	}
	feedbackBody := strings.NewReader(`{"packageId":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedPackageVersion":2,"parentVersion":1,"parentContentHash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","items":[{"criterionId":"criterion","category":"defect","priority":"high","target":"artifact","description":"wrong","expectedOutcome":"fixed","scopeClaim":"in_scope"}]}`)
	feedbackRequest := httptest.NewRequest(http.MethodPost, "/v1/tasks/task-1/formal-feedback", feedbackBody)
	feedbackRequest.Header.Set("authorization", "Bearer session")
	feedbackRequest.Header.Set("Idempotency-Key", "formal-feedback")
	feedbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(feedbackRecorder, feedbackRequest)
	if feedbackRecorder.Code != http.StatusCreated || repo.feedbackCalls != 1 {
		t.Fatalf("unexpected feedback response: %d %s calls=%d", feedbackRecorder.Code, feedbackRecorder.Body.String(), repo.feedbackCalls)
	}
	acceptanceBody := strings.NewReader(`{"packageId":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedPackageVersion":2,"formalVersion":1,"contentHash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","proofDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","workNonce":1}`)
	acceptanceRequest := httptest.NewRequest(http.MethodPost, "/v1/tasks/task-1/formal-acceptance-intents", acceptanceBody)
	acceptanceRequest.Header.Set("authorization", "Bearer session")
	acceptanceRequest.Header.Set("Idempotency-Key", "formal-acceptance")
	acceptanceRecorder := httptest.NewRecorder()
	handler.ServeHTTP(acceptanceRecorder, acceptanceRequest)
	if acceptanceRecorder.Code != http.StatusCreated || repo.acceptanceCalls != 1 {
		t.Fatalf("unexpected acceptance response: %d %s calls=%d", acceptanceRecorder.Code, acceptanceRecorder.Body.String(), repo.acceptanceCalls)
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
	agent       engineagent.Agent
	healthInput engineagent.HealthInput
}

type passingHealthChecker struct{}

func (passingHealthChecker) Check(context.Context, string) error { return nil }

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
func (s *apiAgentStore) UpdateHealth(_ context.Context, _ engineagent.Mutation, _ string, input engineagent.HealthInput) (engineagent.Agent, bool, error) {
	s.healthInput = input
	return engineagent.Agent{ID: "agent-1", Health: input.Health, AggregateVersion: input.ExpectedVersion + 1}, false, nil
}
func (s *apiAgentStore) CheckHealth(ctx context.Context, mutation engineagent.Mutation, id string, input engineagent.HealthCheckInput, probe func(context.Context, string) error) (engineagent.Agent, bool, error) {
	if s.agent.ID != id || s.agent.OwnerID != mutation.ActorID {
		return engineagent.Agent{}, false, engineagent.ErrNotFound
	}
	if s.agent.AggregateVersion != input.ExpectedVersion {
		return engineagent.Agent{}, false, engineagent.ErrStaleVersion
	}
	health := engineagent.HealthHealthy
	if err := probe(ctx, s.agent.EndpointURL); err != nil {
		health = engineagent.HealthUnhealthy
	}
	s.healthInput = engineagent.HealthInput{Health: health, ExpectedVersion: input.ExpectedVersion, CheckedAt: mutation.Now}
	return engineagent.Agent{ID: id, Health: health, AggregateVersion: input.ExpectedVersion + 1}, false, nil
}
func (*apiAgentStore) UpdateCapacity(context.Context, engineagent.Mutation, string, engineagent.CapacityInput) (engineagent.Agent, bool, error) {
	return engineagent.Agent{}, false, nil
}
func (*apiAgentStore) PublishPrice(context.Context, engineagent.Mutation, string, engineagent.PriceInput) (engineagent.PriceVersion, bool, error) {
	return engineagent.PriceVersion{}, false, nil
}
func (s *apiAgentStore) Get(context.Context, string, string) (engineagent.Agent, error) {
	if s.agent.ID == "" {
		return engineagent.Agent{}, engineagent.ErrNotFound
	}
	return s.agent, nil
}
func (s *apiAgentStore) GetForActions(context.Context, string, string) (engineagent.Agent, time.Time, error) {
	if s.agent.ID == "" {
		return engineagent.Agent{}, time.Time{}, engineagent.ErrNotFound
	}
	return s.agent, time.Now().UTC(), nil
}
func (*apiAgentStore) ReserveCapacity(context.Context, string, string, time.Time) (engineagent.CapacityLease, error) {
	return engineagent.CapacityLease{}, nil
}
func (*apiAgentStore) ReleaseCapacity(context.Context, string, int64) error { return nil }

type apiTaskStore struct {
	createCalls int
	mutation    enginetask.Mutation
	task        enginetask.Task
	databaseNow time.Time
}

func (s *apiTaskStore) Create(_ context.Context, mutation enginetask.Mutation, input enginetask.DraftInput, id string) (enginetask.Task, bool, error) {
	s.createCalls++
	s.mutation = mutation
	return enginetask.Task{ID: id, PublisherID: mutation.ActorID, Status: enginetask.StatusDraft, Title: input.Title, AggregateVersion: 1}, false, nil
}
func (*apiTaskStore) UpdateDraft(context.Context, enginetask.Mutation, string, enginetask.UpdateDraftInput) (enginetask.Task, bool, error) {
	return enginetask.Task{}, false, enginetask.ErrStaleVersion
}
func (*apiTaskStore) Publish(context.Context, enginetask.Mutation, string, enginetask.PublishInput) (enginetask.Publication, bool, error) {
	return enginetask.Publication{}, false, enginetask.ErrInvalidState
}
func (*apiTaskStore) Get(context.Context, string, string) (enginetask.Task, error) {
	return enginetask.Task{}, enginetask.ErrNotFound
}
func (s *apiTaskStore) GetForActions(context.Context, string, string) (enginetask.Task, time.Time, error) {
	if s.task.ID == "" {
		return enginetask.Task{}, time.Time{}, enginetask.ErrNotFound
	}
	return s.task, s.databaseNow, nil
}

func TestTaskTransportRequiresSessionIdempotencyAndMapsDomainErrors(t *testing.T) {
	store := &apiTaskStore{}
	taskService, err := enginetask.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	authService, err := engineauth.NewService(staticSessionStore{session: engineauth.Session{UserID: "publisher", Roles: []string{"publisher"}, ExpiresAt: now.Add(time.Hour)}}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "1", Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithTaskService(slog.New(slog.NewTextHandler(io.Discard, nil)), authService, nil, nil, taskService)
	draft := enginetask.DraftInput{Title: "Research", Description: "Research the market", ExpertType: "research", Language: "en", OverviewBudget: "1", FormalBudget: "10", ExternalCostCap: "0", Deadline: now.Add(time.Hour), Inputs: []string{"data"}, AllowedTools: []string{"search"}, Exclusions: []string{"PII"}, DeliveryFormat: "markdown", AcceptanceCriteria: []enginetask.AcceptanceCriterion{{ID: "quality", Title: "Quality", Description: "Accurate", Weight: 100}}}
	body, _ := json.Marshal(draft)
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status: %d", unauthorized.Code)
	}
	missingKeyRequest := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body))
	missingKeyRequest.Header.Set("authorization", "Bearer token")
	missingKey := httptest.NewRecorder()
	handler.ServeHTTP(missingKey, missingKeyRequest)
	if missingKey.Code != http.StatusBadRequest {
		t.Fatalf("missing key status: %d %s", missingKey.Code, missingKey.Body.String())
	}
	createRequest := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(body))
	createRequest.Header.Set("authorization", "Bearer token")
	createRequest.Header.Set("Idempotency-Key", "create-task")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, createRequest)
	if created.Code != http.StatusCreated || store.createCalls != 1 || store.mutation.ActorID != "publisher" {
		t.Fatalf("create: status=%d calls=%d mutation=%#v body=%s", created.Code, store.createCalls, store.mutation, created.Body.String())
	}
	publishBody := bytes.NewBufferString(`{"expectedVersion":1}`)
	publishRequest := httptest.NewRequest(http.MethodPost, "/v1/tasks/task-1/publish", publishBody)
	publishRequest.Header.Set("authorization", "Bearer token")
	publishRequest.Header.Set("Idempotency-Key", "publish-task")
	published := httptest.NewRecorder()
	handler.ServeHTTP(published, publishRequest)
	if published.Code != http.StatusConflict {
		t.Fatalf("publish conflict status: %d %s", published.Code, published.Body.String())
	}
	getRequest := httptest.NewRequest(http.MethodGet, "/v1/tasks/other-task", nil)
	getRequest.Header.Set("authorization", "Bearer token")
	notFound := httptest.NewRecorder()
	handler.ServeHTTP(notFound, getRequest)
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("get not found status: %d", notFound.Code)
	}
}

func TestAvailableActionsTransportReturnsEngineDomainDecisions(t *testing.T) {
	now := time.Now().UTC()
	agentStore := &apiAgentStore{agent: engineagent.Agent{ID: "agent-1", OwnerID: "owner", Status: engineagent.StatusDraft, Health: engineagent.HealthUnknown, AggregateVersion: 2}}
	taskStore := &apiTaskStore{databaseNow: now, task: enginetask.Task{ID: "task-1", PublisherID: "owner", Status: enginetask.StatusDraft, AggregateVersion: 3, Deadline: now.Add(time.Hour)}}
	agentService, _ := engineagent.NewService(agentStore)
	taskService, _ := enginetask.NewService(taskStore)
	authService, err := engineauth.NewService(staticSessionStore{session: engineauth.Session{UserID: "owner", Roles: []string{"publisher", "agent_provider"}, ExpiresAt: now.Add(time.Hour)}}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "1", Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithTaskService(slog.New(slog.NewTextHandler(io.Discard, nil)), authService, agentService, nil, taskService)
	for _, test := range []struct {
		path          string
		resourceType  string
		blockedAction string
	}{
		{path: "/v1/agents/agent-1/available-actions", resourceType: "agent", blockedAction: "activate"},
		{path: "/v1/tasks/task-1/available-actions", resourceType: "task"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("authorization", "Bearer token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", test.path, recorder.Code, recorder.Body.String())
		}
		var response struct {
			ResourceType string `json:"resourceType"`
			Actions      []struct {
				Action  string `json:"action"`
				Allowed bool   `json:"allowed"`
				Reasons []struct {
					Code string `json:"code"`
				} `json:"reasons"`
			} `json:"actions"`
		}
		if err = json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.ResourceType != test.resourceType || len(response.Actions) == 0 {
			t.Fatalf("%s response: %#v err=%v", test.path, response, err)
		}
		if test.blockedAction != "" {
			found := false
			for _, decision := range response.Actions {
				if decision.Action == test.blockedAction {
					found = !decision.Allowed && len(decision.Reasons) > 0
				}
			}
			if !found {
				t.Fatalf("%s missing Engine blocking reason: %#v", test.path, response.Actions)
			}
		}
	}
	for _, test := range []struct {
		path     string
		resource string
	}{
		{path: "/v1/agents/agent-1/view", resource: "agent"},
		{path: "/v1/tasks/task-1/view", resource: "task"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("authorization", "Bearer token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", test.path, recorder.Code, recorder.Body.String())
		}
		var view map[string]json.RawMessage
		if err = json.Unmarshal(recorder.Body.Bytes(), &view); err != nil || len(view[test.resource]) == 0 || len(view["availableActions"]) == 0 {
			t.Fatalf("%s invalid view: %#v err=%v", test.path, view, err)
		}
	}
}

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
	body := `{"name":"Research Agent","category":"research","tags":["analysis"],"capabilities":"research","languages":["en"],"estimatedDurationSeconds":300,"authorBio":"provider","endpointUrl":"https://agent.example","controllerAddress":"0x1111111111111111111111111111111111111111","payoutAddress":"0x2222222222222222222222222222222222222222","maxConcurrency":2}`

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

func TestAgentHealthTransportDoesNotAcceptBrowserSuppliedResult(t *testing.T) {
	store := &apiAgentStore{agent: engineagent.Agent{ID: "agent-1", OwnerID: "owner", EndpointURL: "https://agent.example", AggregateVersion: 3}}
	agentService, err := engineagent.NewServiceWithHealthChecker(store, passingHealthChecker{})
	if err != nil {
		t.Fatal(err)
	}
	authService, err := engineauth.NewService(staticSessionStore{session: engineauth.Session{UserID: "owner", Roles: []string{"agent_provider"}}}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "1", Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), authService, agentService)
	for _, body := range []string{`{"health":"healthy","expectedVersion":3}`, `{"health":"unhealthy","expectedVersion":3}`} {
		request := httptest.NewRequest(http.MethodPost, "/v1/agents/agent-1/health", bytes.NewBufferString(body))
		request.Header.Set("authorization", "Bearer session-token")
		request.Header.Set("Idempotency-Key", "health-untrusted")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("browser health result accepted: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/agents/agent-1/health", bytes.NewBufferString(`{"expectedVersion":3}`))
	request.Header.Set("authorization", "Bearer session-token")
	request.Header.Set("Idempotency-Key", "health-check-3")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || store.healthInput.Health != engineagent.HealthHealthy || store.healthInput.ExpectedVersion != 3 {
		t.Fatalf("Engine health check not recorded: status=%d input=%#v body=%s", recorder.Code, store.healthInput, recorder.Body.String())
	}
}

type apiCredentialStore struct{ calls int }

func (s *apiCredentialStore) Rotate(_ context.Context, mutation enginecredential.Mutation, agentID string, input enginecredential.StoreInput, envelope enginecredential.Envelope) (enginecredential.Metadata, bool, error) {
	s.calls++
	return enginecredential.Metadata{AgentID: agentID, Version: 1, AgentAggregateVersion: input.ExpectedVersion + 1, CredentialType: input.CredentialType, Label: input.Label, Fingerprint: envelope.Fingerprint, CreatedAt: mutation.Now}, false, nil
}

func TestCredentialTransportNeverReturnsOrLogsSecretAndRejectsAdmin(t *testing.T) {
	store := &apiCredentialStore{}
	encryptor, err := enginecredential.NewAESGCMEncryptor(bytes.Repeat([]byte{0x33}, 32), bytes.Repeat([]byte{0x34}, 32), "transport-key-v1")
	if err != nil {
		t.Fatal(err)
	}
	credentialService, err := enginecredential.NewService(store, encryptor)
	if err != nil {
		t.Fatal(err)
	}
	providerAuth, err := engineauth.NewService(staticSessionStore{session: engineauth.Session{UserID: "owner", Roles: []string{"agent_provider"}}}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "1", Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	providerHandler := NewHandlerWithCredentials(slog.New(slog.NewJSONHandler(&logs, nil)), providerAuth, nil, credentialService)
	secret := "sk_live_transport_secret_must_not_escape"
	body := `{"credentialType":"api_key","label":"production","secret":"` + secret + `","expectedVersion":1}`
	request := httptest.NewRequest(http.MethodPost, "/v1/agents/agent-1/credentials", bytes.NewBufferString(body))
	request.Header.Set("authorization", "Bearer provider-session")
	request.Header.Set("Idempotency-Key", "rotate-1")
	recorder := httptest.NewRecorder()
	providerHandler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || store.calls != 1 {
		t.Fatalf("credential rotation: status=%d calls=%d body=%s", recorder.Code, store.calls, recorder.Body.String())
	}
	for _, output := range []string{recorder.Body.String(), logs.String()} {
		for _, forbidden := range []string{secret, "ciphertext", "nonce", "wrappedDataKey", "keyNonce", "secretDigest", "keyReference"} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("credential transport exposed %q: %s", forbidden, output)
			}
		}
	}
	adminAuth, err := engineauth.NewService(staticSessionStore{session: engineauth.Session{UserID: "admin", Roles: []string{"admin"}}}, engineauth.EthereumVerifier{}, engineauth.Config{Domain: "app.example", ChainID: "1", Purpose: "login"})
	if err != nil {
		t.Fatal(err)
	}
	adminHandler := NewHandlerWithCredentials(slog.New(slog.NewTextHandler(io.Discard, nil)), adminAuth, nil, credentialService)
	adminRequest := httptest.NewRequest(http.MethodPost, "/v1/agents/agent-1/credentials", bytes.NewBufferString(body))
	adminRequest.Header.Set("authorization", "Bearer admin-session")
	adminRequest.Header.Set("Idempotency-Key", "admin-rotate")
	adminRecorder := httptest.NewRecorder()
	adminHandler.ServeHTTP(adminRecorder, adminRequest)
	if adminRecorder.Code != http.StatusForbidden || store.calls != 1 || strings.Contains(adminRecorder.Body.String(), secret) {
		t.Fatalf("admin credential rotation: status=%d calls=%d body=%s", adminRecorder.Code, store.calls, adminRecorder.Body.String())
	}
}
