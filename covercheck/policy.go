package covercheck

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
)

// ErrInvalidPolicy identifies an invalid policy option.
var ErrInvalidPolicy = errors.New("covercheck: invalid policy")

// Violation is one failed coverage rule.
type Violation struct {
	Scope    string  `json:"scope"`
	Path     string  `json:"path,omitempty"`
	Rule     string  `json:"rule"`
	Actual   float64 `json:"actual"`
	Required float64 `json:"required"`
	Message  string  `json:"message"`
}

type filePolicy struct {
	minimumFile    *float64
	minimumChanged *float64
	maxRegression  *float64
}

type fileOverride struct {
	pattern *regexp.Regexp
	policy  filePolicy
}

type policy struct {
	minimumTotal   *float64
	minimumPackage *float64
	minimumFile    *float64
	minimumChanged *float64
	maxTotalDrop   *float64
	maxFileDrop    *float64
	requireChanged bool
	excludes       []*regexp.Regexp
	overrides      []fileOverride
}

// PolicyOption configures [Evaluate].
type PolicyOption func(*policy) error

// FilePolicyOption configures one [ForFiles] override.
type FilePolicyOption func(*filePolicy) error

// WithMinimumTotal requires at least percent total head coverage.
func WithMinimumTotal(percent float64) PolicyOption {
	return thresholdPolicyOption("minimum total", percent, func(p *policy, value *float64) { p.minimumTotal = value })
}

// WithMinimumPackage requires at least percent head coverage in every package.
func WithMinimumPackage(percent float64) PolicyOption {
	return thresholdPolicyOption("minimum package", percent, func(p *policy, value *float64) { p.minimumPackage = value })
}

// WithMinimumFile requires at least percent head coverage for every changed,
// non-deleted file that contains executable statements.
func WithMinimumFile(percent float64) PolicyOption {
	return thresholdPolicyOption("minimum file", percent, func(p *policy, value *float64) { p.minimumFile = value })
}

// WithMinimumChanged requires at least percent coverage across all changed
// blocks in the report.
func WithMinimumChanged(percent float64) PolicyOption {
	return thresholdPolicyOption("minimum changed", percent, func(p *policy, value *float64) { p.minimumChanged = value })
}

// WithMaximumTotalRegression permits at most percentagePoints of total
// coverage decrease.
func WithMaximumTotalRegression(percentagePoints float64) PolicyOption {
	return thresholdPolicyOption("maximum total regression", percentagePoints, func(p *policy, value *float64) { p.maxTotalDrop = value })
}

// WithMaximumFileRegression permits at most percentagePoints of coverage
// decrease in each changed file.
func WithMaximumFileRegression(percentagePoints float64) PolicyOption {
	return thresholdPolicyOption("maximum file regression", percentagePoints, func(p *policy, value *float64) { p.maxFileDrop = value })
}

// WithRequiredChangedFiles fails when a changed production Go file is absent
// from the head profile.
func WithRequiredChangedFiles() PolicyOption {
	return func(p *policy) error {
		p.requireChanged = true
		return nil
	}
}

// ExcludeFiles excludes paths matching any supplied Go regular expression
// from file-level and changed-block policy checks.
func ExcludeFiles(patterns ...string) PolicyOption {
	return func(p *policy) error {
		for _, pattern := range patterns {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("%w: exclusion %q: %v", ErrInvalidPolicy, pattern, err)
			}
			p.excludes = append(p.excludes, compiled)
		}
		return nil
	}
}

// ForFiles installs the first-match override for paths matching pattern.
func ForFiles(pattern string, opts ...FilePolicyOption) PolicyOption {
	return func(p *policy) error {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("%w: override %q: %v", ErrInvalidPolicy, pattern, err)
		}
		rule := filePolicy{}
		for _, opt := range opts {
			if opt == nil {
				return fmt.Errorf("%w: nil file policy option", ErrInvalidPolicy)
			}
			if err := opt(&rule); err != nil {
				return err
			}
		}
		p.overrides = append(p.overrides, fileOverride{pattern: compiled, policy: rule})
		return nil
	}
}

// WithOverrideMinimumFile overrides minimum full-file coverage.
func WithOverrideMinimumFile(percent float64) FilePolicyOption {
	return fileThresholdOption("override minimum file", percent, func(p *filePolicy, value *float64) { p.minimumFile = value })
}

// WithOverrideMinimumChanged overrides minimum changed-block coverage.
func WithOverrideMinimumChanged(percent float64) FilePolicyOption {
	return fileThresholdOption("override minimum changed", percent, func(p *filePolicy, value *float64) { p.minimumChanged = value })
}

// WithOverrideMaximumRegression overrides the allowed file regression.
func WithOverrideMaximumRegression(percentagePoints float64) FilePolicyOption {
	return fileThresholdOption("override maximum regression", percentagePoints, func(p *filePolicy, value *float64) { p.maxRegression = value })
}

// Evaluate applies policy options to a report and returns all violations in a
// stable scope/path/rule order.
func Evaluate(report Report, opts ...PolicyOption) ([]Violation, error) {
	configured := policy{}
	for _, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("%w: nil option", ErrInvalidPolicy)
		}
		if err := opt(&configured); err != nil {
			return nil, err
		}
	}
	var violations []Violation
	if configured.minimumTotal != nil {
		violations = appendMinimum(violations, "total", "", "minimum_total", report.Total.Head, *configured.minimumTotal)
	}
	if configured.maxTotalDrop != nil && report.Total.Delta != nil && *report.Total.Delta < -*configured.maxTotalDrop {
		violations = append(violations, regressionViolation("total", "", "maximum_total_regression", *report.Total.Delta, *configured.maxTotalDrop))
	}
	if configured.minimumPackage != nil {
		for _, packageReport := range report.Packages {
			if packageReport.Coverage.State == EntityDeleted {
				continue
			}
			violations = appendMinimum(violations, "package", packageReport.Path, "minimum_package", packageReport.Coverage.Head, *configured.minimumPackage)
		}
	}
	if configured.minimumChanged != nil {
		changed := report.ChangedBlocks
		if len(configured.excludes) > 0 {
			changed = Stats{}
			for _, file := range report.Files {
				if file.Kind == ChangeDeleted || matchesAny(file.Path, configured.excludes) {
					continue
				}
				changed.Covered += file.ChangedBlocks.Covered
				changed.Total += file.ChangedBlocks.Total
			}
		}
		violations = appendMinimum(violations, "changed", "", "minimum_changed", changed, *configured.minimumChanged)
	}
	for _, file := range report.Files {
		if file.Kind == ChangeDeleted || matchesAny(file.Path, configured.excludes) {
			continue
		}
		fileRules := filePolicy{minimumFile: configured.minimumFile, maxRegression: configured.maxFileDrop}
		for _, override := range configured.overrides {
			if override.pattern.MatchString(file.Path) {
				mergeFilePolicy(&fileRules, override.policy)
				break
			}
		}
		if configured.requireChanged && file.MissingAtHead {
			violations = append(violations, Violation{Scope: "file", Path: file.Path, Rule: "changed_file_missing", Required: 1, Message: "changed production Go file is absent from the head profile"})
		}
		if fileRules.minimumFile != nil {
			violations = appendMinimum(violations, "file", file.Path, "minimum_file", file.Coverage.Head, *fileRules.minimumFile)
		}
		if fileRules.minimumChanged != nil {
			violations = appendMinimum(violations, "file", file.Path, "minimum_changed", file.ChangedBlocks, *fileRules.minimumChanged)
		}
		if fileRules.maxRegression != nil && file.Coverage.Delta != nil && *file.Coverage.Delta < -*fileRules.maxRegression {
			violations = append(violations, regressionViolation("file", file.Path, "maximum_file_regression", *file.Coverage.Delta, *fileRules.maxRegression))
		}
	}
	sort.SliceStable(violations, func(i, j int) bool {
		left := violations[i].Scope + "\x00" + violations[i].Path + "\x00" + violations[i].Rule
		right := violations[j].Scope + "\x00" + violations[j].Path + "\x00" + violations[j].Rule
		return left < right
	})
	return violations, nil
}

func appendMinimum(violations []Violation, scope, pathValue, rule string, stats Stats, required float64) []Violation {
	actual, measured := stats.Percent()
	if !measured || actual >= required {
		return violations
	}
	return append(violations, Violation{
		Scope: scope, Path: pathValue, Rule: rule, Actual: actual, Required: required,
		Message: fmt.Sprintf("coverage %.2f%% is below required %.2f%%", actual, required),
	})
}

func regressionViolation(scope, pathValue, rule string, delta, allowedDrop float64) Violation {
	return Violation{
		Scope: scope, Path: pathValue, Rule: rule, Actual: delta, Required: -allowedDrop,
		Message: fmt.Sprintf("coverage changed %.2f percentage points; maximum allowed decrease is %.2f", delta, allowedDrop),
	}
}

func thresholdPolicyOption(name string, value float64, set func(*policy, *float64)) PolicyOption {
	return func(p *policy) error {
		if err := validatePercent(name, value); err != nil {
			return err
		}
		valueCopy := value
		set(p, &valueCopy)
		return nil
	}
}

func fileThresholdOption(name string, value float64, set func(*filePolicy, *float64)) FilePolicyOption {
	return func(p *filePolicy) error {
		if err := validatePercent(name, value); err != nil {
			return err
		}
		valueCopy := value
		set(p, &valueCopy)
		return nil
	}
}

func validatePercent(name string, value float64) error {
	if value < 0 || value > 100 {
		return fmt.Errorf("%w: %s must be between 0 and 100", ErrInvalidPolicy, name)
	}
	return nil
}

func matchesAny(value string, patterns []*regexp.Regexp) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func mergeFilePolicy(target *filePolicy, override filePolicy) {
	if override.minimumFile != nil {
		target.minimumFile = override.minimumFile
	}
	if override.minimumChanged != nil {
		target.minimumChanged = override.minimumChanged
	}
	if override.maxRegression != nil {
		target.maxRegression = override.maxRegression
	}
}
