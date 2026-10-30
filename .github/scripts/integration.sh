#!/usr/bin/env bash
# The dao integration gate: PostgreSQL 18 with pgvector and MySQL 8.4 in containers, then the dao
# suites with the integration tag, with both servers' URLs set. CI runs it (.github/workflows/go.yml,
# job integration); so can a machine with Docker, to see exactly what CI sees.
#
# A database test that cannot reach its server SKIPS, and a skip looks like every other skip: a
# run with a server missing passes and proves nothing. So a skip naming an absent service, or a
# server without two-phase commit, FAILS this gate. Skips for other reasons are listed, not fatal.
set -euo pipefail

prefix="golib-it-$$"
pg="$prefix-pg" my="$prefix-mysql"
pg_port="${GOLIB_IT_PG_PORT:-55432}" my_port="${GOLIB_IT_MYSQL_PORT:-53306}"
log="${GOLIB_IT_LOG:-integration.log}"

cleanup() { docker rm -f "$pg" "$my" >/dev/null 2>&1 || true; }
trap cleanup EXIT

# max_prepared_transactions > 0 for the two-phase commit tests; postgres defaults it to 0.
docker run -d --name "$pg" -e POSTGRES_PASSWORD=golib -e POSTGRES_DB=golib \
  -p "127.0.0.1:$pg_port:5432" pgvector/pgvector:pg18 \
  postgres -c max_prepared_transactions=10 >/dev/null
docker run -d --name "$my" -e MYSQL_ROOT_PASSWORD=golib -e MYSQL_DATABASE=golib \
  -p "127.0.0.1:$my_port:3306" mysql:8.4 >/dev/null

# wait_for <what> <command...>: until two minutes of wall time have passed (each try's own time
# included), then fail with the server's own log
wait_for() {
  local what=$1; shift
  local deadline=$((SECONDS + 120))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if timeout 10 "$@" >/dev/null 2>&1; then echo "$what is up"; return 0; fi
    sleep 1
  done
  echo "::error::$what did not come up within 120s"
  docker logs "${what}" 2>&1 | tail -30
  return 1
}
wait_for "$pg" docker exec "$pg" pg_isready -U postgres -d golib
wait_for "$my" docker exec "$my" mysql -uroot -pgolib -e 'SELECT 1' golib

export TEST_PGURL="postgres://postgres:golib@127.0.0.1:$pg_port/golib?sslmode=disable"
export TEST_MYSQL_DSN="root:golib@tcp(127.0.0.1:$my_port)/golib?parseTime=true"

set +e
# GOLIB_IT_GO runs go elsewhere (a container with the module's toolchain, on the host network),
# passing TEST_PGURL and TEST_MYSQL_DSN through; CI's go is on the path.
${GOLIB_IT_GO:-go} test -tags integration -race -count=1 -timeout 20m -v ./dao/... > "$log" 2>&1
status=$?
set -e
grep -a -E '^(ok|FAIL|---)[[:space:]]' "$log" | grep -a -v -- '--- PASS' || true

# every skip that names a service this gate provides is a failure of the gate itself
absent=$(grep -a -E 'TEST_PGURL not set|TEST_MYSQL_DSN not set|max_prepared_transactions=' "$log" || true)
if [ -n "$absent" ]; then
  echo "::error::tests skipped for a service this gate provides:"
  echo "$absent"
  status=1
fi
echo "skips for other reasons: $(grep -a -c -- '--- SKIP' "$log" || true) in all, listed above"
exit "$status"
