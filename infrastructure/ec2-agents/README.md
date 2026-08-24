# Agent deployment on EC2 with Cloudflare Tunnel

This deployment runs the three visual Agents as long-lived containers on one
Ubuntu EC2 instance. The Agents are not assigned public ports. A remotely
managed Cloudflare Tunnel publishes one hostname per container over the shared
Docker network.

## URL contract

The platform stores a clean protocol base URL with no path:

- `https://image-agent.example.com`
- `https://glm-code-agent.example.com`
- `https://qwen-code-agent.example.com`

Engine appends `/health` for protocol health checks and appends
`/v1/executions*` for execution calls. `*_PUBLIC_BASE_URL` must use the same
base URL so artifact references remain pinned to the registered Agent origin.

## 1. Create the EC2 host

Use an Ubuntu LTS x86-64 instance in the same region as Engine. A 2 vCPU / 4 GiB
instance with a 30 GiB encrypted gp3 volume is a practical starting point for
building and running these API-backed Agents; resize after observing CPU,
memory, disk and provider rate limits.

Attach an instance role that permits only `secretsmanager:GetSecretValue` for
the deployment secret. Prefer AWS Systems Manager Session Manager for operator
access. Cloudflare Tunnel is outbound-only, so the security group does not need
public Agent ports or inbound HTTP(S). Allow the outbound HTTPS/provider and
Cloudflare Tunnel traffic required by the services.

Install Git, Docker Engine with the Compose plugin, AWS CLI v2 and `jq` using
their official Ubuntu installation instructions. Add the deployment user to the
`docker` group, then reconnect before continuing.

## 2. Create the Cloudflare Tunnel

In Cloudflare Zero Trust, create one remotely managed Tunnel and add three
published application routes:

| Public hostname | Tunnel service |
| --- | --- |
| `image-agent.example.com` | `http://image-generator:8092` |
| `glm-code-agent.example.com` | `http://glm-image-to-code:8093` |
| `qwen-code-agent.example.com` | `http://qwen-image-to-code:8094` |

Do not enable Cloudflare Access authentication on these routes: Engine uses the
Agent Bearer credential and cannot complete an interactive Access login. Keep
Cloudflare caching disabled for Agent protocol and artifact paths. Copy the
Tunnel token for the next step; never commit or paste it into Compose YAML.

## 3. Store deployment secrets

Create three unique API tokens and three independent 32-byte callback keys:

```bash
openssl rand -hex 32
openssl rand -base64 32
```

Use [secrets.example.json](./secrets.example.json) as the schema for one AWS
Secrets Manager JSON secret. Create or edit the secret through a protected
console/session so secret values do not enter shell history. The three
`*_PUBLIC_BASE_URL` values must be the exact Cloudflare origins above, without
`/health` or a trailing path.

The callback values must decode to exactly 32 bytes because Engine enforces that
size. API tokens must be unique per Agent and contain at least 24 characters.

## 4. Deploy

Clone this repository onto the EC2 host and run:

```bash
cd /opt/agent-task-dispatch
chmod +x infrastructure/ec2-agents/deploy.sh
export AWS_REGION=ap-southeast-1
export AGENT_DEPLOYMENT_SECRET_ID=agent-platform/production/agents
infrastructure/ec2-agents/deploy.sh
```

`deploy.sh` retrieves the JSON secret with the EC2 instance role and splits the
sensitive values into mode-`0600` files in `/dev/shm`. Compose mounts those
files as secrets; API keys and Tunnel tokens are not stored in container
environment metadata. The script validates Compose, builds the Agent image and
starts the services. It retains only the secret files required for container
restarts in memory-backed `/dev/shm`; the downloaded JSON is removed. Each Agent
has an independent named volume for `/var/lib/agent`. Containers are read-only
apart from that volume and `/tmp`.

Install the supplied systemd template so a host reboot repopulates the tmpfs
secrets before reconciling the containers:

```bash
sudo install -d -m 0755 /etc/agent-platform
sudo install -m 0644 infrastructure/ec2-agents/deployment.env.example /etc/agent-platform/agents-deployment.env
sudo install -m 0644 infrastructure/ec2-agents/agent-platform-agents.service.example /etc/systemd/system/agent-platform-agents.service
sudo systemctl daemon-reload
sudo systemctl enable --now agent-platform-agents.service
```

Edit `/etc/agent-platform/agents-deployment.env` first if the region or secret
ID differs. The file contains identifiers only, not credentials.

Inspect status without printing container environments:

```bash
docker compose --env-file infrastructure/ec2-agents/.runtime.env -f infrastructure/ec2-agents/compose.yaml ps
docker compose --env-file infrastructure/ec2-agents/.runtime.env -f infrastructure/ec2-agents/compose.yaml logs --tail=100
```

## 5. Verify and onboard

Wait until the Tunnel reports healthy, then verify from outside the EC2 host:

```bash
curl --fail --show-error https://image-agent.example.com/health
curl --fail --show-error https://glm-code-agent.example.com/health
curl --fail --show-error https://qwen-code-agent.example.com/health
```

Each response must include `"status":"healthy"` and
`"protocolVersion":"1"`, without a redirect. In the platform onboarding form,
enter the base URL without `/health`, choose Bearer Token, use the matching
`*_API_TOKEN`, and set maximum concurrency to `1` initially.

After onboarding assigns the three Agent IDs, configure Engine's server-only
`ENGINE_AGENT_RUNTIME_CREDENTIALS_JSON` secret with the same per-Agent Bearer
tokens, callback keys and callback key versions:

```json
{
  "agent-id-from-engine": {
    "bearerToken": "same Agent API token",
    "callbackKeyBase64": "same 32-byte callback key",
    "callbackKeyVersion": "image-agent-callback-v1"
  }
}
```

Use `glm-image-to-code-callback-v1` and `qwen-image-to-code-callback-v1` for the
other two Agents. Do not store this JSON in PostgreSQL or commit it.

## Operational limits

The current execution records live in process memory while artifacts live on
the named volumes. Keep one replica of each Agent and drain work before
restarting or deploying. Before horizontal scaling, move execution state to a
durable coordination store and artifacts to object storage.
