# Infrastructure

The local environment uses Docker Compose for PostgreSQL, Redis, and LocalStack. LocalStack provisions separate task and Agent SNS topics, raw-delivery projection queues, formal/admin FIFO command queues, queue policies, and a DLQ for every queue from `localstack/init-aws.sh`.

Production infrastructure will live in AWS and remain behind Cloudflare. The deployment implementation is intentionally not guessed in this scaffold because these decisions are still open:

- ECS or EKS for Go Engine and workers
- concrete RDS and ElastiCache shapes
- Lambda adapter and deployment pipeline for Next.js
- Ethereum testnet, RPC providers, and escrow deployment account
- Milvus or Pinecone when V1 semantic matching starts

All production resources must be created through IaC and use private networking and least-privilege service identities.

The current single-host deployment path for the three visual Agents is
documented in [`ec2-agents/README.md`](./ec2-agents/README.md). It uses Docker
Compose, persistent named volumes, AWS Secrets Manager, and an outbound-only
Cloudflare Tunnel; it is an MVP deployment rather than the final ECS/EKS IaC.
