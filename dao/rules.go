package dao

import "sort"

// Rule is one field's disposition in a partial write: write a value, skip
// the field, or clear it. The zero Rule is Skip — a rules map
// that doesn't mention a field and one that maps it to the zero Rule mean
// the same thing. Construct via [Write], [Skip], or [Clear]; the kind is
// not exported so the vocabulary stays closed (no sentinel-value pattern).
type Rule struct {
	kind  ruleKind
	value any
}

type ruleKind uint8

const (
	ruleSkip ruleKind = iota // zero value: leave the column alone
	ruleWrite
	ruleClear
)

// Write returns a Rule that stages v for the field (same write path as Set).
func Write(v any) Rule { return Rule{kind: ruleWrite, value: v} }

// Skip returns a Rule that leaves the field alone. It is authoritative: a
// Skip removes the field from the staged write even if Set/SetMap/DefaultValues
// staged a value for it.
func Skip() Rule { return Rule{} }

// Clear returns a Rule that requests the field's cleared state. What that
// means is decided by the Field's clearability declaration:
// the declared ClearValue (default SQL NULL) for a Clearable field, a
// downgrade to Skip — or ErrNotClearable under StrictClears — otherwise.
func Clear() Rule { return Rule{kind: ruleClear} }

// resolvedRule is a field's rule after SetRules resolved it against the
// schema: the write column is the map key, a clear carries its resolved
// value, and a StrictClears violation travels here — NOT through the DAO's
// sticky first-error path — so a later rule for the same field replaces it
// (last-rule-wins, ADR-0010 §2.3). err is non-nil only with kind ruleSkip.
type resolvedRule struct {
	kind  ruleKind
	value any
	err   error
}

// sortedRuleCols returns the rule columns in stable order, so the first
// surviving strict violation reported by stagedSet is deterministic.
func sortedRuleCols(rules map[string]resolvedRule) []string {
	cols := make([]string, 0, len(rules))
	for c := range rules {
		cols = append(cols, c)
	}
	sort.Strings(cols)
	return cols
}
