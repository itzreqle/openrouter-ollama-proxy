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

orollama_resolved_env_path() {
  printf '%s' "${HOME}/.cache/orollama/resolved.env"
}

# Load proxy-written resolved.env (auto models + context window).
orollama_load_resolved_env() {
  local path
  path="$(orollama_resolved_env_path)"
  [[ -f "$path" ]] || return 0
  # shellcheck disable=SC1090
  set -a
  # shellcheck disable=SC1090
  source "$path"
  set +a
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

  # Prefer live auto-picked models from the running proxy when .env leaves MODEL_* empty.
  orollama_load_resolved_env
  [[ -z "$opus" && -n "${OROLLAMA_MODEL_OPUS:-}" ]] && opus="$OROLLAMA_MODEL_OPUS"
  [[ -z "$sonnet" && -n "${OROLLAMA_MODEL_SONNET:-}" ]] && sonnet="$OROLLAMA_MODEL_SONNET"
  [[ -z "$haiku" && -n "${OROLLAMA_MODEL_HAIKU:-}" ]] && haiku="$OROLLAMA_MODEL_HAIKU"
  [[ -z "$default" && -n "${OROLLAMA_MODEL_DEFAULT:-}" ]] && default="$OROLLAMA_MODEL_DEFAULT"

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

  # Reduce "unknown model → 200k context" warning for gateway IDs.
  export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1
  if [[ -n "${CLAUDE_CODE_MAX_CONTEXT_TOKENS:-}" ]]; then
    export CLAUDE_CODE_MAX_CONTEXT_TOKENS
  fi
}

orollama_color_enabled() {
  [[ -n "${NO_COLOR:-}" ]] && return 1
  [[ "${OROLLAMA_FORCE_COLOR:-}" == "1" ]] && return 0
  [[ -t 2 ]]
}

orollama_paint() {
  local code="$1" text="$2"
  if orollama_color_enabled; then
    printf '\033[%sm%s\033[0m' "$code" "$text"
  else
    printf '%s' "$text"
  fi
}

orollama_print_status() {
  local proxy_url="${1:-http://127.0.0.1:8082}"
  printf '%s %s  %s\n' \
    "$(orollama_paint '1;96' 'orollama:')" \
    "$(orollama_paint '1;92' 'proxy ON')" \
    "$(orollama_paint '0;37' "$proxy_url")" >&2
  printf '  %s  %s\n' "$(orollama_paint '0;36' 'provider')" "$(orollama_paint '0;37' 'OpenRouter + local Ollama')" >&2
  printf '  %s      %s\n' "$(orollama_paint '0;35' 'opus')" "$(orollama_paint '0;37' "${ANTHROPIC_DEFAULT_OPUS_MODEL:-unset}")" >&2
  printf '  %s    %s\n' "$(orollama_paint '0;35' 'sonnet')" "$(orollama_paint '0;37' "${ANTHROPIC_DEFAULT_SONNET_MODEL:-unset}")" >&2
  printf '  %s     %s\n' "$(orollama_paint '0;35' 'haiku')" "$(orollama_paint '0;37' "${ANTHROPIC_DEFAULT_HAIKU_MODEL:-unset}")" >&2
  if [[ -n "${CLAUDE_CODE_MAX_CONTEXT_TOKENS:-}" ]]; then
    printf '  %s  %s\n' "$(orollama_paint '0;36' 'context')" "$(orollama_paint '0;37' "${CLAUDE_CODE_MAX_CONTEXT_TOKENS}")" >&2
  fi
  printf '  %s\n' "$(orollama_paint '1;93' 'if auth warning: run  claude /logout  then relaunch')" >&2
}

orollama_print_off() {
  printf '%s %s — launching Claude Code without local gateway\n' \
    "$(orollama_paint '1;96' 'orollama:')" \
    "$(orollama_paint '1;93' 'proxy OFF')" >&2
}
