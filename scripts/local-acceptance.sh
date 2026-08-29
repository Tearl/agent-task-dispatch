#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPOSITORY_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
STATE_DIR="${LOCAL_ACCEPTANCE_STATE_DIR:-$REPOSITORY_ROOT/.local/acceptance}"
COMPOSE_PROJECT="${LOCAL_ACCEPTANCE_COMPOSE_PROJECT:-agent-platform-acceptance}"
POSTGRES_PORT_VALUE="${LOCAL_ACCEPTANCE_POSTGRES_PORT:-55432}"
REDIS_PORT_VALUE="${LOCAL_ACCEPTANCE_REDIS_PORT:-56379}"
LOCALSTACK_PORT_VALUE="${LOCAL_ACCEPTANCE_LOCALSTACK_PORT:-14566}"
ANVIL_PORT="${LOCAL_ACCEPTANCE_ANVIL_PORT:-18545}"
ANVIL_RPC_URL="${LOCAL_ACCEPTANCE_ANVIL_RPC_URL:-http://127.0.0.1:$ANVIL_PORT}"
ANVIL_CHAIN_ID="${LOCAL_ACCEPTANCE_ANVIL_CHAIN_ID:-31337}"
DATABASE_URL="${LOCAL_ACCEPTANCE_POSTGRES_URL:-postgres://agent:agent@127.0.0.1:$POSTGRES_PORT_VALUE/agent_platform?sslmode=disable}"
LOCALSTACK_ENDPOINT="${LOCAL_ACCEPTANCE_LOCALSTACK_ENDPOINT:-http://127.0.0.1:$LOCALSTACK_PORT_VALUE}"
AWS_REGION_VALUE="${LOCAL_ACCEPTANCE_AWS_REGION:-us-east-1}"
PROOF_SIGNER_ADDRESS="${LOCAL_ACCEPTANCE_PROOF_SIGNER_ADDRESS:-0x70997970c51812dc3a010c7d01b50e0d17dc79c8}"
GOCACHE="${LOCAL_ACCEPTANCE_GO_CACHE:-$STATE_DIR/go-build}"

export AWS_ACCESS_KEY_ID="test"
export AWS_SECRET_ACCESS_KEY="test"
export AWS_DEFAULT_REGION="$AWS_REGION_VALUE"
export AWS_PAGER=""
export GOCACHE
export AGENT_PLATFORM_POSTGRES_PORT="$POSTGRES_PORT_VALUE"
export AGENT_PLATFORM_REDIS_PORT="$REDIS_PORT_VALUE"
export AGENT_PLATFORM_LOCALSTACK_PORT="$LOCALSTACK_PORT_VALUE"
export AGENT_PLATFORM_ANVIL_PORT="$ANVIL_PORT"

say() {
  printf '\n==> %s\n' "$1"
}

fail() {
  printf 'local acceptance: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is missing: $1"
}

require_base_commands() {
  mkdir -p "$GOCACHE"
  for acceptance_command in docker go pnpm forge cast jq aws curl openssl; do
    require_command "$acceptance_command"
  done
  docker compose version >/dev/null 2>&1 || fail "Docker Compose is unavailable"
}

compose() {
  docker compose --project-name "$COMPOSE_PROJECT" --project-directory "$REPOSITORY_ROOT" -f "$REPOSITORY_ROOT/compose.yaml" --profile acceptance "$@"
}

aws_local() {
  aws --endpoint-url "$LOCALSTACK_ENDPOINT" --region "$AWS_REGION_VALUE" "$@"
}

start_infrastructure() {
  say "Starting PostgreSQL, Redis, LocalStack, and Anvil"
  compose up -d --wait --wait-timeout 180 postgres redis localstack anvil
}

deploy_escrow() {
  say "Deploying the local TaskEscrow contract"
  (
    cd "$REPOSITORY_ROOT"
    EVM_RPC_URL="$ANVIL_RPC_URL" \
      SELECTION_PROOF_SIGNER_ADDRESS="$PROOF_SIGNER_ADDRESS" \
      sh ./scripts/deploy-local-escrow.sh
  )
}

migrate_postgres() {
  say "Applying Engine PostgreSQL migrations"
  (
    cd "$REPOSITORY_ROOT/services/engine"
    DATABASE_URL="$DATABASE_URL" go run ./cmd/migrate
  )
}

smoke_postgres() {
  say "Checking PostgreSQL migrations"
  compose exec -T postgres pg_isready -U agent -d agent_platform >/dev/null
  acceptance_expected="$(find "$REPOSITORY_ROOT/services/engine/internal/persistence/postgres/migrations" -name '*.up.sql' -type f | wc -l | tr -d ' ')"
  acceptance_applied="$(compose exec -T postgres psql -U agent -d agent_platform -Atqc 'SELECT count(*) FROM schema_migrations')"
  [ "$acceptance_applied" = "$acceptance_expected" ] || fail "PostgreSQL has $acceptance_applied migrations, expected $acceptance_expected"
  printf 'PostgreSQL is ready with %s migrations.\n' "$acceptance_applied"
}

smoke_redis() {
  say "Checking Redis"
  acceptance_redis="$(compose exec -T redis redis-cli ping | tr -d '\r')"
  [ "$acceptance_redis" = "PONG" ] || fail "Redis ping returned $acceptance_redis"
  printf 'Redis is ready.\n'
}

queue_url() {
  aws_local sqs get-queue-url --queue-name "$1" --query QueueUrl --output text
}

assert_redrive_policy() {
  acceptance_queue_url="$(queue_url "$1")"
  acceptance_attributes="$(aws_local sqs get-queue-attributes --queue-url "$acceptance_queue_url" --attribute-names RedrivePolicy FifoQueue --output json)"
  printf '%s' "$acceptance_attributes" | jq -e '.Attributes.RedrivePolicy | fromjson | .maxReceiveCount == "5"' >/dev/null || fail "$1 has no valid redrive policy"
  case "$1" in
    *.fifo)
      printf '%s' "$acceptance_attributes" | jq -e '.Attributes.FifoQueue == "true"' >/dev/null || fail "$1 is not FIFO"
      ;;
  esac
}

smoke_localstack() {
  say "Checking LocalStack topics, queues, redrive, and message I/O"
  curl -fsS "$LOCALSTACK_ENDPOINT/_localstack/health" | jq -e \
    '.services as $services | (["available", "running"] | index($services.sns)) != null and (["available", "running"] | index($services.sqs)) != null' >/dev/null

  acceptance_topics="$(aws_local sns list-topics --query 'Topics[].TopicArn' --output text)"
  printf '%s\n' "$acceptance_topics" | grep -q 'agent-task-events' || fail "agent-task-events topic is missing"
  printf '%s\n' "$acceptance_topics" | grep -q 'agent-events' || fail "agent-events topic is missing"

  for acceptance_queue in \
    agent-task-events-projection-dlq \
    agent-task-events-projection \
    agent-events-projection-dlq \
    agent-events-projection \
    agent-formal-execution-dlq.fifo \
    agent-formal-execution.fifo \
    admin-operations-dlq.fifo \
    admin-operations.fifo; do
    queue_url "$acceptance_queue" >/dev/null
  done

  for acceptance_queue in \
    agent-task-events-projection \
    agent-events-projection \
    agent-formal-execution.fifo \
    admin-operations.fifo; do
    assert_redrive_policy "$acceptance_queue"
  done

  acceptance_smoke_queue="agent-acceptance-smoke-$$"
  acceptance_smoke_url="$(aws_local sqs create-queue --queue-name "$acceptance_smoke_queue" --query QueueUrl --output text)"
  aws_local sqs send-message --queue-url "$acceptance_smoke_url" --message-body 'local-acceptance-smoke' >/dev/null
  acceptance_receipt="$(aws_local sqs receive-message --queue-url "$acceptance_smoke_url" --wait-time-seconds 2 --max-number-of-messages 1 --query 'Messages[0].ReceiptHandle' --output text)"
  [ -n "$acceptance_receipt" ] && [ "$acceptance_receipt" != "None" ] || fail "LocalStack SQS did not return the smoke message"
  aws_local sqs delete-message --queue-url "$acceptance_smoke_url" --receipt-handle "$acceptance_receipt"
  aws_local sqs delete-queue --queue-url "$acceptance_smoke_url"
  printf 'LocalStack topics and queues are ready.\n'
}

env_chain_value() {
  sed -n "s/^$1=//p" "$REPOSITORY_ROOT/.env.chain" | tail -n 1
}

smoke_anvil() {
  say "Checking Anvil and the deployed TaskEscrow"
  acceptance_chain="$(cast chain-id --rpc-url "$ANVIL_RPC_URL")"
  [ "$acceptance_chain" = "$ANVIL_CHAIN_ID" ] || fail "Anvil chain ID is $acceptance_chain, expected $ANVIL_CHAIN_ID"
  [ -f "$REPOSITORY_ROOT/.env.chain" ] || fail ".env.chain was not generated"
  acceptance_contract="$(env_chain_value ESCROW_CONTRACT_ADDRESS)"
  acceptance_resolver="$(env_chain_value DISPUTE_RESOLVER_ADDRESS)"
  [ -n "$acceptance_contract" ] || fail "ESCROW_CONTRACT_ADDRESS is missing from .env.chain"
  acceptance_code="$(cast code "$acceptance_contract" --rpc-url "$ANVIL_RPC_URL")"
  [ "$acceptance_code" != "0x" ] && [ -n "$acceptance_code" ] || fail "TaskEscrow has no deployed bytecode"
  acceptance_chain_resolver="$(cast call "$acceptance_contract" 'disputeResolver()(address)' --rpc-url "$ANVIL_RPC_URL" | tr '[:upper:]' '[:lower:]')"
  [ "$acceptance_chain_resolver" = "$acceptance_resolver" ] || fail "TaskEscrow resolver does not match .env.chain"
  (
    cd "$REPOSITORY_ROOT"
    EVM_RPC_URL="$ANVIL_RPC_URL" sh ./scripts/verify-work-nonce-anvil.sh
  )
  printf 'Anvil chain %s and TaskEscrow %s are ready.\n' "$acceptance_chain" "$acceptance_contract"
}

run_smoke() {
  smoke_postgres
  smoke_redis
  smoke_localstack
  smoke_anvil
}

run_engine_integration() {
  say "Running PostgreSQL integration tests without skips"
  mkdir -p "$STATE_DIR"
  acceptance_log="$STATE_DIR/engine-integration.json"
  if ! (
    cd "$REPOSITORY_ROOT/services/engine"
    ENGINE_TEST_POSTGRES_URL="$DATABASE_URL" go test -tags=integration -count=1 -json ./...
  ) >"$acceptance_log"; then
    jq -s -r '
      ([.[] | select(.Action == "fail") | .Package] | unique) as $failed |
      .[] as $event |
      select($event.Action == "output" and ($failed | index($event.Package))) |
      $event.Output // empty
    ' "$acceptance_log" >&2 || true
    fail "Engine PostgreSQL integration tests failed"
  fi
  if jq -e 'select(.Action == "skip" and .Test != null)' "$acceptance_log" >/dev/null; then
    jq -r 'select(.Action == "skip" and .Test != null) | "skipped: \(.Package) \(.Test)"' "$acceptance_log" >&2
    fail "Engine PostgreSQL integration tests skipped tests"
  fi
  acceptance_test_count="$(jq -s '[.[] | select(.Action == "pass" and .Test != null)] | length' "$acceptance_log")"
  printf 'Engine PostgreSQL integration tests passed (%s tests, zero skipped).\n' "$acceptance_test_count"
}

run_quality_gates() {
  say "Running repository type checks"
  (cd "$REPOSITORY_ROOT" && pnpm check-types)
  say "Running repository unit tests"
  (cd "$REPOSITORY_ROOT" && pnpm test)
  say "Running repository builds"
  (cd "$REPOSITORY_ROOT" && pnpm build)
  say "Running the Engine race detector"
  (cd "$REPOSITORY_ROOT/services/engine" && go test -race -count=1 ./...)
  run_engine_integration
}

acceptance_up() {
  require_base_commands
  start_infrastructure
  deploy_escrow
  migrate_postgres
}

acceptance_down() {
  say "Stopping local acceptance services"
  compose down
}

case "${1:-verify}" in
  up)
    acceptance_up
    ;;
  smoke)
    require_base_commands
    run_smoke
    ;;
  verify)
    acceptance_up
    run_smoke
    run_quality_gates
    say "Local acceptance verification passed"
    printf 'Services remain running for inspection. Stop them with: pnpm acceptance:down\n'
    ;;
  down)
    require_command docker
    acceptance_down
    ;;
  *)
    fail "usage: $0 {up|smoke|verify|down}"
    ;;
esac
