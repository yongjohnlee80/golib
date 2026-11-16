#!/usr/bin/env bash
# The gui module's tests on Linux, run from gui/ (the gui job's; any machine with Gio's libraries
# and jq can run it to see what CI sees). Arguments are go test's.
#
# The cells that check pixels draw on a headless GPU, Mesa's EGL without a display
# (EGL_PLATFORM=surfaceless, set here unless the caller set it). Where none can be made, each such
# cell SKIPS, and a skip looks like every other: a run on a machine without one passes and checks no
# pixel. So a skip saying there is no headless GPU or window FAILS this script. Skips for other
# reasons (a font not installed, no Vulkan device) are listed, not fatal.
set -uo pipefail

export EGL_PLATFORM="${EGL_PLATFORM:-surfaceless}"
log="${GOLIB_GUI_LOG:-${RUNNER_TEMP:-/tmp}/gui-tests.json}"

go test -json "$@" ./... > "$log"
status=$?

echo "== packages"
jq -r 'select(.Test == null and (.Action == "pass" or .Action == "fail" or .Action == "skip"))
       | "\(.Action) \(.Package) \(.Elapsed // "")s"' "$log"

echo "== skips, each with how many times and why"
jq -rs 'map(select(.Test != null)) | group_by([.Package, .Test])
        | map(select(any(.Action == "skip"))) | .[]
        | "\([.[] | select(.Action == "skip")] | length)x \(.[0].Package) \(.[0].Test): "
          + ([.[] | select(.Action == "output" and (.Output | test("_test\\.go:[0-9]+: ")))
              | .Output | sub("^\\s+"; "") | rtrimstr("\n")] | last // "")' "$log"

# each failed test or package, with what it said
jq -r 'select(.Action == "fail") | "\(.Package)\t\(.Test // "")"' "$log" | sort -u |
  while IFS=$'\t' read -r pkg test; do
    echo "== FAIL $pkg $test"
    jq -j --arg p "$pkg" --arg t "$test" \
      'select(.Package == $p and (.Test // "") == $t and .Action == "output") | .Output' "$log" |
      tr -d '\r' | head -80
  done

# a package that did not compile: go test -json gives the compiler's words as build-output events,
# not as a test's output
if jq -e -s 'any(.[]; .Action == "build-fail")' "$log" > /dev/null; then
  echo "== build failures"
  jq -j 'select(.Action == "build-output") | .Output' "$log" | tr -d '\r' | head -200
fi

nogpu=$(jq -r 'select(.Action == "output" and (.Output | test("no headless (GPU|window) here")))
               | "\(.Package) \(.Test): \(.Output | sub("^\\s+"; "") | rtrimstr("\n"))"' "$log" |
  sort | uniq -c)
if [ -n "$nogpu" ]; then
  echo "::error::cells skipped for want of a headless GPU (EGL_PLATFORM=$EGL_PLATFORM): their pixels were not checked"
  echo "$nogpu"
  status=1
fi
exit "$status"
