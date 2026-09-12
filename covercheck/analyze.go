package covercheck

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// ErrInvalidInput identifies incompatible analysis inputs.
var ErrInvalidInput = errors.New("covercheck: invalid analysis input")

// Stats is a statement-weighted coverage measurement.
type Stats struct {
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

// Percent returns the covered percentage and whether the measurement contains
// any executable statements.
func (s Stats) Percent() (float64, bool) {
	if s.Total == 0 {
		return 0, false
	}
	return float64(s.Covered) * 100 / float64(s.Total), true
}

// EntityState describes whether an analyzed entity exists at each revision.
type EntityState string

const (
	// EntityExisting is present at both revisions.
	EntityExisting EntityState = "existing"
	// EntityAdded is present only at the head revision.
	EntityAdded EntityState = "added"
	// EntityDeleted is present only at the base revision.
	EntityDeleted EntityState = "deleted"
)

// Comparison holds base/head measurements and an optional percentage-point
// delta. Delta is nil when either side has no executable statements.
type Comparison struct {
	Base  Stats       `json:"base"`
	Head  Stats       `json:"head"`
	Delta *float64    `json:"delta_percentage_points,omitempty"`
	State EntityState `json:"state"`
}

// FileReport is the comparison for one changed source file.
type FileReport struct {
	Path          string     `json:"path"`
	OldPath       string     `json:"old_path,omitempty"`
	Kind          ChangeKind `json:"kind"`
	Coverage      Comparison `json:"coverage"`
	ChangedBlocks Stats      `json:"changed_blocks"`
	MissingAtHead bool       `json:"missing_at_head"`
}

// PackageReport is the comparison for one Go package directory.
type PackageReport struct {
	Path     string     `json:"path"`
	Coverage Comparison `json:"coverage"`
}

// Report is a deterministic base/head coverage comparison.
type Report struct {
	Mode          Mode            `json:"mode"`
	Total         Comparison      `json:"total"`
	ChangedBlocks Stats           `json:"changed_blocks"`
	Packages      []PackageReport `json:"packages"`
	Files         []FileReport    `json:"files"`
}

// Analyze compares profiles and intersects head coverage blocks with changed
// head-side line ranges. Both profiles must use the same coverage mode.
func Analyze(base, head *Profile, changes []FileChange) (Report, error) {
	if base == nil || head == nil {
		return Report{}, fmt.Errorf("%w: base and head profiles are required", ErrInvalidInput)
	}
	if base.Mode != head.Mode {
		return Report{}, fmt.Errorf("%w: profile modes differ (%s vs %s)", ErrInvalidInput, base.Mode, head.Mode)
	}

	baseFiles := profileFileStats(base)
	headFiles := profileFileStats(head)
	report := Report{
		Mode:     head.Mode,
		Total:    compareStats(sumStats(baseFiles), sumStats(headFiles), true, true),
		Packages: comparePackages(baseFiles, headFiles),
	}

	for _, change := range changes {
		file := analyzeFile(change, base, head, baseFiles, headFiles)
		report.Files = append(report.Files, file)
		report.ChangedBlocks.Covered += file.ChangedBlocks.Covered
		report.ChangedBlocks.Total += file.ChangedBlocks.Total
	}
	sort.Slice(report.Files, func(i, j int) bool {
		if report.Files[i].Path == report.Files[j].Path {
			return report.Files[i].OldPath < report.Files[j].OldPath
		}
		return report.Files[i].Path < report.Files[j].Path
	})
	return report, nil
}

func analyzeFile(change FileChange, base, head *Profile, baseFiles, headFiles map[string]Stats) FileReport {
	oldPath := change.OldPath
	newPath := change.NewPath
	if oldPath == "" {
		oldPath = newPath
	}
	if newPath == "" {
		newPath = oldPath
	}
	baseStats, baseOK := baseFiles[oldPath]
	headStats, headOK := headFiles[newPath]
	file := FileReport{
		Path:          newPath,
		OldPath:       change.OldPath,
		Kind:          change.Kind,
		Coverage:      compareStats(baseStats, headStats, baseOK, headOK),
		MissingAtHead: change.Kind != ChangeDeleted && isProductionGo(newPath) && change.Executability != ExecutabilityAbsent && !headOK,
	}
	if change.Kind != ChangeDeleted {
		file.ChangedBlocks = intersectStats(head.Files[newPath], change.Ranges)
	}
	return file
}

func comparePackages(baseFiles, headFiles map[string]Stats) []PackageReport {
	basePackages := aggregatePackages(baseFiles)
	headPackages := aggregatePackages(headFiles)
	names := make(map[string]struct{}, len(basePackages)+len(headPackages))
	for name := range basePackages {
		names[name] = struct{}{}
	}
	for name := range headPackages {
		names[name] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	reports := make([]PackageReport, 0, len(ordered))
	for _, name := range ordered {
		baseStats, baseOK := basePackages[name]
		headStats, headOK := headPackages[name]
		reports = append(reports, PackageReport{
			Path:     name,
			Coverage: compareStats(baseStats, headStats, baseOK, headOK),
		})
	}
	return reports
}

func compareStats(base, head Stats, baseOK, headOK bool) Comparison {
	comparison := Comparison{Base: base, Head: head, State: EntityExisting}
	switch {
	case !baseOK && headOK:
		comparison.State = EntityAdded
	case baseOK && !headOK:
		comparison.State = EntityDeleted
	}
	basePercent, baseMeasured := base.Percent()
	headPercent, headMeasured := head.Percent()
	if baseOK && headOK && baseMeasured && headMeasured {
		delta := headPercent - basePercent
		comparison.Delta = &delta
	}
	return comparison
}

func profileFileStats(profile *Profile) map[string]Stats {
	result := make(map[string]Stats, len(profile.Files))
	for filename, blocks := range profile.Files {
		result[filename] = blocksStats(blocks)
	}
	return result
}

func blocksStats(blocks []Block) Stats {
	var result Stats
	for _, block := range blocks {
		result.Total += block.Statements
		if block.Count > 0 {
			result.Covered += block.Statements
		}
	}
	return result
}

func intersectStats(blocks []Block, ranges []LineRange) Stats {
	var result Stats
	for _, block := range blocks {
		if !intersectsAny(block, ranges) {
			continue
		}
		result.Total += block.Statements
		if block.Count > 0 {
			result.Covered += block.Statements
		}
	}
	return result
}

func intersectsAny(block Block, ranges []LineRange) bool {
	endLine := block.End.Line
	if block.End.Column == 1 && endLine > block.Start.Line {
		endLine--
	}
	for _, changed := range ranges {
		if block.Start.Line <= changed.End && changed.Start <= endLine {
			return true
		}
	}
	return false
}

func aggregatePackages(files map[string]Stats) map[string]Stats {
	packages := make(map[string]Stats)
	for filename, stats := range files {
		packageName := path.Dir(filename)
		if packageName == "." {
			packageName = "(root)"
		}
		value := packages[packageName]
		value.Covered += stats.Covered
		value.Total += stats.Total
		packages[packageName] = value
	}
	return packages
}

func sumStats(values map[string]Stats) Stats {
	var result Stats
	for _, stats := range values {
		result.Covered += stats.Covered
		result.Total += stats.Total
	}
	return result
}

func isProductionGo(filename string) bool {
	return strings.HasSuffix(filename, ".go") && !strings.HasSuffix(filename, "_test.go")
}
