#!/bin/sh
set -eu

REPOSITORY_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
RPC_URL="${EVM_RPC_URL:-http://127.0.0.1:8545}"
PUBLISHER="${LOCAL_NONCE_PUBLISHER_ADDRESS:-0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc}"
AGENT_CONTROLLER="${LOCAL_NONCE_AGENT_CONTROLLER_ADDRESS:-0x90f79bf6eb2c4f870365e785982e1f101e93b906}"
PAYOUT="${LOCAL_NONCE_PAYOUT_ADDRESS:-0x15d34aaf54267db7d7c367839aaf71a00a2c6a65}"
AMOUNT="${LOCAL_NONCE_FORMAL_BUDGET:-1000000}"

env_value() {
  sed -n "s/^$1=//p" "$REPOSITORY_ROOT/.env.chain" | tail -n 1
}

CONTRACT="$(env_value ESCROW_CONTRACT_ADDRESS)"
ASSET="$(env_value ESCROW_ASSET_ADDRESS)"
GOVERNANCE="$(env_value GOVERNANCE_ADMIN_ADDRESS)"
[ -n "$CONTRACT" ] && [ -n "$ASSET" ] || { printf '%s\n' 'Deploy local Escrow first with pnpm acceptance:up.' >&2; exit 1; }
[ -n "$GOVERNANCE" ] || { printf '%s\n' 'GOVERNANCE_ADMIN_ADDRESS is missing from .env.chain.' >&2; exit 1; }

PREVIOUS_PROOF_SIGNER="$(cast call "$CONTRACT" 'platformProofSigner()(address)' --rpc-url "$RPC_URL")"
SIGNER_ROTATED=0
restore_proof_signer() {
  status=$?
  trap - 0 HUP INT TERM
  if [ "$SIGNER_ROTATED" = "1" ]; then
    if ! cast send "$CONTRACT" 'setPlatformProofSigner(address)' "$PREVIOUS_PROOF_SIGNER" \
      --rpc-url "$RPC_URL" --unlocked --from "$GOVERNANCE" >/dev/null; then
      printf '%s\n' 'Failed to restore the original platform proof signer.' >&2
      status=1
    fi
  fi
  exit "$status"
}
trap restore_proof_signer 0
trap 'exit 1' HUP INT TERM

# Anvil's eth_sign applies the EIP-191 message prefix, while SelectionProof uses
# a raw digest. Generate a signer only in process memory, rotate to it through
# governance, and clear the private key immediately after producing the proof.
EPHEMERAL_SIGNER_KEY="0x$(openssl rand -hex 32)"
PROOF_SIGNER="$(cast wallet address --private-key "$EPHEMERAL_SIGNER_KEY")"
cast send "$CONTRACT" 'setPlatformProofSigner(address)' "$PROOF_SIGNER" \
  --rpc-url "$RPC_URL" --unlocked --from "$GOVERNANCE" >/dev/null
SIGNER_ROTATED=1

PLATFORM_TASK_KEY="$(cast keccak 'anvil-work-nonce-v3')"
TASK_SPEC_HASH="$(cast keccak 'anvil-work-nonce-spec-v3')"
ASSIGNMENT_ID="$(cast keccak 'anvil-work-nonce-assignment-v3')"
OVERVIEW_ID="$(cast keccak 'anvil-work-nonce-overview-v3')"
ALLOCATION_ID="$(cast keccak 'anvil-work-nonce-allocation-v3')"
QUOTE_HASH="$(cast keccak 'anvil-work-nonce-quote-v3')"
POLICY_HASH="$(cast keccak 'anvil-work-nonce-policy-v3')"
PROOF_NONCE="$(cast keccak "anvil-work-nonce-proof-$(cast block-number --rpc-url "$RPC_URL")")"
DEADLINE="$(( $(date +%s) + 3600 ))"

TASK_ID="$(cast call "$CONTRACT" 'deriveTaskId(address,bytes32,bytes32,uint64,uint256)(bytes32)' "$PUBLISHER" "$PLATFORM_TASK_KEY" "$TASK_SPEC_HASH" "$DEADLINE" "$AMOUNT" --rpc-url "$RPC_URL")"

cast send "$ASSET" 'mint(address,uint256)' "$PUBLISHER" "$AMOUNT" --rpc-url "$RPC_URL" --unlocked --from "$PUBLISHER" >/dev/null
cast send "$ASSET" 'approve(address,uint256)' "$CONTRACT" "$AMOUNT" --rpc-url "$RPC_URL" --unlocked --from "$PUBLISHER" >/dev/null
cast send "$CONTRACT" 'createTask(bytes32,bytes32,uint64,uint256)' "$PLATFORM_TASK_KEY" "$TASK_SPEC_HASH" "$DEADLINE" "$AMOUNT" --rpc-url "$RPC_URL" --unlocked --from "$PUBLISHER" >/dev/null

PROOF="($TASK_ID,$ASSIGNMENT_ID,$AGENT_CONTROLLER,$PAYOUT,$OVERVIEW_ID,$ALLOCATION_ID,$QUOTE_HASH,$TASK_SPEC_HASH,1,1,0,$AMOUNT,0,$POLICY_HASH,$PROOF_NONCE,$DEADLINE)"
DIGEST="$(cast call "$CONTRACT" 'selectionProofDigest((bytes32,bytes32,address,address,bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint256,uint256,uint256,bytes32,bytes32,uint64))(bytes32)' "$PROOF" --rpc-url "$RPC_URL")"
SIGNATURE="$(cast wallet sign --no-hash --private-key "$EPHEMERAL_SIGNER_KEY" "$DIGEST")"
unset EPHEMERAL_SIGNER_KEY
cast send "$CONTRACT" 'selectAgent((bytes32,bytes32,address,address,bytes32,bytes32,bytes32,bytes32,uint64,uint64,uint256,uint256,uint256,bytes32,bytes32,uint64),bytes)' "$PROOF" "$SIGNATURE" --rpc-url "$RPC_URL" --unlocked --from "$PUBLISHER" >/dev/null

CURRENT="$(cast call "$CONTRACT" 'workNonces(bytes32)(uint256)' "$TASK_ID" --rpc-url "$RPC_URL")"
[ "$CURRENT" = "1" ] || { printf 'Expected work nonce 1, got %s\n' "$CURRENT" >&2; exit 1; }
cast send "$CONTRACT" 'advanceWorkNonce(bytes32,uint256)' "$TASK_ID" "$CURRENT" --rpc-url "$RPC_URL" --unlocked --from "$PUBLISHER" >/dev/null
NEXT="$(cast call "$CONTRACT" 'workNonces(bytes32)(uint256)' "$TASK_ID" --rpc-url "$RPC_URL")"
[ "$NEXT" = "2" ] || { printf 'Expected work nonce 2, got %s\n' "$NEXT" >&2; exit 1; }

printf 'Anvil work nonce advanced on-chain from %s to %s for task %s.\n' "$CURRENT" "$NEXT" "$TASK_ID"
