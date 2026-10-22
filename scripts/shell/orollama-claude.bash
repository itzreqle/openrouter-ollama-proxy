# orollama: plain `claude` uses the local proxy when it is running.
# Installed via: make install-cli  (sourced from ~/.bashrc)

_orollama_bash_init() {
  local _here="${BASH_SOURCE[0]}"
  while [[ -L "$_here" ]]; do
    local _dir
    _dir="$(cd -P "$(dirname "$_here")" && pwd)"
    _here="$(readlink "$_here")"
    [[ "$_here" != /* ]] && _here="$_dir/$_here"
  done
  local _root
  _root="$(cd -P "$(dirname "$_here")/../.." && pwd)"
  # shellcheck disable=SC1091
  source "$_root/scripts/lib/orollama-env.sh"
  export OROLLAMA_ROOT="$_root"
  export OROLLAMA_ENV_FILE="${OROLLAMA_ENV:-$_root/.env}"
  export OROLLAMA_PROXY_URL="${OROLLAMA_URL:-http://127.0.0.1:8082}"
}
_orollama_bash_init
unset -f _orollama_bash_init

claude() {
  # Re-source helpers every launch so long-lived shells don't keep a stale
  # orollama_export_claude_env (old openrouter/ pins, missing discovery).
  if [[ -n "${OROLLAMA_ROOT:-}" && -f "${OROLLAMA_ROOT}/scripts/lib/orollama-env.sh" ]]; then
    # shellcheck disable=SC1091
    # shellcheck source=/dev/null
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
  command claude "$@"
}
