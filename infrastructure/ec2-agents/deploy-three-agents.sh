#!/usr/bin/env bash
set -euo pipefail

deployment_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
compose_file="${deployment_dir}/compose.three-agents.yaml"
runtime_dir="/dev/shm/agent-platform-three-agents-$(id -u)"
runtime_env="${deployment_dir}/.runtime.three-agents.env"
deployment_json="${runtime_dir}/deployment.json"

: "${AWS_REGION:?AWS_REGION is required}"
: "${AGENT_DEPLOYMENT_SECRET_ID:?AGENT_DEPLOYMENT_SECRET_ID is required}"

umask 077
install -d -m 0700 "${runtime_dir}"
cleanup() {
  rm -f "${deployment_json}"
}
trap cleanup EXIT

aws secretsmanager get-secret-value \
  --region "${AWS_REGION}" \
  --secret-id "${AGENT_DEPLOYMENT_SECRET_ID}" \
  --query SecretString \
  --output text > "${deployment_json}"

jq -er '
      def required: [
        "CLOUDFLARE_TUNNEL_CREDENTIALS_BASE64",
        "IMAGE_AGENT_PUBLIC_BASE_URL",
        "IMAGE_AGENT_API_TOKEN",
        "IMAGE_AGENT_CALLBACK_KEY_BASE64",
        "QWEN_UI_AGENT_PUBLIC_BASE_URL",
        "QWEN_UI_AGENT_API_TOKEN",
        "QWEN_UI_AGENT_CALLBACK_KEY_BASE64",
        "QWEN_UI_DASHSCOPE_API_KEY",
        "DASHSCOPE_IMAGE_BASE_URL",
        "SEEDREAM_AGENT_PUBLIC_BASE_URL",
        "SEEDREAM_AGENT_API_TOKEN",
        "SEEDREAM_AGENT_CALLBACK_KEY_BASE64",
        "ARK_API_KEY",
        "ZAI_API_KEY"
      ];
      . as $document |
      if (type != "object") or any(required[]; . as $key | ($document | has($key) | not)) then
        error("deployment secret is missing required keys")
      elif any(required[]; . as $key | ($document[$key] | type != "string" or length == 0 or test("[\\r\\n]"))) then
        error("deployment secret values must be non-empty single-line strings")
      else
        true
      end
    ' "${deployment_json}" > /dev/null

secret_names=(
  IMAGE_AGENT_API_TOKEN
  IMAGE_AGENT_CALLBACK_KEY_BASE64
  QWEN_UI_AGENT_API_TOKEN
  QWEN_UI_AGENT_CALLBACK_KEY_BASE64
  QWEN_UI_DASHSCOPE_API_KEY
  SEEDREAM_AGENT_API_TOKEN
  SEEDREAM_AGENT_CALLBACK_KEY_BASE64
  ARK_API_KEY
  ZAI_API_KEY
)

jq -er '.CLOUDFLARE_TUNNEL_CREDENTIALS_BASE64' "${deployment_json}" \
  | base64 --decode > "${runtime_dir}/CLOUDFLARE_TUNNEL_CREDENTIALS_JSON"
chmod 0600 "${runtime_dir}/CLOUDFLARE_TUNNEL_CREDENTIALS_JSON"
jq -e '
  .TunnelID == "285e2b7d-2e9f-4bf3-b47d-1645cd7e7404" and
  (.AccountTag | type == "string" and length > 0) and
  (.TunnelSecret | type == "string" and length > 0)
' "${runtime_dir}/CLOUDFLARE_TUNNEL_CREDENTIALS_JSON" > /dev/null

for secret_name in "${secret_names[@]}"; do
  jq -er --arg key "${secret_name}" '.[$key]' "${deployment_json}" > "${runtime_dir}/${secret_name}"
  chmod 0600 "${runtime_dir}/${secret_name}"
done

for callback_name in IMAGE_AGENT_CALLBACK_KEY_BASE64 QWEN_UI_AGENT_CALLBACK_KEY_BASE64 SEEDREAM_AGENT_CALLBACK_KEY_BASE64; do
  decoded_bytes="$(base64 --decode "${runtime_dir}/${callback_name}" | wc -c | tr -d ' ')"
  if [ "${decoded_bytes}" != "32" ]; then
    echo "callback keys must decode to exactly 32 bytes" >&2
    exit 1
  fi
done

image_base_url="$(jq -er '.IMAGE_AGENT_PUBLIC_BASE_URL' "${deployment_json}")"
qwen_ui_base_url="$(jq -er '.QWEN_UI_AGENT_PUBLIC_BASE_URL' "${deployment_json}")"
dashscope_image_base_url="$(jq -er '.DASHSCOPE_IMAGE_BASE_URL' "${deployment_json}")"
seedream_base_url="$(jq -er '.SEEDREAM_AGENT_PUBLIC_BASE_URL' "${deployment_json}")"
for base_url in "${image_base_url}" "${qwen_ui_base_url}" "${seedream_base_url}"; do
  if [[ ! "${base_url}" =~ ^https://[^/]+/?$ ]]; then
    echo "Agent public base URLs must be clean HTTPS origins without paths" >&2
    exit 1
  fi
done
if [[ ! "${dashscope_image_base_url}" =~ ^https://[^/]+/?$ ]]; then
  echo "DASHSCOPE_IMAGE_BASE_URL must be a clean HTTPS origin without paths" >&2
  exit 1
fi

{
  printf 'AGENT_SECRET_DIR=%s\n' "${runtime_dir}"
  printf 'IMAGE_AGENT_PUBLIC_BASE_URL=%s\n' "${image_base_url}"
  printf 'QWEN_UI_AGENT_PUBLIC_BASE_URL=%s\n' "${qwen_ui_base_url}"
  printf 'DASHSCOPE_IMAGE_BASE_URL=%s\n' "${dashscope_image_base_url}"
  printf 'SEEDREAM_AGENT_PUBLIC_BASE_URL=%s\n' "${seedream_base_url}"
} > "${runtime_env}"
chmod 0600 "${runtime_env}"

docker compose --env-file "${runtime_env}" -f "${compose_file}" config --quiet
docker compose --env-file "${runtime_env}" -f "${compose_file}" up -d --build --remove-orphans
docker compose --env-file "${runtime_env}" -f "${compose_file}" ps
