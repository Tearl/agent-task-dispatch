#!/bin/sh
set -eu

RPC_URL="${EVM_RPC_URL:-http://127.0.0.1:8545}"
DEPLOYER_ADDRESS="${LOCAL_CHAIN_DEPLOYER_ADDRESS:-0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266}"
RESOLVER_ADDRESS="${DISPUTE_RESOLVER_ADDRESS:-$DEPLOYER_ADDRESS}"
: "${SELECTION_PROOF_SIGNER_ADDRESS:?Set SELECTION_PROOF_SIGNER_ADDRESS to the public address corresponding to the Engine signing key}"

cast block-number --rpc-url "$RPC_URL" >/dev/null
deployment="$(cd contracts/escrow && forge create src/TaskEscrow.sol:TaskEscrow --rpc-url "$RPC_URL" --unlocked --from "$DEPLOYER_ADDRESS" --broadcast --json --constructor-args "$RESOLVER_ADDRESS" "$SELECTION_PROOF_SIGNER_ADDRESS")"
contract="$(printf '%s' "$deployment" | jq -r '.deployedTo' | tr '[:upper:]' '[:lower:]')"
transaction="$(printf '%s' "$deployment" | jq -r '.transactionHash')"
block_hex="$(cast receipt "$transaction" blockNumber --rpc-url "$RPC_URL")"
block="$(cast to-dec "$block_hex")"

umask 077
{
  printf 'EVM_RPC_URL=%s\n' "$RPC_URL"
  printf 'EVM_RPC_ALLOW_PRIVATE_HTTP=true\n'
  printf 'ESCROW_CONTRACT_ADDRESS=%s\n' "$contract"
  printf 'DISPUTE_RESOLVER_ADDRESS=%s\n' "$(printf '%s' "$RESOLVER_ADDRESS" | tr '[:upper:]' '[:lower:]')"
  printf 'ESCROW_DEPLOYMENT_BLOCK=%s\n' "$block"
  printf 'EVM_CONFIRMATIONS=1\n'
  printf 'EVM_MAX_REORG_DEPTH=64\n'
} > .env.chain

printf 'Local TaskEscrow deployed at %s (block %s). Public chain config written to .env.chain.\n' "$contract" "$block"
