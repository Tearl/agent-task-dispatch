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
	"syscall"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	agentpostgres "github.com/example/agent-platform/engine/internal/agent/postgres"
	"github.com/example/agent-platform/engine/internal/api"
	"github.com/example/agent-platform/engine/internal/auth"
	authpostgres "github.com/example/agent-platform/engine/internal/auth/postgres"
	"github.com/example/agent-platform/engine/internal/credential"
	credentialpostgres "github.com/example/agent-platform/engine/internal/credential/postgres"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	enginetask "github.com/example/agent-platform/engine/internal/task"
	taskpostgres "github.com/example/agent-platform/engine/internal/task/postgres"
	_ "github.com/lib/pq"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	databaseURL := requiredEnv(logger, "DATABASE_URL")
	domain := requiredEnv(logger, "AUTH_DOMAIN")
	chainID := requiredEnv(logger, "EVM_CHAIN_ID")
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
	agentService, err := agent.NewService(agentStore)
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
	address := os.Getenv("ENGINE_ADDR")
	if address == "" {
		address = ":8080"
	}

	server := &http.Server{
		Addr:              address,
		Handler:           api.NewHandlerWithTaskService(logger, authService, agentService, credentialService, taskService),
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

func requiredEnv(logger *slog.Logger, name string) string {
	value := os.Getenv(name)
	if value == "" {
		logger.Error("required environment variable is missing", "name", name)
		os.Exit(1)
	}
	return value
}
