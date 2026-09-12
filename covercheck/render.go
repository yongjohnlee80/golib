package covercheck

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Result couples a report with the violations produced by a policy evaluation.
type Result struct {
	Report     Report      `json:"report"`
	Violations []Violation `json:"violations"`
	Passed     bool        `json:"passed"`
}

// NewResult constructs a renderable result from a report and violations.
func NewResult(report Report, violations []Violation) Result {
	return Result{Report: report, Violations: violations, Passed: len(violations) == 0}
}

// WriteText writes a compact terminal report.
func WriteText(w io.Writer, result Result) error {
	if _, err := fmt.Fprintf(w, "TOTAL %s -> %s (%s)\n", formatStats(result.Report.Total.Base), formatStats(result.Report.Total.Head), formatDelta(result.Report.Total.Delta)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "CHANGED BLOCKS %s\n\n", formatStats(result.Report.ChangedBlocks)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "FILE\tBASE\tHEAD\tDELTA\tCHANGED\tSTATE"); err != nil {
		return err
	}
	for _, file := range result.Report.Files {
		state := string(file.Kind)
		if file.MissingAtHead {
			state += ",missing"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", file.Path, formatStats(file.Coverage.Base), formatStats(file.Coverage.Head), formatDelta(file.Coverage.Delta), formatStats(file.ChangedBlocks), state); err != nil {
			return err
		}
	}
	return writeViolationText(w, result)
}

// WriteMarkdown writes a report suitable for a CI job summary.
func WriteMarkdown(w io.Writer, result Result) error {
	status := "PASS"
	if !result.Passed {
		status = "FAIL"
	}
	if _, err := fmt.Fprintf(w, "## Coverage check: %s\n\n", status); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "- Total: %s → %s (%s)\n- Changed blocks: %s\n\n", formatStats(result.Report.Total.Base), formatStats(result.Report.Total.Head), formatDelta(result.Report.Total.Delta), formatStats(result.Report.ChangedBlocks)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| File | Base | Head | Delta | Changed blocks | State |\n|---|---:|---:|---:|---:|---|"); err != nil {
		return err
	}
	for _, file := range result.Report.Files {
		state := string(file.Kind)
		if file.MissingAtHead {
			state += ", missing"
		}
		if _, err := fmt.Fprintf(w, "| `%s` | %s | %s | %s | %s | %s |\n", escapeMarkdown(file.Path), formatStats(file.Coverage.Base), formatStats(file.Coverage.Head), formatDelta(file.Coverage.Delta), formatStats(file.ChangedBlocks), state); err != nil {
			return err
		}
	}
	if len(result.Violations) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "\n### Violations"); err != nil {
		return err
	}
	for _, violation := range result.Violations {
		label := violation.Scope
		if violation.Path != "" {
			label += ": `" + escapeMarkdown(violation.Path) + "`"
		}
		if _, err := fmt.Fprintf(w, "- **%s / %s:** %s\n", label, violation.Rule, violation.Message); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSON writes the complete result as indented JSON.
func WriteJSON(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func writeViolationText(w io.Writer, result Result) error {
	if len(result.Violations) == 0 {
		_, err := fmt.Fprintln(w, "\nPASS")
		return err
	}
	if _, err := fmt.Fprintln(w, "\nVIOLATIONS"); err != nil {
		return err
	}
	for _, violation := range result.Violations {
		label := violation.Scope
		if violation.Path != "" {
			label += "/" + violation.Path
		}
		if _, err := fmt.Fprintf(w, "- %s %s: %s\n", label, violation.Rule, violation.Message); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, "\nFAIL")
	return err
}

func formatStats(stats Stats) string {
	percent, ok := stats.Percent()
	if !ok {
		return "N/A"
	}
	return fmt.Sprintf("%.2f%% (%d/%d)", percent, stats.Covered, stats.Total)
}

func formatDelta(delta *float64) string {
	if delta == nil {
		return "N/A"
	}
	return fmt.Sprintf("%+.2fpp", *delta)
}

func escapeMarkdown(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}
