module github.com/yongjohnlee80/golib/dao/bigquery

go 1.25.3

require (
	cloud.google.com/go v0.123.0
	cloud.google.com/go/bigquery v1.77.0
	github.com/yongjohnlee80/golib v0.5.12
	google.golang.org/api v0.280.0
)

require (
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.7.0 // indirect
	github.com/apache/arrow/go/v15 v15.0.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/google/flatbuffers v23.5.26+incompatible // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.15 // indirect
	github.com/googleapis/gax-go/v2 v2.22.0 // indirect
	github.com/klauspost/compress v1.16.7 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/pierrec/lz4/v4 v4.1.18 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/exp v0.0.0-20240719175910-8a7402abbf56 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/telemetry v0.0.0-20260708182218-49f421fb7959 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260511170946-3700d4141b60 // indirect
	google.golang.org/grpc v1.81.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// Build against a RELEASED golib, with no replace directive, so this module
// compiles from exactly what a third party gets.
//
// There WAS a replace pointing at ../.. here, and the comment that justified it
// is what this replaced: it argued that compiling against the tree catches a
// dao change that breaks this driver in the pull request that caused it, rather
// than after a release. That argument is still true, and it is the cost of the
// current setup rather than a reason to undo it — an incompatible golib change
// now surfaces at the next version bump instead of immediately.
//
// The reason it changed: this module needs golib/errs, and no tag contained that
// package, so the replace was the only thing making it build at all. v0.5.12 is
// the first release carrying errs.
//
// WHAT NEITHER SETUP FIXES: this is a nested module, so the repo-root
// `go build ./...` never compiles it, and its integration tests sit behind a
// build tag, so the module's own default vet skips those too. Only
// `go vet -mod=readonly -tags integration ./...` run FROM this directory sees
// them, and nothing in CI does that yet.
