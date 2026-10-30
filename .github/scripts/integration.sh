#!/usr/bin/env bash
# The dao integration gate: PostgreSQL 18 with pgvector and MySQL 8.4 in containers, then the dao
# suites with the integration tag, with both servers' URLs set. CI runs it (.github/workflows/go.yml,
# job integration); so can a machine with Docker, to see exactly what CI sees.
#
# A database test that cannot reach its server SKIPS, and a skip looks like every other skip: a
# run with a server missing passes and proves nothing. So a skip naming an absent service, or a
# server without two-phase commit, FAILS this gate. Skips for other reasons are listed, not fatal.
set -euo pipefail

log="${GOLIB_IT_LOG:-integration.log}"

# the two servers (databases.sh, which the coverage job also starts them with)
. "$(dirname "$0")/databases.sh"
trap databases_down EXIT
databases_up

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
