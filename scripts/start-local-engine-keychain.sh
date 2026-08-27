#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KEYCHAIN_ACCOUNT=${USER:?USER is required}

set -a
. "$ROOT_DIR/.env"
. "$ROOT_DIR/.env.chain"
set +a

keychain_secret() {
  security find-generic-password -w -a "$KEYCHAIN_ACCOUNT" -s "agent-task-dispatch/$1"
}

# Local runtime data intentionally lives separately from the older development
# database. Secrets remain in the macOS login keychain and are never written to
# project files or logs.
export DATABASE_URL=${LOCAL_ENGINE_DATABASE_URL:-postgres://agent:agent@localhost:5432/agent_platform_runtime?sslmode=disable}
export AGENT_CREDENTIAL_KEK_BASE64=$(keychain_secret credential-kek)
export AGENT_CREDENTIAL_IDEMPOTENCY_HMAC_BASE64=$(keychain_secret credential-idempotency)
export SELECTION_PROOF_SIGNING_KEY_HEX=$(keychain_secret selection-proof)
export DELIVERY_PROOF_SIGNING_KEY_HEX=$(keychain_secret delivery-proof)
export MATCHING_SHUFFLE_SECRET_BASE64=$(keychain_secret matching-shuffle)
export EXECUTION_NONCE_SECRET_BASE64=$(keychain_secret execution-nonce)

# Committed protocol bundles are restored from encrypted PostgreSQL records.
# Static JSON credentials are intentionally disabled for this local runtime.
export ENGINE_AGENT_RUNTIME_CREDENTIALS_JSON='{}'

cd "$ROOT_DIR/services/engine"
exec go run ./cmd/api
