package main

import (
	"testing"
	"time"
)

func TestConsumerConfigRequiresExplicitSQSModeAndUsesSafeDefaults(t *testing.T) {
	values := consumerEnvironment()
	config, err := loadConsumerConfig(func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	if config.SQS.ConsumerName != "formal-execution-v1" || config.SQS.WaitTime != 20*time.Second || config.SQS.VisibilityTimeout != 2*time.Minute || config.SQS.HeartbeatEvery != 30*time.Second || len(config.Execution.NonceSecret) != 32 || len(config.Execution.RuntimeCredentials) != 1 {
		t.Fatalf("unexpected defaults: %#v", config)
	}
	values["ENGINE_FORMAL_EXECUTION_TRANSPORT"] = "database"
	if _, err = loadConsumerConfig(func(name string) string { return values[name] }); err == nil {
		t.Fatal("database mode started the SQS consumer")
	}
}

func TestConsumerConfigRejectsMalformedSecretsWithoutReturningTheirValues(t *testing.T) {
	values := consumerEnvironment()
	values["EXECUTION_NONCE_SECRET_BASE64"] = "not-base64"
	if _, err := loadConsumerConfig(func(name string) string { return values[name] }); err == nil || err.Error() != "EXECUTION_NONCE_SECRET_BASE64 is not valid base64" {
		t.Fatalf("unexpected secret error: %v", err)
	}
	values = consumerEnvironment()
	values["ENGINE_AGENT_RUNTIME_CREDENTIALS_JSON"] = `{"agent-1":{"bearerToken":"secret","callbackKeyBase64":"bad","callbackKeyVersion":"v1"}}`
	if _, err := loadConsumerConfig(func(name string) string { return values[name] }); err == nil || err.Error() == values["ENGINE_AGENT_RUNTIME_CREDENTIALS_JSON"] {
		t.Fatalf("credential secret escaped in error: %v", err)
	}
}

func consumerEnvironment() map[string]string {
	return map[string]string{
		"DATABASE_URL":                          "postgres://agent:agent@localhost/agent_platform",
		"AWS_REGION":                            "us-east-1",
		"TASK_EXECUTION_QUEUE_URL":              "http://localhost:4566/000000000000/agent-formal-execution.fifo",
		"ENGINE_FORMAL_EXECUTION_TRANSPORT":     "sqs",
		"EXECUTION_CALLBACK_BASE_URL":           "https://engine.example/v1/agent-callbacks",
		"EXECUTION_NONCE_KEY_VERSION":           "nonce-v1",
		"EXECUTION_NONCE_SECRET_BASE64":         "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"ENGINE_AGENT_RUNTIME_CREDENTIALS_JSON": `{"agent-1":{"bearerToken":"transport-secret","callbackKeyBase64":"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","callbackKeyVersion":"callback-v1"}}`,
	}
}
