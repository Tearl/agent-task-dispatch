package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/example/agent-platform/engine/internal/agent"
	agentpostgres "github.com/example/agent-platform/engine/internal/agent/postgres"
	enginecore "github.com/example/agent-platform/engine/internal/core"
	"github.com/example/agent-platform/engine/internal/delivery"
	deliverypostgres "github.com/example/agent-platform/engine/internal/delivery/postgres"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/example/agent-platform/engine/internal/sqsconsumer"
	consumerpostgres "github.com/example/agent-platform/engine/internal/sqsconsumer/postgres"
	_ "github.com/lib/pq"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := loadConsumerConfig(os.Getenv)
	if err != nil {
		logger.Error("formal execution consumer configuration failed", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err = run(ctx, logger, config); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("formal execution consumer stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, config consumerConfig) error {
	db, err := sql.Open("postgres", config.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database configuration failed: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	startupContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = db.PingContext(startupContext); err != nil {
		return fmt.Errorf("database unavailable: %w", err)
	}
	if err = persistencepostgres.ApplyMigrations(startupContext, db); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}
	agentStore, err := agentpostgres.NewStore(db)
	if err != nil {
		return fmt.Errorf("Agent store failed: %w", err)
	}
	agentService, err := agent.NewService(agentStore)
	if err != nil {
		return fmt.Errorf("Agent service failed: %w", err)
	}
	deliveryStore, err := deliverypostgres.NewStore(db)
	if err != nil {
		return fmt.Errorf("formal delivery store failed: %w", err)
	}
	deliveryService, err := delivery.NewService(deliveryStore)
	if err != nil {
		return fmt.Errorf("formal delivery service failed: %w", err)
	}
	executionRuntime, err := enginecore.NewExecutionRuntime(db, agentService, deliveryService, config.Execution)
	if err != nil {
		return fmt.Errorf("execution runtime failed: %w", err)
	}
	ledger, err := consumerpostgres.NewStore(db)
	if err != nil {
		return fmt.Errorf("processed-message ledger failed: %w", err)
	}
	sdkConfig, err := awsconfig.LoadDefaultConfig(startupContext, awsconfig.WithRegion(config.AWSRegion))
	if err != nil {
		return fmt.Errorf("AWS configuration failed: %w", err)
	}
	client := sqs.NewFromConfig(sdkConfig, func(options *sqs.Options) {
		if config.AWSEndpointURL != "" {
			options.BaseEndpoint = aws.String(config.AWSEndpointURL)
		}
	})
	consumer, err := sqsconsumer.New(client, ledger, executionRuntime.FormalHandler, config.SQS)
	if err != nil {
		return fmt.Errorf("SQS consumer failed: %w", err)
	}
	logger.Info("formal execution consumer started", "consumerName", config.SQS.ConsumerName)
	for {
		result, consumeErr := consumer.RunOnce(ctx)
		if consumeErr != nil {
			return consumeErr
		}
		if result.Outcome == sqsconsumer.OutcomeRetry {
			logger.Warn("formal execution message scheduled for retry", "failureCode", result.FailureCode, "permanent", result.Permanent, "receiveCount", result.ReceiveCount)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
}
