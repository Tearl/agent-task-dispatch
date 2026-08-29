# ADR-0001: Escrow V3 testnet-only chain, asset, identity, and governance

- Status: Accepted
- Date: 2026-08-28
- Decision owners: Agent Platform maintainers
- Scope: `TaskEscrow` V3 and its off-chain ABI consumers

## Context

The platform will run publicly on a test network and will not connect Escrow V3
to a mainnet. Solidity remains the source of truth for escrowed asset state, but
the previous local contract used native value, immutable operational addresses,
and handwritten ABI selectors in TypeScript and Go. Those choices were not a
safe or reproducible public-testnet interface.

## Decision

### Network

- Local development and deterministic acceptance use Anvil chain ID `31337`.
- The only public deployment target is Base Sepolia chain ID `84532`.
- Mainnet chain IDs, RPC endpoints, contract addresses, and credentials are out
  of scope and must not be committed.

### Asset

- Each V3 deployment binds one immutable ERC-20 asset with exactly 6 decimals.
- Base Sepolia uses Circle test USDC. Its address is deployment configuration,
  not a Solidity constant.
- Anvil uses the repository's `MockUSDC`, which has 6 decimals and no value.
- All amounts are unsigned integers in the asset's smallest unit. V3 does not
  accept native value and does not support arbitrary per-task assets.

### Chain task identity

The contract derives, rather than accepts, the authoritative task ID:

```solidity
keccak256(
    abi.encode(
        "agent-platform-task-v3",
        block.chainid,
        address(this),
        address(asset),
        publisher,
        platformTaskKey,
        taskSpecHash,
        fundingDeadline,
        formalBudget
    )
)
```

`platformTaskKey` is `keccak256(bytes(task_id))`. `taskSpecHash` is the frozen
specification digest. `formalBudget` is the exact FormalEscrow amount; overview
and external-cost budgets are not funded by this deposit.

### Privilege model

- `governanceAdmin` is a 2-of-3 Safe address and owns the default admin role.
- `disputeResolver` is a separate 2-of-3 Safe address.
- `platformProofSigner` is a separate test-only operational signer. Its private
  key is injected from a secret manager and is never committed or logged.
- `pauseGuardian` is an independent test operations address. It can pause but
  cannot unpause, rotate roles, resolve disputes, or transfer customer assets.
- Governance can rotate the proof signer and dispute resolver. Every rotation
  emits an event. Governance and guardian can pause; only governance can
  unpause.

Pause blocks new deposits, Agent selection, work-nonce advancement, and new
dispute freezes. It deliberately leaves refunds, acceptance, earnings
withdrawal, and finalization of an already frozen dispute available so an
emergency stop cannot trap exits.

### Upgrade and migration

V3 is not behind a proxy. A semantic or ABI change requires a versioned new
deployment. Engine configuration routes only new tasks to the new address; old
deployments remain observable and retain their exit paths until their test
positions are closed. There is no administrator sweep or in-place storage
migration.

### ABI ownership

The ABI emitted by Foundry from `TaskEscrow.sol` is the only ABI source.
Repository generation produces the TypeScript and Go bindings. Application
code may refer to generated functions, method IDs, and event IDs but must not
embed selectors or canonical event-signature strings. A drift check regenerates
both outputs and fails if the worktree differs.

## Consequences

- Existing native-asset deployments and chain task IDs are incompatible with
  V3 and are not migrated.
- Publishers approve test USDC before `createTask`; the contract pulls the exact
  formal budget with `transferFrom`.
- Test wallets still need test ETH for gas.
- Safe ownership is an operational deployment prerequisite, not a Solidity
  assertion; deployment configuration and runbooks must record the approved
  Safe addresses.
- ABI changes require regenerated bindings in the same change.

## Rejected alternatives

- Native ETH: volatile unit semantics do not model stable task pricing.
- Multi-asset escrow: increases accounting and token-risk surface without a
  testnet requirement.
- UUPS or transparent proxy: storage-layout and upgrade-authority risk outweigh
  the benefit for disposable testnet deployments.
- Immutable proof signer/resolver: prevents recovery from test key loss and does
  not exercise governance rotation.
