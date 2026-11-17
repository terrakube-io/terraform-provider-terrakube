#!/usr/bin/env bash
# Starts a throwaway Terrakube for the acceptance tests and prints the provider settings.
#
# Usage:
#   scripts/acceptance.sh up     start Terrakube and print TERRAKUBE_VERSION, _ENDPOINT and _TOKEN
#   scripts/acceptance.sh logs   print the API logs
#   scripts/acceptance.sh down   stop Terrakube and delete its data
#   scripts/acceptance.sh versions pinned|latest [previous]
#                                print the Terrakube versions to test as a JSON list (needs jq, gh)
#
# Run the tests locally:
#   set -a; eval "$(scripts/acceptance.sh up)"; set +a
#   make testacc TESTARGS='-run TestAcc'
#   scripts/acceptance.sh down
#
# TERRAKUBE_VERSION picks the image tag (default from versions.json), TERRAKUBE_ACC_PORT the host port
# (default 18080). Needs docker with compose (plugin or standalone), curl, openssl and uuidgen.
set -euo pipefail

cd "$(dirname "$0")/.." || exit 1

compose_file=scripts/acceptance/compose.yaml
port=${TERRAKUBE_ACC_PORT:-18080}
# The Terrakube image tag. Tests that need a newer server read it to skip themselves. The default
# is the version pull requests are tested against, from scripts/acceptance/versions.json.
default_version=$(sed -n 's/.*"terrakube": *"\([^"]*\)".*/\1/p' scripts/acceptance/versions.json)
export TERRAKUBE_VERSION=${TERRAKUBE_VERSION:-$default_version}

compose() {
  # The compose plugin (docker compose) or the standalone binary (docker-compose, e.g. with Colima).
  local compose_cmd
  if docker compose version >/dev/null 2>&1; then
    compose_cmd=(docker compose)
  elif command -v docker-compose >/dev/null 2>&1; then
    compose_cmd=(docker-compose)
  else
    log "docker compose is required"
    exit 1
  fi
  # compose.yaml requires the secrets; logs and down don't use them.
  TERRAKUBE_PAT_SECRET=${TERRAKUBE_PAT_SECRET:-unused} \
    TERRAKUBE_INTERNAL_SECRET=${TERRAKUBE_INTERNAL_SECRET:-unused} \
    "${compose_cmd[@]}" -f "$compose_file" "$@"
}

# Prints the Terrakube versions to test, as a JSON list.
#   versions pinned          the version in scripts/acceptance/versions.json
#   versions latest          the latest Terrakube release
#   versions latest previous the latest release and the latest release of the previous minor
versions() {
  case "${1:-}" in
    pinned)
      jq -c '[.terrakube]' scripts/acceptance/versions.json
      ;;
    latest)
      gh api 'repos/terrakube-io/terrakube/releases?per_page=50' | jq -c --arg with "${2:-}" '
        [.[] | select(.draft or .prerelease | not) | .tag_name | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$"))]
        | sort_by(split(".") | map(tonumber)) | reverse
        | .[0] as $latest
        | ($latest | split(".")[:2]) as $minor
        | [$latest] + if $with == "previous" then [first(.[] | select(split(".")[:2] != $minor))] else [] end'
      ;;
    *)
      log "usage: scripts/acceptance.sh versions pinned|latest [previous]"
      exit 2
      ;;
  esac
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

  log "starting Terrakube $TERRAKUBE_VERSION on port $port"
  compose up -d --quiet-pull >&2

  local endpoint="http://127.0.0.1:$port"
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
  # A port forwarder (Colima, for one) can answer the health check before the API has created
  # its tables, so retry until the pat table exists.
  local jti inserted=false
  jti=$(uuidgen | tr '[:upper:]' '[:lower:]')
  for _ in $(seq 1 30); do
    if compose exec -T postgres psql -q -U terrakube -d terrakube -c \
      "insert into pat (id, days, description, created_date, created_by, deleted) values ('$jti', 1, 'acceptance tests', now(), 'acceptance-tests@example.com', false);" >/dev/null 2>&1; then
      inserted=true
      break
    fi
    sleep 2
  done
  if [ "$inserted" != true ]; then
    log "Could not create the API token: the pat table does not exist"
    compose logs api >&2
    exit 1
  fi

  local now header claims signing_input signature hexkey
  now=$(date +%s)
  header=$(printf '{"alg":"HS256"}' | base64url)
  claims=$(printf '{"iss":"Terrakube","sub":"acceptance-tests (Token)","aud":["Terrakube"],"jti":"%s","email":"acceptance-tests@example.com","email_verified":true,"name":"acceptance-tests (Token)","groups":["TERRAKUBE_ADMIN"],"iat":%d,"exp":%d}' \
    "$jti" "$now" "$((now + 6 * 3600))" | base64url)
  signing_input="$header.$claims"
  hexkey=$(od -An -tx1 "$key_file" | tr -d ' \n')
  signature=$(printf '%s' "$signing_input" | openssl dgst -sha256 -mac HMAC -macopt "hexkey:$hexkey" -binary | base64url)

  # Wait until the API accepts the token: that is the readiness the tests need, and a port
  # forwarder can make the health check pass early.
  local token="$signing_input.$signature" accepted=false
  for _ in $(seq 1 30); do
    if [ "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token" "$endpoint/api/v1/organization")" = 200 ]; then
      accepted=true
      break
    fi
    sleep 2
  done
  if [ "$accepted" != true ]; then
    log "Terrakube did not accept the generated token"
    compose logs api >&2
    exit 1
  fi

  log "Terrakube is ready"
  echo "TERRAKUBE_VERSION=$TERRAKUBE_VERSION"
  echo "TERRAKUBE_ENDPOINT=$endpoint"
  echo "TERRAKUBE_TOKEN=$token"
}

case "${1:-}" in
  up) up ;;
  logs) compose logs api ;;
  down) compose down -v >&2 ;;
  versions) versions "${@:2}" ;;
  *)
    log "usage: scripts/acceptance.sh up|logs|down|versions"
    exit 2
    ;;
esac
