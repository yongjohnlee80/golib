// Command covercheck compares Go coverage profiles and enforces CI policy.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/yongjohnlee80/golib/covercheck"
)

const (
	exitPass        = 0
	exitPolicy      = 1
	exitOperational = 2
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type config struct {
	baseProfile    string
	headProfile    string
	diffFile       string
	baseRef        string
	headRef        string
	root           string
	modulePath     string
	format         string
	minimumTotal   float64
	minimumPackage float64
	minimumFile    float64
	minimumChanged float64
	maxTotalDrop   float64
	maxFileDrop    float64
	requireChanged bool
	excludes       stringList
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("covercheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	cfg := config{}
	flags.StringVar(&cfg.baseProfile, "base-profile", "", "base Go coverage profile (required)")
	flags.StringVar(&cfg.headProfile, "head-profile", "", "head Go coverage profile (required)")
	flags.StringVar(&cfg.diffFile, "diff", "", "unified=0 Git diff file; use - for stdin")
	flags.StringVar(&cfg.baseRef, "base-ref", "", "explicit base revision for read-only git diff discovery")
	flags.StringVar(&cfg.headRef, "head-ref", "", "explicit head revision for read-only git diff discovery")
	flags.StringVar(&cfg.root, "root", ".", "module and Git repository root")
	flags.StringVar(&cfg.modulePath, "module", "", "Go module path; defaults to the root go.mod module directive")
	flags.StringVar(&cfg.format, "format", "text", "output format: text, markdown, or json")
	flags.Float64Var(&cfg.minimumTotal, "minimum-total", -1, "minimum total head coverage percentage")
	flags.Float64Var(&cfg.minimumPackage, "minimum-package", -1, "minimum head coverage percentage for every package")
	flags.Float64Var(&cfg.minimumFile, "minimum-file", -1, "minimum head coverage percentage for every changed file")
	flags.Float64Var(&cfg.minimumChanged, "minimum-changed", -1, "minimum coverage percentage across changed blocks")
	flags.Float64Var(&cfg.maxTotalDrop, "maximum-total-regression", -1, "maximum allowed total decrease in percentage points")
	flags.Float64Var(&cfg.maxFileDrop, "maximum-file-regression", -1, "maximum allowed decrease per changed file in percentage points")
	flags.BoolVar(&cfg.requireChanged, "require-changed-files", false, "fail when a changed production Go file is absent from the head profile")
	flags.Var(&cfg.excludes, "exclude", "Go regular expression excluding file-level checks; repeatable")
	if err := flags.Parse(args); err != nil {
		return exitOperational
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "covercheck: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return exitOperational
	}
	if cfg.baseRef != "" || cfg.headRef != "" {
		fmt.Fprintf(stderr, "covercheck: comparing explicit revisions %s..%s\n", cfg.baseRef, cfg.headRef)
	}

	result, err := execute(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "covercheck: %v\n", err)
		return exitOperational
	}
	if err := render(stdout, cfg.format, result); err != nil {
		fmt.Fprintf(stderr, "covercheck: rendering report: %v\n", err)
		return exitOperational
	}
	if !result.Passed {
		return exitPolicy
	}
	return exitPass
}

func execute(ctx context.Context, cfg config) (covercheck.Result, error) {
	if cfg.baseProfile == "" || cfg.headProfile == "" {
		return covercheck.Result{}, errors.New("--base-profile and --head-profile are required")
	}
	root, err := filepath.Abs(cfg.root)
	if err != nil {
		return covercheck.Result{}, fmt.Errorf("resolving root: %w", err)
	}
	modulePath := cfg.modulePath
	if modulePath == "" {
		modulePath, err = readModulePath(filepath.Join(root, "go.mod"))
		if err != nil {
			return covercheck.Result{}, err
		}
	}
	base, err := readProfile(cfg.baseProfile, modulePath, root)
	if err != nil {
		return covercheck.Result{}, fmt.Errorf("base profile: %w", err)
	}
	head, err := readProfile(cfg.headProfile, modulePath, root)
	if err != nil {
		return covercheck.Result{}, fmt.Errorf("head profile: %w", err)
	}
	diff, err := readDiff(ctx, cfg, root)
	if err != nil {
		return covercheck.Result{}, err
	}
	defer diff.Close()
	changes, err := covercheck.ParseUnifiedDiff(diff)
	if err != nil {
		return covercheck.Result{}, err
	}
	changes = productionGoChanges(changes)
	if err := annotateExecutability(root, changes); err != nil {
		return covercheck.Result{}, err
	}
	report, err := covercheck.Analyze(base, head, changes)
	if err != nil {
		return covercheck.Result{}, err
	}
	options, err := policyOptions(cfg)
	if err != nil {
		return covercheck.Result{}, err
	}
	violations, err := covercheck.Evaluate(report, options...)
	if err != nil {
		return covercheck.Result{}, err
	}
	return covercheck.NewResult(report, violations), nil
}

func readProfile(filename, modulePath, root string) (*covercheck.Profile, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return covercheck.ParseProfile(file, covercheck.WithModulePath(modulePath), covercheck.WithModuleRoot(root))
}

func readDiff(ctx context.Context, cfg config, root string) (io.ReadCloser, error) {
	if cfg.diffFile != "" && (cfg.baseRef != "" || cfg.headRef != "") {
		return nil, errors.New("use either --diff or --base-ref with --head-ref, not both")
	}
	if cfg.diffFile == "-" {
		return io.NopCloser(os.Stdin), nil
	}
	if cfg.diffFile != "" {
		file, err := os.Open(cfg.diffFile)
		if err != nil {
			return nil, fmt.Errorf("opening diff: %w", err)
		}
		return file, nil
	}
	if cfg.baseRef == "" || cfg.headRef == "" {
		return nil, errors.New("provide --diff or both --base-ref and --head-ref")
	}
	command := exec.CommandContext(ctx, "git", "-C", root, "diff", "--unified=0", "--no-color", "--no-ext-diff", "--find-renames", cfg.baseRef, cfg.headRef)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return nil, fmt.Errorf("git diff %s %s: %w: %s", cfg.baseRef, cfg.headRef, err, strings.TrimSpace(string(exitError.Stderr)))
		}
		return nil, fmt.Errorf("git diff %s %s: %w", cfg.baseRef, cfg.headRef, err)
	}
	return io.NopCloser(strings.NewReader(string(output))), nil
}

func productionGoChanges(changes []covercheck.FileChange) []covercheck.FileChange {
	filtered := make([]covercheck.FileChange, 0, len(changes))
	for _, change := range changes {
		filename := change.NewPath
		if filename == "" {
			filename = change.OldPath
		}
		if !strings.HasSuffix(filename, ".go") || strings.HasSuffix(filename, "_test.go") {
			continue
		}
		filtered = append(filtered, change)
	}
	return filtered
}

func annotateExecutability(root string, changes []covercheck.FileChange) error {
	for index := range changes {
		change := &changes[index]
		if change.Kind == covercheck.ChangeDeleted || change.NewPath == "" {
			continue
		}
		filename := filepath.Join(root, filepath.FromSlash(change.NewPath))
		source, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			return fmt.Errorf("classifying executable statements in %s: %w", change.NewPath, err)
		}
		change.Executability = covercheck.ExecutabilityAbsent
		ast.Inspect(source, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.FuncDecl:
				if value.Body != nil && len(value.Body.List) > 0 {
					change.Executability = covercheck.ExecutabilityPresent
					return false
				}
			case *ast.FuncLit:
				if value.Body != nil && len(value.Body.List) > 0 {
					change.Executability = covercheck.ExecutabilityPresent
					return false
				}
			}
			return change.Executability != covercheck.ExecutabilityPresent
		})
	}
	return nil
}

func readModulePath(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("opening go.mod: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}
	return "", errors.New("go.mod has no module directive")
}

func policyOptions(cfg config) ([]covercheck.PolicyOption, error) {
	var options []covercheck.PolicyOption
	thresholds := []struct {
		value float64
		make  func(float64) covercheck.PolicyOption
	}{
		{cfg.minimumTotal, covercheck.WithMinimumTotal},
		{cfg.minimumPackage, covercheck.WithMinimumPackage},
		{cfg.minimumFile, covercheck.WithMinimumFile},
		{cfg.minimumChanged, covercheck.WithMinimumChanged},
		{cfg.maxTotalDrop, covercheck.WithMaximumTotalRegression},
		{cfg.maxFileDrop, covercheck.WithMaximumFileRegression},
	}
	for _, threshold := range thresholds {
		if threshold.value >= 0 {
			options = append(options, threshold.make(threshold.value))
		}
	}
	if cfg.requireChanged {
		options = append(options, covercheck.WithRequiredChangedFiles())
	}
	if len(cfg.excludes) > 0 {
		options = append(options, covercheck.ExcludeFiles(cfg.excludes...))
	}
	return options, nil
}

func render(w io.Writer, format string, result covercheck.Result) error {
	switch format {
	case "text":
		return covercheck.WriteText(w, result)
	case "markdown":
		return covercheck.WriteMarkdown(w, result)
	case "json":
		return covercheck.WriteJSON(w, result)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}
