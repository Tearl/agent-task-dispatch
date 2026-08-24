#!/bin/sh
set -eu

load_secret() {
  variable_name="$1"
  secret_path="$2"
  case "${variable_name}" in
    ''|*[!A-Z0-9_]*)
      echo "invalid secret environment variable name" >&2
      exit 1
      ;;
  esac
  if [ ! -r "${secret_path}" ]; then
    echo "required secret file is unavailable" >&2
    exit 1
  fi
  secret_value="$(cat "${secret_path}")"
  if [ -z "${secret_value}" ]; then
    echo "required secret file is empty" >&2
    exit 1
  fi
  export "${variable_name}=${secret_value}"
}

load_secret "${AGENT_API_TOKEN_ENV:?required}" /run/secrets/agent_api_token
load_secret "${AGENT_CALLBACK_KEY_ENV:?required}" /run/secrets/agent_callback_key
load_secret "${PROVIDER_API_KEY_ENV:?required}" /run/secrets/provider_api_key

exec "$@"

