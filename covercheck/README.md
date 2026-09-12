# covercheck

`covercheck` compares Go coverage profiles from two source revisions, relates
the head profile to a Git diff, and evaluates explicit coverage policy. The
library is CI-provider neutral and the companion command can be used without
adding a dependency to application code.

## Install

```bash
go get github.com/yongjohnlee80/golib/covercheck
go install github.com/yongjohnlee80/golib/cmd/covercheck@latest
```

## Features

- Standard `set`, `count`, and `atomic` Go coverage profiles.
- Statement-weighted total, package, and changed-file comparisons.
- Changed-block coverage from `git diff --unified=0` head-side ranges.
- Minimum total/package/file/changed coverage thresholds.
- Total and per-file regression budgets.
- Regular-expression exclusions and library-level file overrides.
- Go-parser classification keeps declaration-only files at `N/A` while strict
  mode fails executable changed files missing from the head profile.
- Text, Markdown, and JSON output with stable CI exit codes.
- No network calls, hosted service, token, or third-party Go dependency.

Changed-block coverage is deliberately conservative. If an added or modified
line intersects a Go coverage block, the entire block and its `numStmt` weight
are included once. Go's profile format does not expose each statement's exact
line within a multi-line block.

## Command-line usage

Generate equivalent profiles for the base and head revisions, then provide an
existing diff:

```bash
covercheck \
  --base-profile coverage-base.out \
  --head-profile coverage-head.out \
  --diff pr.diff \
  --minimum-changed 80 \
  --maximum-total-regression 0 \
  --maximum-file-regression 1 \
  --require-changed-files
```

Or let the command perform a read-only diff between explicit revisions:

```bash
covercheck \
  --base-profile coverage-base.out \
  --head-profile coverage-head.out \
  --base-ref "$BASE_SHA" \
  --head-ref "$HEAD_SHA" \
  --format markdown
```

Exit code `0` means the policy passed, `1` means valid inputs failed policy,
and `2` identifies invalid input, configuration, Git discovery, or output.

## Library usage

```go
base, err := covercheck.ParseProfile(baseReader,
	covercheck.WithModulePath("example.com/project"),
)
// Handle err, parse the head profile and unified diff, then:
report, err := covercheck.Analyze(base, head, changes)
violations, err := covercheck.Evaluate(report,
	covercheck.WithMinimumChanged(80),
	covercheck.WithMaximumTotalRegression(0),
	covercheck.WithMaximumFileRegression(1),
	covercheck.WithRequiredChangedFiles(),
)
```

The library never runs tests or Git and never changes a checkout. Consumers
can instead construct `FileChange` values from another forge or source-control
API.

## GitHub Actions outline

Use `actions/checkout` with full history, generate one profile at the explicit
base SHA and one at the explicit comparison SHA with identical flags, then run
the same command shown above. The repository workflow—not this package—decides
whether the comparison revision is the PR head or GitHub's synthetic merge
commit.

```yaml
permissions:
  contents: read

steps:
  - uses: actions/checkout@v4
    with:
      fetch-depth: 0
  - uses: actions/setup-go@v5
    with:
      go-version-file: go.mod
  - run: go test -count=1 -covermode=atomic -coverpkg=./... -coverprofile="$RUNNER_TEMP/head.out" ./...
  - name: collect base coverage
    env:
      BASE_SHA: ${{ github.event.pull_request.base.sha }}
    run: |
      git worktree add --detach "$RUNNER_TEMP/base" "$BASE_SHA"
      cd "$RUNNER_TEMP/base"
      go test -count=1 -covermode=atomic -coverpkg=./... -coverprofile="$RUNNER_TEMP/base.out" ./...
  - run: go install github.com/yongjohnlee80/golib/cmd/covercheck@v0.6.0
  - name: compare coverage
    env:
      BASE_SHA: ${{ github.event.pull_request.base.sha }}
      HEAD_SHA: ${{ github.sha }}
    run: >-
      covercheck --base-profile "$RUNNER_TEMP/base.out"
      --head-profile "$RUNNER_TEMP/head.out"
      --base-ref "$BASE_SHA" --head-ref "$HEAD_SHA"
      --minimum-changed 80 --maximum-total-regression 0
```

Other CI systems use the identical executable and profiles; only checkout and
artifact orchestration changes.

## License

See the repository [LICENSE](../LICENSE).
