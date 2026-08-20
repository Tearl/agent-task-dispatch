# Infrastructure

The local environment uses Docker Compose for PostgreSQL, Redis, and LocalStack. LocalStack provisions the development SNS topic, SQS task queue, and DLQ from `localstack/init-aws.sh`.

Production infrastructure will live in AWS and remain behind Cloudflare. The deployment implementation is intentionally not guessed in this scaffold because these decisions are still open:

- ECS or EKS for Go Engine and workers
- concrete RDS and ElastiCache shapes
- Lambda adapter and deployment pipeline for Next.js
- Ethereum testnet, RPC providers, and escrow deployment account
- Milvus or Pinecone when V1 semantic matching starts

All production resources must be created through IaC and use private networking and least-privilege service identities.

