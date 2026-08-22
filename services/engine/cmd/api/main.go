package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	agentpostgres "github.com/example/agent-platform/engine/internal/agent/postgres"
	"github.com/example/agent-platform/engine/internal/api"
	"github.com/example/agent-platform/engine/internal/auth"
	authpostgres "github.com/example/agent-platform/engine/internal/auth/postgres"
	chainprojection "github.com/example/agent-platform/engine/internal/chain"
	chainpostgres "github.com/example/agent-platform/engine/internal/chain/postgres"
	"github.com/example/agent-platform/engine/internal/credential"
	credentialpostgres "github.com/example/agent-platform/engine/internal/credential/postgres"
	"github.com/example/agent-platform/engine/internal/delivery"
	deliverypostgres "github.com/example/agent-platform/engine/internal/delivery/postgres"
	"github.com/example/agent-platform/engine/internal/financeview"
	financepostgres "github.com/example/agent-platform/engine/internal/financeview/postgres"
	"github.com/example/agent-platform/engine/internal/matchingview"
	matchingviewpostgres "github.com/example/agent-platform/engine/internal/matchingview/postgres"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/example/agent-platform/engine/internal/selection"
	selectionpostgres "github.com/example/agent-platform/engine/internal/selection/postgres"
	enginetask "github.com/example/agent-platform/engine/internal/task"
	taskpostgres "github.com/example/agent-platform/engine/internal/task/postgres"
	_ "github.com/lib/pq"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	databaseURL := requiredEnv(logger, "DATABASE_URL")
	domain := requiredEnv(logger, "AUTH_DOMAIN")
	chainID := requiredEnv(logger, "EVM_CHAIN_ID")
	escrowContract := os.Getenv("ESCROW_CONTRACT_ADDRESS")
	purpose := requiredEnv(logger, "AUTH_PURPOSE")
	credentialKeyReference := requiredEnv(logger, "AGENT_CREDENTIAL_KEY_REF")
	credentialRootKey, err := base64.StdEncoding.DecodeString(requiredEnv(logger, "AGENT_CREDENTIAL_KEK_BASE64"))
	if err != nil {
		logger.Error("credential encryption key is not valid base64")
		os.Exit(1)
	}
	credentialIdempotencyKey, err := base64.StdEncoding.DecodeString(requiredEnv(logger, "AGENT_CREDENTIAL_IDEMPOTENCY_HMAC_BASE64"))
	if err != nil {
		logger.Error("credential idempotency key is not valid base64")
		os.Exit(1)
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		logger.Error("database configuration failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	startupContext, startupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startupCancel()
	if err = db.PingContext(startupContext); err != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	if err = persistencepostgres.ApplyMigrations(startupContext, db); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}
	authStore, err := authpostgres.NewStore(db)
	if err != nil {
		logger.Error("authentication store failed", "error", err)
		os.Exit(1)
	}
	authService, err := auth.NewService(authStore, auth.EthereumVerifier{}, auth.Config{Domain: domain, ChainID: chainID, Purpose: purpose})
	if err != nil {
		logger.Error("authentication configuration failed", "error", err)
		os.Exit(1)
	}
	agentStore, err := agentpostgres.NewStore(db)
	if err != nil {
		logger.Error("agent store failed", "error", err)
		os.Exit(1)
	}
	allowPrivateAgentHealth := os.Getenv("AGENT_HEALTH_ALLOW_PRIVATE_NETWORKS") == "true"
	agentService, err := agent.NewServiceWithHealthChecker(agentStore, agent.NewProtocolHealthChecker(allowPrivateAgentHealth))
	if err != nil {
		logger.Error("agent service failed", "error", err)
		os.Exit(1)
	}
	credentialEncryptor, err := credential.NewAESGCMEncryptor(credentialRootKey, credentialIdempotencyKey, credentialKeyReference)
	if err != nil {
		logger.Error("credential encryption configuration failed", "error", err)
		os.Exit(1)
	}
	credentialStore, err := credentialpostgres.NewStore(db)
	if err != nil {
		logger.Error("credential store failed", "error", err)
		os.Exit(1)
	}
	credentialService, err := credential.NewService(credentialStore, credentialEncryptor)
	if err != nil {
		logger.Error("credential service failed", "error", err)
		os.Exit(1)
	}
	taskStore, err := taskpostgres.NewStore(db)
	if err != nil {
		logger.Error("task store failed", "error", err)
		os.Exit(1)
	}
	taskService, err := enginetask.NewService(taskStore)
	if err != nil {
		logger.Error("task service failed", "error", err)
		os.Exit(1)
	}
	financeStore, err := financepostgres.NewStore(db)
	if err != nil {
		logger.Error("finance view store failed", "error", err)
		os.Exit(1)
	}
	financeService, err := financeview.NewService(financeStore)
	if err != nil {
		logger.Error("finance view service failed", "error", err)
		os.Exit(1)
	}
	matchingViewStore, err := matchingviewpostgres.NewStore(db)
	if err != nil {
		logger.Error("matching view store failed", "error", err)
		os.Exit(1)
	}
	matchingViewService, err := matchingview.NewService(matchingViewStore)
	if err != nil {
		logger.Error("matching view service failed", "error", err)
		os.Exit(1)
	}
	fundsAsset := os.Getenv("FUNDS_ASSET")
	if fundsAsset == "" {
		fundsAsset = "evm:" + chainID + "/native"
	}
	deliveryStore, err := deliverypostgres.NewStoreWithConfig(db, chainIDIfConfigured(escrowContract, chainID), escrowContract, fundsAsset, os.Getenv("PLATFORM_INCIDENT_OWNER_ID"))
	if err != nil {
		logger.Error("formal delivery store failed", "error", err)
		os.Exit(1)
	}
	var deliverySigner delivery.ProofSigner = delivery.PendingProofSigner{}
	if signingKey := os.Getenv("DELIVERY_PROOF_SIGNING_KEY_HEX"); signingKey != "" {
		deliverySigner, err = delivery.NewECDSAProofSigner(signingKey)
		if err != nil {
			logger.Error("formal delivery proof signing configuration failed")
			os.Exit(1)
		}
	} else if escrowContract != "" {
		logger.Error("formal delivery proof signing key is required with escrow projection")
		os.Exit(1)
	}
	deliveryService, err := delivery.NewServiceWithDependencies(deliveryStore, deliveryStore, deliverySigner)
	if err != nil {
		logger.Error("formal delivery service failed", "error", err)
		os.Exit(1)
	}
	var selectionService *selection.Service
	var chainProjector *chainprojection.Projector
	var chainReconciler *chainprojection.Reconciler
	selectionContract := escrowContract
	selectionSigningKey := os.Getenv("SELECTION_PROOF_SIGNING_KEY_HEX")
	if selectionContract != "" || selectionSigningKey != "" {
		if selectionContract == "" || selectionSigningKey == "" {
			logger.Error("selection contract and proof signing key must be configured together")
			os.Exit(1)
		}
		rpcURL := requiredEnv(logger, "EVM_RPC_URL")
		projectionScope := chainprojection.Scope{
			ChainID: chainID, Contract: selectionContract,
			StartBlock:    requiredUintEnv(logger, "ESCROW_DEPLOYMENT_BLOCK"),
			Confirmations: uintEnv(logger, "EVM_CONFIRMATIONS", 12),
			MaxReorgDepth: uintEnv(logger, "EVM_MAX_REORG_DEPTH", 64),
		}
		chainSource, sourceErr := chainprojection.NewRPCSource(rpcURL, selectionContract, os.Getenv("EVM_RPC_ALLOW_PRIVATE_HTTP") == "true")
		if sourceErr != nil {
			logger.Error("chain RPC configuration failed")
			os.Exit(1)
		}
		chainStore, chainStoreErr := chainpostgres.NewStore(db)
		if chainStoreErr != nil {
			logger.Error("chain projection store failed", "error", chainStoreErr)
			os.Exit(1)
		}
		chainProjector, err = chainprojection.NewProjector(chainSource, chainStore, projectionScope)
		if err != nil {
			logger.Error("chain projector configuration failed", "error", err)
			os.Exit(1)
		}
		chainVerifier, verifierErr := chainprojection.NewVerifier(chainStore, projectionScope)
		if verifierErr != nil {
			logger.Error("chain verifier configuration failed", "error", verifierErr)
			os.Exit(1)
		}
		chainReconciler, err = chainprojection.NewReconciler(chainStore, chainSource, projectionScope)
		if err != nil {
			logger.Error("chain reconciler configuration failed", "error", err)
			os.Exit(1)
		}
		selectionStore, storeErr := selectionpostgres.NewStore(db)
		if storeErr != nil {
			logger.Error("selection store failed", "error", storeErr)
			os.Exit(1)
		}
		proofSigner, signerErr := selection.NewEIP712Signer(selectionSigningKey, chainID, selectionContract)
		if signerErr != nil {
			logger.Error("selection proof signer configuration failed")
			os.Exit(1)
		}
		selectionService, err = selection.NewService(selectionStore, agentService, proofSigner, chainVerifier, selection.Config{ChainID: chainID, ContractAddress: selectionContract, ReservationTTL: 10 * time.Minute})
		if err != nil {
			logger.Error("selection service failed", "error", err)
			os.Exit(1)
		}
	}
	address := os.Getenv("ENGINE_ADDR")
	if address == "" {
		address = ":8080"
	}

	server := &http.Server{
		Addr:              address,
		Handler:           api.NewHandlerWithDelivery(logger, authService, agentService, credentialService, taskService, selectionService, financeService, matchingViewService, deliveryService),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownSignal, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()
	if chainProjector != nil {
		go runChainProjection(shutdownSignal, logger, chainProjector, chainReconciler)
	}

	go func() {
		<-shutdownSignal.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	logger.Info("engine listening", "address", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("engine stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func chainIDIfConfigured(contract, chainID string) string {
	if contract == "" {
		return ""
	}
	return chainID
}

func runChainProjection(ctx context.Context, logger *slog.Logger, projector *chainprojection.Projector, reconciler *chainprojection.Reconciler) {
	syncTicker := time.NewTicker(5 * time.Second)
	reconcileTicker := time.NewTicker(24 * time.Hour)
	defer syncTicker.Stop()
	defer reconcileTicker.Stop()
	sync := func() {
		cursor, err := projector.SyncOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("chain projection sync failed", "error", err)
			return
		}
		if cursor.Set {
			logger.Debug("chain projection synchronized", "block", cursor.Height, "hash", cursor.Hash)
		}
	}
	reconcile := func() {
		run, err := reconciler.Run(ctx)
		if errors.Is(err, chainprojection.ErrPending) || errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			logger.Error("chain reconciliation failed", "error", err)
		} else if len(run.Differences) > 0 {
			logger.Error("chain reconciliation differences detected", "reconciliationId", run.ID, "count", len(run.Differences), "safeBlock", run.SafeHeight)
		}
	}
	sync()
	reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-syncTicker.C:
			sync()
		case <-reconcileTicker.C:
			reconcile()
		}
	}
}

func requiredEnv(logger *slog.Logger, name string) string {
	value := os.Getenv(name)
	if value == "" {
		logger.Error("required environment variable is missing", "name", name)
		os.Exit(1)
	}
	return value
}

func requiredUintEnv(logger *slog.Logger, name string) uint64 {
	value := requiredEnv(logger, name)
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		logger.Error("required environment variable is not an unsigned integer", "name", name)
		os.Exit(1)
	}
	return parsed
}

func uintEnv(logger *slog.Logger, name string, fallback uint64) uint64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		logger.Error("environment variable is not a positive unsigned integer", "name", name)
		os.Exit(1)
	}
	return parsed
}
