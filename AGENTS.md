# Agent Platform Engineering Rules

## Architecture boundaries

- `apps/web` owns the React/Vite customer UI. It never accesses databases or AWS queues directly.
- `apps/bff` owns SSR, SEO, session-facing endpoints, and response aggregation. It must not implement task state transitions, matching rules, or fund rules.
- `services/engine` is the source of truth for off-chain domain rules, task state transitions, matching, and Agent scheduling.
- `contracts/escrow` is the source of truth for escrowed fund state.
- PostgreSQL is the off-chain system of record. Redis contains only reconstructable cache and coordination data.
- Browser requests flow through BFF to Engine. Internal Engine endpoints are not public APIs.

## Security rules

- Never store or log wallet private keys, signatures beyond their verification need, Agent API keys, JWTs, or secrets in plaintext.
- Enforce role, resource ownership, domain state, and idempotency in the Engine even when the BFF has already checked them.
- Admin users cannot sign escrow transactions for customer wallets or read Agent credentials in plaintext.
- Every message consumer and chain-event consumer must be idempotent.

## Quality gates

- Keep functions and files focused; split domain, transport, and infrastructure concerns.
- Add tests for state transitions, authorization boundaries, message redelivery, and contract fund paths.
- Run `pnpm check-types`, `pnpm test`, and `pnpm build` before handing off changes.

