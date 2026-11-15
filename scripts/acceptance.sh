#!/usr/bin/env bash
# Starts a throwaway Terrakube for the acceptance tests and prints the provider settings.
#
# Usage:
#   scripts/acceptance.sh up     start Terrakube and print TERRAKUBE_ENDPOINT and TERRAKUBE_TOKEN
#   scripts/acceptance.sh logs   print the API logs
#   scripts/acceptance.sh down   stop Terrakube and delete its data
#
# Run the tests locally:
#   set -a; eval "$(scripts/acceptance.sh up)"; set +a
#   make testacc TESTARGS='-run TestAcc'
#   scripts/acceptance.sh down
#
# TERRAKUBE_VERSION picks the image tag (default in compose.yaml), TERRAKUBE_ACC_PORT the host port
# (default 18080). Needs docker with compose (plugin or standalone), curl, openssl and uuidgen.
set -euo pipefail

cd "$(dirname "$0")/.." || exit 1

compose_file=scripts/acceptance/compose.yaml
port=${TERRAKUBE_ACC_PORT:-18080}

# The compose plugin (docker compose) or the standalone binary (docker-compose, e.g. with Colima).
if docker compose version >/dev/null 2>&1; then
  compose_cmd=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  compose_cmd=(docker-compose)
else
  echo "docker compose is required" >&2
  exit 1
fi

compose() {
  # compose.yaml requires the secrets; logs and down don't use them.
  TERRAKUBE_PAT_SECRET=${TERRAKUBE_PAT_SECRET:-unused} \
    TERRAKUBE_INTERNAL_SECRET=${TERRAKUBE_INTERNAL_SECRET:-unused} \
    "${compose_cmd[@]}" -f "$compose_file" "$@"
}

log() {
  echo "$@" >&2
}

base64url() {
  base64 | tr '+/' '-_' | tr -d '=\n'
}

up() {
  local key_file
  key_file=$(mktemp)
  trap 'rm -f "$key_file"' RETURN

  # 32 random bytes: the API decodes the secret as base64url and signs with HS256.
  openssl rand 32 >"$key_file"
  export TERRAKUBE_PAT_SECRET TERRAKUBE_INTERNAL_SECRET
  TERRAKUBE_PAT_SECRET=$(base64url <"$key_file")
  TERRAKUBE_INTERNAL_SECRET=$(openssl rand 32 | base64url)

  log "starting Terrakube ${TERRAKUBE_VERSION:-(default version)} on port $port"
  compose up -d --quiet-pull >&2

  local endpoint="http://localhost:$port"
  for _ in $(seq 1 60); do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' "$endpoint/actuator/health")" = 200 ]; then
      break
    fi
    sleep 2
  done
  if [ "$(curl -s -o /dev/null -w '%{http_code}' "$endpoint/actuator/health")" != 200 ]; then
    log "Terrakube did not become healthy:"
    compose logs api >&2
    exit 1
  fi

  # Terrakube 2.34 and later only accept a token whose jti has a row in the pat table. Insert
  # only the columns every supported version has.
  local jti
  jti=$(uuidgen | tr '[:upper:]' '[:lower:]')
  compose exec -T postgres psql -q -U terrakube -d terrakube -c \
    "insert into pat (id, days, description, created_date, created_by, deleted) values ('$jti', 1, 'acceptance tests', now(), 'acceptance-tests@example.com', false);" >&2

  local now header claims signing_input signature hexkey
  now=$(date +%s)
  header=$(printf '{"alg":"HS256"}' | base64url)
  claims=$(printf '{"iss":"Terrakube","sub":"acceptance-tests (Token)","aud":["Terrakube"],"jti":"%s","email":"acceptance-tests@example.com","email_verified":true,"name":"acceptance-tests (Token)","groups":["TERRAKUBE_ADMIN"],"iat":%d,"exp":%d}' \
    "$jti" "$now" "$((now + 6 * 3600))" | base64url)
  signing_input="$header.$claims"
  hexkey=$(od -An -tx1 "$key_file" | tr -d ' \n')
  signature=$(printf '%s' "$signing_input" | openssl dgst -sha256 -mac HMAC -macopt "hexkey:$hexkey" -binary | base64url)

  local token="$signing_input.$signature"
  if [ "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token" "$endpoint/api/v1/organization")" != 200 ]; then
    log "Terrakube did not accept the generated token"
    exit 1
  fi

  log "Terrakube is ready"
  echo "TERRAKUBE_ENDPOINT=$endpoint"
  echo "TERRAKUBE_TOKEN=$token"
}

case "${1:-}" in
  up) up ;;
  logs) compose logs api ;;
  down) compose down -v >&2 ;;
  *)
    log "usage: scripts/acceptance.sh up|logs|down"
    exit 2
    ;;
esac
