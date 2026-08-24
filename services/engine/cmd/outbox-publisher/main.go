package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/example/agent-platform/engine/internal/outbox"
	"github.com/example/agent-platform/engine/internal/outbox/awspublisher"
	outboxpostgres "github.com/example/agent-platform/engine/internal/outbox/postgres"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	_ "github.com/lib/pq"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := loadPublisherConfig(os.Getenv)
	if err != nil {
		logger.Error("outbox publisher configuration failed", "error", err)
		os.Exit(1)
	}
	if config.Outbox.WorkerID == "" {
		hostname, _ := os.Hostname()
		config.Outbox.WorkerID = hostname + "-publisher-" + strconv.Itoa(os.Getpid())
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err = run(ctx, logger, config); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("outbox publisher stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, config publisherConfig) error {
	db, err := sql.Open("postgres", config.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database configuration failed: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	startupContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = db.PingContext(startupContext); err != nil {
		return fmt.Errorf("database unavailable: %w", err)
	}
	if err = persistencepostgres.ApplyMigrations(startupContext, db); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}
	sdkConfig, err := awsconfig.LoadDefaultConfig(startupContext, awsconfig.WithRegion(config.AWSRegion))
	if err != nil {
		return fmt.Errorf("AWS configuration failed: %w", err)
	}
	snsClient := sns.NewFromConfig(sdkConfig, func(options *sns.Options) {
		if config.AWSEndpointURL != "" {
			options.BaseEndpoint = aws.String(config.AWSEndpointURL)
		}
	})
	sqsClient := sqs.NewFromConfig(sdkConfig, func(options *sqs.Options) {
		if config.AWSEndpointURL != "" {
			options.BaseEndpoint = aws.String(config.AWSEndpointURL)
		}
	})
	handlers, err := publisherHandlers(config, snsClient, sqsClient)
	if err != nil {
		return fmt.Errorf("publisher routes failed: %w", err)
	}
	store, err := outboxpostgres.NewStore(db)
	if err != nil {
		return fmt.Errorf("outbox store failed: %w", err)
	}
	worker, err := outbox.NewWorker(store, handlers, config.Outbox)
	if err != nil {
		return fmt.Errorf("outbox worker failed: %w", err)
	}
	logger.Info("outbox publisher started", "routeCount", len(handlers), "formalTransport", config.FormalTransport)
	return worker.Run(ctx)
}

func publisherHandlers(config publisherConfig, snsClient awspublisher.SNSClient, sqsClient awspublisher.SQSClient) (map[string]outbox.Handler, error) {
	handlers := make(map[string]outbox.Handler, 4)
	taskHandler, err := awspublisher.NewSNSHandler(snsClient, awspublisher.TaskEventsTopic, config.TaskEventsTopicARN, config.PublishTimeout)
	if err != nil {
		return nil, err
	}
	handlers[awspublisher.TaskEventsTopic] = taskHandler
	agentHandler, err := awspublisher.NewSNSHandler(snsClient, awspublisher.AgentEventsTopic, config.AgentEventsTopicARN, config.PublishTimeout)
	if err != nil {
		return nil, err
	}
	handlers[awspublisher.AgentEventsTopic] = agentHandler
	adminHandler, err := awspublisher.NewSQSHandler(sqsClient, awspublisher.AdminOperationTopic, config.AdminOperationQueueURL, true, config.PublishTimeout)
	if err != nil {
		return nil, err
	}
	handlers[awspublisher.AdminOperationTopic] = adminHandler
	if config.FormalTransport == formalTransportSQS {
		formalHandler, formalErr := awspublisher.NewSQSHandler(sqsClient, awspublisher.FormalExecutionTopic, config.FormalExecutionQueueURL, true, config.PublishTimeout)
		if formalErr != nil {
			return nil, formalErr
		}
		handlers[awspublisher.FormalExecutionTopic] = formalHandler
	}
	return handlers, nil
}
