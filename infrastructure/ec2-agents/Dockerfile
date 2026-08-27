FROM node:24.16.0-bookworm-slim

ENV PNPM_HOME=/pnpm
ENV PATH=/pnpm:$PATH
ENV CI=true

RUN corepack enable

WORKDIR /app

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml nx.json tsconfig.json ./
COPY agents ./agents
COPY packages ./packages
COPY infrastructure/ec2-agents/agent-entrypoint.sh /usr/local/bin/agent-entrypoint

RUN pnpm install --frozen-lockfile \
    && pnpm exec tsc -p agents/image-generator/tsconfig.build.json \
    && pnpm exec tsc -p agents/qwen-ui-prototype/tsconfig.build.json \
    && pnpm exec tsc -p agents/seedream-visual-design/tsconfig.build.json \
    && pnpm exec tsc -p agents/glm-image-to-code/tsconfig.build.json \
    && pnpm exec tsc -p agents/qwen-image-to-code/tsconfig.build.json \
    && pnpm install --prod --offline --frozen-lockfile \
    && mkdir -p /var/lib/agent \
    && chmod 0555 /usr/local/bin/agent-entrypoint \
    && chown -R node:node /app /var/lib/agent

ENV NODE_ENV=production

USER node

ENTRYPOINT ["/usr/local/bin/agent-entrypoint"]
