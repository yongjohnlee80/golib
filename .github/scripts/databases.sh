#!/usr/bin/env bash
# The dao suites' databases: PostgreSQL 18 with pgvector and MySQL 8.4 in containers, up and
# answering, with TEST_PGURL and TEST_MYSQL_DSN exported for them.
#
# Sourced, databases_up starts them under a name prefix and the caller removes them
# (databases_down), as integration.sh does. Run as a script, it starts them for the rest of a CI
# job: the URLs go to $GITHUB_ENV, and the runner's end removes the containers (the coverage job).
set -euo pipefail

db_prefix="${GOLIB_DB_PREFIX:-golib-it-$$}"
db_pg="$db_prefix-pg" db_my="$db_prefix-mysql"
db_pg_port="${GOLIB_IT_PG_PORT:-55432}" db_my_port="${GOLIB_IT_MYSQL_PORT:-53306}"

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

databases_up() {
  # max_prepared_transactions > 0 for the two-phase commit tests; postgres defaults it to 0.
  docker run -d --name "$db_pg" -e POSTGRES_PASSWORD=golib -e POSTGRES_DB=golib \
    -p "127.0.0.1:$db_pg_port:5432" pgvector/pgvector:pg18 \
    postgres -c max_prepared_transactions=10 >/dev/null
  docker run -d --name "$db_my" -e MYSQL_ROOT_PASSWORD=golib -e MYSQL_DATABASE=golib \
    -p "127.0.0.1:$db_my_port:3306" mysql:8.4 >/dev/null
  wait_for "$db_pg" docker exec "$db_pg" pg_isready -U postgres -d golib
  wait_for "$db_my" docker exec "$db_my" mysql -uroot -pgolib -e 'SELECT 1' golib
  export TEST_PGURL="postgres://postgres:golib@127.0.0.1:$db_pg_port/golib?sslmode=disable"
  export TEST_MYSQL_DSN="root:golib@tcp(127.0.0.1:$db_my_port)/golib?parseTime=true"
}

databases_down() { docker rm -f "$db_pg" "$db_my" >/dev/null 2>&1 || true; }

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  : "${GITHUB_ENV:?run as a script, databases.sh is for a CI job: source it and call databases_up elsewhere}"
  databases_up
  { echo "TEST_PGURL=$TEST_PGURL"; echo "TEST_MYSQL_DSN=$TEST_MYSQL_DSN"; } >> "$GITHUB_ENV"
fi
