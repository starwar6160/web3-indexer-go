#!/usr/bin/env bash
set -euo pipefail

# force_beast.sh
# Force local demo runtime into EPHEMERAL_ANVIL mode, align START_BLOCK to chain head,
# optionally clear checkpoints, and optionally restart docker compose service.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"
RPC_URL="${RPC_URL:-}"
BUFFER_BLOCKS="${BUFFER_BLOCKS:-5}"
CLEAR_CHECKPOINTS="${CLEAR_CHECKPOINTS:-true}"
RESTART_SERVICE="${RESTART_SERVICE:-false}"
SERVICE_NAME="${SERVICE_NAME:-indexer}"

log() { printf '[force_beast] %s\n' "$*"; }
warn() { printf '[force_beast][WARN] %s\n' "$*"; }
err() { printf '[force_beast][ERROR] %s\n' "$*" >&2; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || { err "missing command: $1"; exit 1; }
}

choose_rpc_url() {
  if [[ -n "$RPC_URL" ]]; then
    echo "$RPC_URL"
    return
  fi

  if [[ -f "$ENV_FILE" ]]; then
    local from_env
    from_env=$(grep -E '^RPC_URL=' "$ENV_FILE" | tail -n1 | cut -d'=' -f2- || true)
    if [[ -n "$from_env" ]]; then
      echo "${from_env//\"/}"
      return
    fi

    local from_envs
    from_envs=$(grep -E '^RPC_URLS=' "$ENV_FILE" | tail -n1 | cut -d'=' -f2- || true)
    if [[ -n "$from_envs" ]]; then
      echo "${from_envs%%,*}" | xargs
      return
    fi
  fi

  echo "http://127.0.0.1:8545"
}

rpc_call() {
  local url="$1"
  local method="$2"
  local params="${3:-[]}"
  curl -sS -H 'Content-Type: application/json' \
    -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"${method}\",\"params\":${params}}" \
    "$url"
}

hex_to_dec() {
  local hex="$1"
  python3 - <<PY
value = "${hex}"
print(int(value, 16))
PY
}

upsert_env_kv() {
  local key="$1"
  local value="$2"
  if [[ ! -f "$ENV_FILE" ]]; then
    touch "$ENV_FILE"
  fi
  if grep -qE "^${key}=" "$ENV_FILE"; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
}

clear_checkpoints() {
  local db_url="${DATABASE_URL:-}"
  if [[ -z "$db_url" && -f "$ENV_FILE" ]]; then
    db_url=$(grep -E '^DATABASE_URL=' "$ENV_FILE" | tail -n1 | cut -d'=' -f2- || true)
    db_url="${db_url//\"/}"
  fi

  if [[ -z "$db_url" ]]; then
    warn "DATABASE_URL not found, skip clearing checkpoints"
    return
  fi

  if ! command -v psql >/dev/null 2>&1; then
    warn "psql not installed, skip clearing checkpoints"
    return
  fi

  log "clearing sync_checkpoints and sync_status"
  psql "$db_url" -v ON_ERROR_STOP=1 -c "DELETE FROM sync_checkpoints; DELETE FROM sync_status;" >/dev/null
}

main() {
  require_cmd curl
  require_cmd python3

  local rpc
  rpc=$(choose_rpc_url)
  log "rpc endpoint: $rpc"

  local chain_resp chain_hex chain_id
  chain_resp=$(rpc_call "$rpc" "eth_chainId")
  chain_hex=$(echo "$chain_resp" | sed -n 's/.*"result":"\(0x[0-9a-fA-F]*\)".*/\1/p')
  if [[ -z "$chain_hex" ]]; then
    err "failed to read chainId from RPC: $chain_resp"
    exit 1
  fi
  chain_id=$(hex_to_dec "$chain_hex")
  log "chain_id: $chain_id"

  if [[ "$chain_id" != "31337" ]]; then
    warn "chain_id is not 31337 (Anvil). refusing to force BEAST by default."
    warn "set FORCE_NON_ANVIL=true to bypass"
    if [[ "${FORCE_NON_ANVIL:-false}" != "true" ]]; then
      exit 1
    fi
  fi

  local head_resp head_hex head_dec
  head_resp=$(rpc_call "$rpc" "eth_blockNumber")
  head_hex=$(echo "$head_resp" | sed -n 's/.*"result":"\(0x[0-9a-fA-F]*\)".*/\1/p')
  if [[ -z "$head_hex" ]]; then
    err "failed to read blockNumber from RPC: $head_resp"
    exit 1
  fi
  head_dec=$(hex_to_dec "$head_hex")

  local start_block=$(( head_dec - BUFFER_BLOCKS ))
  if (( start_block < 0 )); then
    start_block=0
  fi

  log "head=$head_dec, start_block=$start_block (buffer=$BUFFER_BLOCKS)"

  upsert_env_kv APP_MODE EPHEMERAL_ANVIL
  upsert_env_kv START_BLOCK "$start_block"

  # Optional hints for runtime
  upsert_env_kv FORCE_RPS true

  log "updated $ENV_FILE: APP_MODE=EPHEMERAL_ANVIL START_BLOCK=$start_block"

  if [[ "$CLEAR_CHECKPOINTS" == "true" ]]; then
    clear_checkpoints
  fi

  if [[ "$RESTART_SERVICE" == "true" ]]; then
    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
      log "restarting docker compose service: $SERVICE_NAME"
      docker compose restart "$SERVICE_NAME"
    else
      warn "docker compose not available; skip restart"
    fi
  else
    log "restart skipped (set RESTART_SERVICE=true to enable)"
  fi

  log "done. next: run Hot-Tune BEAST"
  log "curl -X POST http://localhost:8082/debug/hotune/preset -H 'Content-Type: application/json' -d '{\"mode\":\"BEAST\"}'"
}

main "$@"
