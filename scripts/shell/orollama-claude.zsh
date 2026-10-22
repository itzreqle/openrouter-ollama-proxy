# orollama: plain `claude` uses the local proxy when it is running.
# Installed via: make install-cli  (sourced from ~/.zshrc)
#
#   proxy UP   → OpenRouter + Ollama models from this repo's .env
#   proxy DOWN → normal Claude Code (no gateway env)

_orollama_zsh_init() {
  local _here="${(%):-%x}"
  [[ -z "$_here" || "$_here" == "stdin" ]] && _here="${(%):-%N}"
  if [[ -L "$_here" ]]; then
    _here="$(readlink "$_here")"
  fi
  local _root
  _root="$(cd -P "$(dirname "$_here")/../.." && pwd)"
  # shellcheck disable=SC1091
  source "$_root/scripts/lib/orollama-env.sh"
  typeset -g OROLLAMA_ROOT="$_root"
  typeset -g OROLLAMA_ENV_FILE="${OROLLAMA_ENV:-$_root/.env}"
  typeset -g OROLLAMA_PROXY_URL="${OROLLAMA_URL:-http://127.0.0.1:8082}"
}
_orollama_zsh_init
unfunction _orollama_zsh_init

claude() {
  # Re-source helpers every launch so long-lived shells don't keep a stale
  # orollama_export_claude_env (old openrouter/ pins, missing discovery).
  if [[ -n "${OROLLAMA_ROOT:-}" && -f "${OROLLAMA_ROOT}/scripts/lib/orollama-env.sh" ]]; then
    # shellcheck disable=SC1091
    source "${OROLLAMA_ROOT}/scripts/lib/orollama-env.sh"
  fi
  if orollama_proxy_up "${OROLLAMA_PROXY_URL}"; then
    if [[ ! -f "${OROLLAMA_ENV_FILE}" ]]; then
      echo "orollama: proxy is up but missing ${OROLLAMA_ENV_FILE}" >&2
      return 1
    fi
    orollama_export_claude_env "${OROLLAMA_ENV_FILE}" "${OROLLAMA_PROXY_URL}"
    orollama_print_status "${OROLLAMA_PROXY_URL}"
  else
    orollama_clear_claude_gateway_env
    orollama_print_off
  fi
  # `command` bypasses this function and runs the real Claude Code binary.
  command claude "$@"
}
