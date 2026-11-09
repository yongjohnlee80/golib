#!/usr/bin/env bash
# gui-packages-check.sh <list>: run in gui/. Fails when a gui package that has tests is neither in
# <list> (a line ./pkg/, or . for gui itself) nor named there as not yet passing (a line !./pkg/
# with the reason after #), so a package that gains tests can't be left out of a platform's run
# unseen.
set -euo pipefail
list=$1
tested=$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... | tr -d '\r' |
  sed -e 's#^github.com/yongjohnlee80/golib/gui$#.#' -e 's#^github.com/yongjohnlee80/golib/gui/\(.*\)$#./\1/#' | grep . | sort -u)
named=$(sed -e 's/#.*//' -e 's/^!//' -e 's/[[:space:]]*$//' "$list" | tr -d '\r' | grep . | sort -u)
missing=$(comm -23 <(echo "$tested") <(echo "$named"))
if [ -n "$missing" ]; then
  echo "::error::gui packages with tests that $list neither runs nor names as not yet passing:"
  echo "$missing"
  exit 1
fi
echo "every tested gui package is in $list: $(echo "$tested" | wc -l) packages"
