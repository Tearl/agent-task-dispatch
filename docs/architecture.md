# Architecture Baseline

```text
Cloudflare CDN/WAF
  ├─ React + Vite customer app (apps/web)
  └─ Next.js SSR/BFF on Lambda (apps/bff)
       └─ Go Distribution Engine on ECS or EKS (services/engine)
            ├─ PostgreSQL
            ├─ Redis
            ├─ SNS → SQS → DLQ
            ├─ External Agent APIs
            └─ Ethereum testnet escrow contract (contracts/escrow)
```

## Ownership

- Customer UI owns presentation and wallet interaction.
- BFF owns SSR, SEO, session-facing APIs, and response aggregation.
- Go Engine owns RBAC enforcement, task state, V0 matching, Agent scheduling, and protocol conversion.
- Solidity owns escrow fund state.
- PostgreSQL owns off-chain durable data. Redis data must be reconstructable.

## Roles

- `TASK_PUBLISHER`: owns tasks, escrow funding, Agent selection, acceptance, and eligible refunds.
- `AGENT_PROVIDER`: owns Agent registration, credentials, delivery, and settlement views.
- `ADMIN`: operates a separate non-public back office. It cannot sign customer transactions or read Agent credentials in plaintext.

