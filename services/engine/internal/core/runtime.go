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
	"github.com/example/agent-platform/engine/internal/overview"
	overviewpostgres "github.com/example/agent-platform/engine/internal/overview/postgres"
	engineworker "github.com/example/agent-platform/engine/internal/worker"
	"github.com/example/agent-platform/engine/internal/workflow"
)

type Config struct {
	FundsAsset              string
	MatchingSeedKeyVersion  string
	MatchingSeedSecret      []byte
	CallbackBaseURL         string
	NonceKeyVersion         string
	NonceSecret             []byte
	CallbackClockSkew       time.Duration
	ExecutionLeaseTTL       time.Duration
	AgentRequestTimeout     time.Duration
	RuntimeCredentials      map[string]execution.RuntimeCredential
	ExecutionInputBaseURL   string
	OverviewMaximumDuration time.Duration
	OverviewAllowedTools    []string
	Outbox                  outbox.Config
}

type ExecutionConfig struct {
	CallbackBaseURL     string
	NonceKeyVersion     string
	NonceSecret         []byte
	CallbackClockSkew   time.Duration
	ExecutionLeaseTTL   time.Duration
	AgentRequestTimeout time.Duration
	RuntimeCredentials  map[string]execution.RuntimeCredential
}

func (config Config) Execution() ExecutionConfig {
	return ExecutionConfig{
		CallbackBaseURL:     config.CallbackBaseURL,
		NonceKeyVersion:     config.NonceKeyVersion,
		NonceSecret:         config.NonceSecret,
		CallbackClockSkew:   config.CallbackClockSkew,
		ExecutionLeaseTTL:   config.ExecutionLeaseTTL,
		AgentRequestTimeout: config.AgentRequestTimeout,
		RuntimeCredentials:  config.RuntimeCredentials,
	}
}

type ExecutionRuntime struct {
	Executions      *execution.Service
	CallbackHandler http.Handler
	FormalHandler   *engineworker.FormalExecutionHandler
	Credentials     *execution.RuntimeCredentialProvider
}

// Runtime owns the process-level assembly of authoritative Engine modules.
// Secrets remain in the credential provider and are not handed to repositories.
type Runtime struct {
	Matching          *matching.Service
	MatchingSnapshots *matching.SnapshotService
	Funds             *funds.Service
	OverviewFunds     *funds.OverviewGateway
	Executions        *execution.Service
	Workflow          *workflow.Service
	ExecutionInputs   http.Handler
	CallbackHandler   http.Handler
	OutboxWorker      *outbox.Worker
	Credentials       *execution.RuntimeCredentialProvider
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

	executionRuntime, err := NewExecutionRuntime(db, agents, deliveries, config.Execution())
	if err != nil {
		return nil, err
	}
	workflowStore, err := workflow.NewStore(db)
	if err != nil {
		return nil, err
	}
	briefs, err := workflow.NewBriefProvider(workflowStore, config.ExecutionInputBaseURL)
	if err != nil {
		return nil, err
	}
	artifacts, err := workflow.NewArtifactReader(workflowStore, executionRuntime.Credentials, config.AgentRequestTimeout)
	if err != nil {
		return nil, err
	}
	overviewStore, err := overviewpostgres.NewStore(db)
	if err != nil {
		return nil, err
	}
	overviewService, err := overview.NewService(overviewStore, matchingStore, briefs, workflow.TargetResolver{Store: workflowStore}, overviewFunds, executionRuntime.Executions, artifacts, workflow.ToolEvidenceReader{Store: workflowStore}, overview.Config{MaximumDuration: config.OverviewMaximumDuration, AllowedTools: config.OverviewAllowedTools})
	if err != nil {
		return nil, err
	}
	workflowService, err := workflow.NewService(workflowStore, matching.NewService(nil, nil), matchingSnapshots, overviewService)
	if err != nil {
		return nil, err
	}
	inputHandler, err := workflow.NewInputHandler(workflowStore, executionRuntime.Credentials)
	if err != nil {
		return nil, err
	}
	outboxStore, err := outboxpostgres.NewStore(db)
	if err != nil {
		return nil, err
	}
	outboxWorker, err := outbox.NewWorker(outboxStore, map[string]outbox.Handler{
		engineworker.FormalExecutionTopic: executionRuntime.FormalHandler,
	}, config.Outbox)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		Matching:          matching.NewService(nil, nil),
		MatchingSnapshots: matchingSnapshots,
		Funds:             fundsService,
		OverviewFunds:     overviewFunds,
		Executions:        executionRuntime.Executions,
		Workflow:          workflowService,
		ExecutionInputs:   inputHandler,
		CallbackHandler:   executionRuntime.CallbackHandler,
		OutboxWorker:      outboxWorker,
		Credentials:       executionRuntime.Credentials,
	}, nil
}

func NewExecutionRuntime(db *sql.DB, agents *agent.Service, deliveries *delivery.Service, config ExecutionConfig) (*ExecutionRuntime, error) {
	if db == nil || agents == nil || deliveries == nil || config.AgentRequestTimeout <= 0 || config.AgentRequestTimeout > time.Minute {
		return nil, outbox.ErrInvalidInput
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
	return &ExecutionRuntime{Executions: executionService, CallbackHandler: callbackHandler, FormalHandler: formalHandler, Credentials: credentials}, nil
}
