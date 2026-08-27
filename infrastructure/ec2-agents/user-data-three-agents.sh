#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y ca-certificates curl gnupg jq unzip

rm -rf /tmp/aws /tmp/awscliv2.zip
curl -fsSL https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip -o /tmp/awscliv2.zip
unzip -q /tmp/awscliv2.zip -d /tmp
/tmp/aws/install --update
rm -rf /tmp/aws /tmp/awscliv2.zip

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc

architecture="$(dpkg --print-architecture)"
ubuntu_codename="$(. /etc/os-release && printf '%s' "${UBUNTU_CODENAME:-$VERSION_CODENAME}")"
printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu %s stable\n' \
  "${architecture}" "${ubuntu_codename}" > /etc/apt/sources.list.d/docker.list

apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
systemctl enable --now docker
usermod -aG docker ubuntu

install -d -m 0755 /opt/agent-task-dispatch /etc/agent-platform
aws s3 cp \
  s3://agent-platform-deploy-496251221975-us-east-1/releases/three-agents/source.tgz \
  /tmp/agent-platform-three-agents-source.tgz \
  --region us-east-1 \
  --only-show-errors
tar -xzf /tmp/agent-platform-three-agents-source.tgz -C /opt/agent-task-dispatch
rm -f /tmp/agent-platform-three-agents-source.tgz
chown -R ubuntu:ubuntu /opt/agent-task-dispatch
chmod 0755 /opt/agent-task-dispatch/infrastructure/ec2-agents/deploy-three-agents.sh

printf '%s\n' \
  'AWS_REGION=us-east-1' \
  'AGENT_DEPLOYMENT_SECRET_ID=agent-platform/production/three-agents' \
  > /etc/agent-platform/agents-deployment.env
chmod 0644 /etc/agent-platform/agents-deployment.env

install -m 0644 \
  /opt/agent-task-dispatch/infrastructure/ec2-agents/agent-platform-three-agents.service.example \
  /etc/systemd/system/agent-platform-three-agents.service
systemctl daemon-reload
systemctl enable --now agent-platform-three-agents.service
