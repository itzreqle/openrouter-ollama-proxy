# shellcheck shell=bash
# Shared helpers for loading orollama .env and detecting the proxy.
# Sourced by claude-orollama and the interactive shell hook.

orollama_resolve_root() {
  local _source="$1"
  while [[ -L "$_source" ]]; do
    local _dir
    _dir="$(cd -P "$(dirname "$_source")" && pwd)"
    _source="$(readlink "$_source")"
    [[ "$_source" != /* ]] && _source="$_dir/$_source"
  done
  cd -P "$(dirname "$_source")/.." && pwd
}

orollama_env_get() {
  local file="$1" key="$2" line val
  line="$(grep -E "^${key}=" "$file" 2>/dev/null | tail -n1 || true)"
  [[ -n "$line" ]] || { echo ""; return 0; }
  val="${line#*=}"
  val="${val%\"}"
  val="${val#\"}"
  val="${val%\'}"
  val="${val#\'}"
  printf '%s' "$val"
}

orollama_proxy_up() {
  local url="${1:-http://127.0.0.1:8082}"
  curl -fsS --max-time 1 "${url%/}/" >/dev/null 2>&1
}

# Export Anthropic/Claude Code vars from an orollama .env file.
# Usage: orollama_export_claude_env /path/to/.env [proxy_url]
orollama_export_claude_env() {
  local env_file="$1"
  local proxy_url="${2:-http://127.0.0.1:8082}"
  local token opus sonnet haiku default

  token="$(orollama_env_get "$env_file" PROXY_AUTH_TOKEN)"
  opus="$(orollama_env_get "$env_file" MODEL_OPUS)"
  sonnet="$(orollama_env_get "$env_file" MODEL_SONNET)"
  haiku="$(orollama_env_get "$env_file" MODEL_HAIKU)"
  default="$(orollama_env_get "$env_file" MODEL)"

  export ANTHROPIC_BASE_URL="$proxy_url"
  # Explicit empty string — required so Claude Code does not fall back to Anthropic login.
  export ANTHROPIC_API_KEY=""

  if [[ -n "$token" ]]; then
    export ANTHROPIC_AUTH_TOKEN="$token"
  else
    export ANTHROPIC_AUTH_TOKEN="orollama"
  fi

  [[ -n "$opus" ]] && export ANTHROPIC_DEFAULT_OPUS_MODEL="$opus"
  [[ -n "$sonnet" ]] && export ANTHROPIC_DEFAULT_SONNET_MODEL="$sonnet"
  [[ -n "$haiku" ]] && export ANTHROPIC_DEFAULT_HAIKU_MODEL="$haiku"

  if [[ -n "$sonnet" ]]; then
    export ANTHROPIC_MODEL="$sonnet"
  elif [[ -n "$default" ]]; then
    export ANTHROPIC_MODEL="$default"
  fi
}

orollama_print_status() {
  local proxy_url="${1:-http://127.0.0.1:8082}"
  printf 'orollama: proxy ON  %s\n' "$proxy_url" >&2
  printf '  provider  OpenRouter + local Ollama\n' >&2
  printf '  opus      %s\n' "${ANTHROPIC_DEFAULT_OPUS_MODEL:-unset}" >&2
  printf '  sonnet    %s\n' "${ANTHROPIC_DEFAULT_SONNET_MODEL:-unset}" >&2
  printf '  haiku     %s\n' "${ANTHROPIC_DEFAULT_HAIKU_MODEL:-unset}" >&2
}
