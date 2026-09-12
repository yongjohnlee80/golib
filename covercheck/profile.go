package covercheck

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrInvalidProfile identifies malformed or unsafe coverage profile input.
var ErrInvalidProfile = errors.New("covercheck: invalid coverage profile")

// Mode is a Go coverage counter mode.
type Mode string

const (
	// ModeSet records whether each block executed.
	ModeSet Mode = "set"
	// ModeCount records the execution count for each block.
	ModeCount Mode = "count"
	// ModeAtomic records execution counts with atomic updates.
	ModeAtomic Mode = "atomic"
)

// Position identifies a one-based source position.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Block is one entry from a Go coverage profile.
type Block struct {
	File       string   `json:"file"`
	Start      Position `json:"start"`
	End        Position `json:"end"`
	Statements int      `json:"statements"`
	Count      uint64   `json:"count"`
}

// Profile is a parsed and path-normalized Go coverage profile.
type Profile struct {
	Mode  Mode               `json:"mode"`
	Files map[string][]Block `json:"files"`
}

// ProfileError describes one invalid coverage-profile line.
type ProfileError struct {
	Line   int
	Text   string
	Reason string
}

func (e *ProfileError) Error() string {
	if e.Line == 0 {
		return fmt.Sprintf("%v: %s", ErrInvalidProfile, e.Reason)
	}
	return fmt.Sprintf("%v at line %d: %s", ErrInvalidProfile, e.Line, e.Reason)
}

// Unwrap makes ProfileError comparable with [ErrInvalidProfile].
func (e *ProfileError) Unwrap() error { return ErrInvalidProfile }

type parseConfig struct {
	modulePath string
	moduleRoot string
}

// ParseOption configures [ParseProfile].
type ParseOption func(*parseConfig) error

// WithModulePath maps filenames prefixed by modulePath to repository-relative
// paths. The value is normally the module directive from go.mod.
func WithModulePath(modulePath string) ParseOption {
	return func(cfg *parseConfig) error {
		modulePath = strings.TrimSuffix(strings.TrimSpace(modulePath), "/")
		if modulePath == "" {
			return fmt.Errorf("%w: module path is empty", ErrInvalidProfile)
		}
		cfg.modulePath = modulePath
		return nil
	}
}

// WithModuleRoot permits absolute profile filenames below root and maps them
// to repository-relative paths. It is useful for profiles emitted by custom
// coverage tooling.
func WithModuleRoot(root string) ParseOption {
	return func(cfg *parseConfig) error {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("%w: resolving module root: %v", ErrInvalidProfile, err)
		}
		cfg.moduleRoot = filepath.Clean(absolute)
		return nil
	}
}

// ParseProfile parses the textual format emitted by go test -coverprofile.
// Duplicate blocks are coalesced so they contribute statement weight once;
// the retained count is the greatest count observed for that block.
func ParseProfile(r io.Reader, opts ...ParseOption) (*Profile, error) {
	cfg := parseConfig{}
	for _, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("%w: nil parse option", ErrInvalidProfile)
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("%w: reading header: %v", ErrInvalidProfile, err)
		}
		return nil, &ProfileError{Reason: "missing mode header"}
	}

	header := strings.TrimSpace(scanner.Text())
	if !strings.HasPrefix(header, "mode: ") {
		return nil, &ProfileError{Line: 1, Text: scanner.Text(), Reason: "expected mode header"}
	}
	mode := Mode(strings.TrimSpace(strings.TrimPrefix(header, "mode: ")))
	if mode != ModeSet && mode != ModeCount && mode != ModeAtomic {
		return nil, &ProfileError{Line: 1, Text: scanner.Text(), Reason: fmt.Sprintf("unsupported mode %q", mode)}
	}

	profile := &Profile{Mode: mode, Files: make(map[string][]Block)}
	seen := make(map[string]map[string]int)
	lineNumber := 1
	for scanner.Scan() {
		lineNumber++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		block, err := parseProfileBlock(text, cfg)
		if err != nil {
			return nil, &ProfileError{Line: lineNumber, Text: scanner.Text(), Reason: err.Error()}
		}
		key := blockKey(block)
		if seen[block.File] == nil {
			seen[block.File] = make(map[string]int)
		}
		if index, ok := seen[block.File][key]; ok {
			if block.Count > profile.Files[block.File][index].Count {
				profile.Files[block.File][index].Count = block.Count
			}
			continue
		}
		seen[block.File][key] = len(profile.Files[block.File])
		profile.Files[block.File] = append(profile.Files[block.File], block)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: reading body: %v", ErrInvalidProfile, err)
	}
	return profile, nil
}

func parseProfileBlock(text string, cfg parseConfig) (Block, error) {
	fields := strings.Fields(text)
	if len(fields) != 3 {
		return Block{}, errors.New("expected filename:range, statement count, and execution count")
	}
	separator := strings.LastIndex(fields[0], ":")
	if separator <= 0 || separator == len(fields[0])-1 {
		return Block{}, errors.New("missing filename or source range")
	}
	filename, err := normalizeProfilePath(fields[0][:separator], cfg)
	if err != nil {
		return Block{}, err
	}
	ranges := strings.Split(fields[0][separator+1:], ",")
	if len(ranges) != 2 {
		return Block{}, errors.New("source range must contain start and end")
	}
	start, err := parsePosition(ranges[0])
	if err != nil {
		return Block{}, fmt.Errorf("invalid start position: %w", err)
	}
	end, err := parsePosition(ranges[1])
	if err != nil {
		return Block{}, fmt.Errorf("invalid end position: %w", err)
	}
	if end.Line < start.Line || (end.Line == start.Line && end.Column < start.Column) {
		return Block{}, errors.New("source range ends before it starts")
	}
	statements, err := strconv.Atoi(fields[1])
	if err != nil || statements < 0 {
		return Block{}, fmt.Errorf("invalid statement count %q", fields[1])
	}
	count, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return Block{}, fmt.Errorf("invalid execution count %q", fields[2])
	}
	return Block{File: filename, Start: start, End: end, Statements: statements, Count: count}, nil
}

func parsePosition(value string) (Position, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return Position{}, errors.New("position must be line.column")
	}
	line, err := strconv.Atoi(parts[0])
	if err != nil || line < 1 {
		return Position{}, errors.New("line must be a positive integer")
	}
	column, err := strconv.Atoi(parts[1])
	if err != nil || column < 1 {
		return Position{}, errors.New("column must be a positive integer")
	}
	return Position{Line: line, Column: column}, nil
}

func normalizeProfilePath(filename string, cfg parseConfig) (string, error) {
	filename = filepath.ToSlash(strings.TrimSpace(filename))
	if filename == "" {
		return "", errors.New("empty filename")
	}
	if cfg.moduleRoot != "" && filepath.IsAbs(filepath.FromSlash(filename)) {
		relative, err := filepath.Rel(cfg.moduleRoot, filepath.FromSlash(filename))
		if err != nil {
			return "", fmt.Errorf("mapping absolute filename: %w", err)
		}
		filename = filepath.ToSlash(relative)
	} else if filepath.IsAbs(filepath.FromSlash(filename)) {
		return "", errors.New("absolute filename requires a module root")
	}
	if cfg.modulePath != "" && strings.HasPrefix(filename, cfg.modulePath+"/") {
		filename = strings.TrimPrefix(filename, cfg.modulePath+"/")
	} else if cfg.modulePath != "" {
		first, _, _ := strings.Cut(filename, "/")
		if strings.Contains(first, ".") {
			return "", fmt.Errorf("filename %q is outside module %q", filename, cfg.modulePath)
		}
	}
	cleaned := path.Clean(filename)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("filename %q escapes the module", filename)
	}
	return cleaned, nil
}

func blockKey(block Block) string {
	return fmt.Sprintf("%d.%d,%d.%d/%d", block.Start.Line, block.Start.Column, block.End.Line, block.End.Column, block.Statements)
}
