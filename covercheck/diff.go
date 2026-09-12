package covercheck

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidDiff identifies malformed unified-diff input.
var ErrInvalidDiff = errors.New("covercheck: invalid unified diff")

var hunkPattern = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

// ChangeKind classifies a changed file.
type ChangeKind string

const (
	// ChangeModified identifies a file present at both revisions.
	ChangeModified ChangeKind = "modified"
	// ChangeAdded identifies a file present only at the head revision.
	ChangeAdded ChangeKind = "added"
	// ChangeDeleted identifies a file present only at the base revision.
	ChangeDeleted ChangeKind = "deleted"
	// ChangeRenamed identifies a Git-detected rename.
	ChangeRenamed ChangeKind = "renamed"
)

// LineRange is an inclusive one-based range in the head revision.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// FileChange describes one file and its added or modified head-side lines.
type FileChange struct {
	OldPath string      `json:"old_path,omitempty"`
	NewPath string      `json:"new_path,omitempty"`
	Kind    ChangeKind  `json:"kind"`
	Ranges  []LineRange `json:"ranges,omitempty"`
}

// DiffError describes one malformed unified-diff line.
type DiffError struct {
	Line   int
	Reason string
}

func (e *DiffError) Error() string {
	return fmt.Sprintf("%v at line %d: %s", ErrInvalidDiff, e.Line, e.Reason)
}

// Unwrap makes DiffError comparable with [ErrInvalidDiff].
func (e *DiffError) Unwrap() error { return ErrInvalidDiff }

// ParseUnifiedDiff parses file identities and head-side ranges from a Git
// unified diff. Call Git with --unified=0 and --find-renames for precise input.
func ParseUnifiedDiff(r io.Reader) ([]FileChange, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var changes []FileChange
	var current *FileChange
	lineNumber := 0
	flush := func() {
		if current == nil {
			return
		}
		classifyChange(current)
		changes = append(changes, *current)
		current = nil
	}
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			current = &FileChange{}
		case current == nil:
			continue
		case strings.HasPrefix(line, "rename from "):
			value, err := normalizeDiffPath(strings.TrimPrefix(line, "rename from "), "")
			if err != nil {
				return nil, &DiffError{Line: lineNumber, Reason: err.Error()}
			}
			current.OldPath = value
			current.Kind = ChangeRenamed
		case strings.HasPrefix(line, "rename to "):
			value, err := normalizeDiffPath(strings.TrimPrefix(line, "rename to "), "")
			if err != nil {
				return nil, &DiffError{Line: lineNumber, Reason: err.Error()}
			}
			current.NewPath = value
			current.Kind = ChangeRenamed
		case strings.HasPrefix(line, "--- "):
			value, err := normalizeDiffPath(strings.TrimPrefix(line, "--- "), "a/")
			if err != nil {
				return nil, &DiffError{Line: lineNumber, Reason: err.Error()}
			}
			current.OldPath = value
		case strings.HasPrefix(line, "+++ "):
			value, err := normalizeDiffPath(strings.TrimPrefix(line, "+++ "), "b/")
			if err != nil {
				return nil, &DiffError{Line: lineNumber, Reason: err.Error()}
			}
			current.NewPath = value
		case strings.HasPrefix(line, "@@ "):
			rangeValue, ok, err := parseHunkRange(line)
			if err != nil {
				return nil, &DiffError{Line: lineNumber, Reason: err.Error()}
			}
			if ok {
				current.Ranges = append(current.Ranges, rangeValue)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: reading input: %v", ErrInvalidDiff, err)
	}
	flush()
	return changes, nil
}

func parseHunkRange(line string) (LineRange, bool, error) {
	matches := hunkPattern.FindStringSubmatch(line)
	if matches == nil {
		return LineRange{}, false, errors.New("malformed hunk header")
	}
	start, _ := strconv.Atoi(matches[3])
	count := 1
	if matches[4] != "" {
		count, _ = strconv.Atoi(matches[4])
	}
	if count == 0 {
		return LineRange{}, false, nil
	}
	return LineRange{Start: start, End: start + count - 1}, true, nil
}

func normalizeDiffPath(value, prefix string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "/dev/null" {
		return "", nil
	}
	if strings.HasPrefix(value, "\"") {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid quoted path: %w", err)
		}
		value = unquoted
	}
	value = strings.TrimPrefix(value, prefix)
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("path %q escapes the repository", value)
	}
	return cleaned, nil
}

func classifyChange(change *FileChange) {
	switch {
	case change.Kind == ChangeRenamed:
		return
	case change.OldPath == "":
		change.Kind = ChangeAdded
	case change.NewPath == "":
		change.Kind = ChangeDeleted
	default:
		change.Kind = ChangeModified
	}
}
