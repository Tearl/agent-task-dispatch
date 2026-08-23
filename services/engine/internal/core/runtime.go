package core

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/delivery"
	"github.com/example/agent-platform/engine/internal/execution"
	executionpostgres "github.com/example/agent-platform/engine/internal/execution/postgres"
	"github.com/example/agent-platform/engine/internal/funds"
	fundspostgres "github.com/example/agent-platform/engine/internal/funds/postgres"
	"github.com/example/agent-platform/engine/internal/matching"
	matchingpostgres "github.com/example/agent-platform/engine/internal/matching/postgres"
	"github.com/example/agent-platform/engine/internal/outbox"
	outboxpostgres "github.com/example/agent-platform/engine/internal/outbox/postgres"
	engineworker "github.com/example/agent-platform/engine/internal/worker"
)

type Config struct {
	FundsAsset             string
	MatchingSeedKeyVersion string
	MatchingSeedSecret     []byte
	CallbackBaseURL        string
	NonceKeyVersion        string
	NonceSecret            []byte
	CallbackClockSkew      time.Duration
	ExecutionLeaseTTL      time.Duration
	AgentRequestTimeout    time.Duration
	RuntimeCredentials     map[string]execution.RuntimeCredential
	Outbox                 outbox.Config
}

// Runtime owns the process-level assembly of authoritative Engine modules.
// Secrets remain in the credential provider and are not handed to repositories.
type Runtime struct {
	Matching          *matching.Service
	MatchingSnapshots *matching.SnapshotService
	Funds             *funds.Service
	OverviewFunds     *funds.OverviewGateway
	Executions        *execution.Service
	CallbackHandler   http.Handler
	OutboxWorker      *outbox.Worker
}

func NewRuntime(db *sql.DB, agents *agent.Service, deliveries *delivery.Service, config Config) (*Runtime, error) {
	if db == nil || agents == nil || deliveries == nil || config.Outbox.BatchSize < 1 || config.AgentRequestTimeout <= 0 || config.AgentRequestTimeout >= config.Outbox.Lease/time.Duration(config.Outbox.BatchSize) {
		return nil, outbox.ErrInvalidInput
	}

	matchingStore, err := matchingpostgres.NewStore(db)
	if err != nil {
		return nil, err
	}
	matchingSnapshots, err := matching.NewSnapshotService(matchingStore, matching.DefaultShufflePolicy(config.MatchingSeedKeyVersion, config.MatchingSeedSecret))
	if err != nil {
		return nil, err
	}

	fundsStore, err := fundspostgres.NewStore(db)
	if err != nil {
		return nil, err
	}
	fundsService, err := funds.NewService(fundsStore, config.FundsAsset)
	if err != nil {
		return nil, err
	}
	overviewFunds, err := funds.NewOverviewGateway(fundsService)
	if err != nil {
		return nil, err
	}

	credentials, err := execution.NewRuntimeCredentialProvider(config.RuntimeCredentials)
	if err != nil {
		return nil, err
	}
	agentClient, err := execution.NewHTTPClient(&http.Client{Timeout: config.AgentRequestTimeout}, credentials)
	if err != nil {
		return nil, err
	}
	callbackVerifier, err := execution.NewCallbackVerifier(credentials, config.CallbackClockSkew)
	if err != nil {
		return nil, err
	}
	executionStore, err := executionpostgres.NewStore(db)
	if err != nil {
		return nil, err
	}
	executionService, err := execution.NewService(executionStore, agents, agentClient, callbackVerifier, execution.Config{
		CallbackBaseURL: config.CallbackBaseURL,
		NonceKeyVersion: config.NonceKeyVersion,
		NonceSecret:     config.NonceSecret,
		LeaseTTL:        config.ExecutionLeaseTTL,
	})
	if err != nil {
		return nil, err
	}
	callbackHandler, err := execution.NewCallbackHandler(executionService)
	if err != nil {
		return nil, err
	}

	formalHandler, err := engineworker.NewFormalExecutionHandler(executionService, deliveries)
	if err != nil {
		return nil, err
	}
	outboxStore, err := outboxpostgres.NewStore(db)
	if err != nil {
		return nil, err
	}
	outboxWorker, err := outbox.NewWorker(outboxStore, map[string]outbox.Handler{
		engineworker.FormalExecutionTopic: formalHandler,
	}, config.Outbox)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		Matching:          matching.NewService(nil, nil),
		MatchingSnapshots: matchingSnapshots,
		Funds:             fundsService,
		OverviewFunds:     overviewFunds,
		Executions:        executionService,
		CallbackHandler:   callbackHandler,
		OutboxWorker:      outboxWorker,
	}, nil
}
