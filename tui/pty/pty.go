package pty

import "strings"

// Cmd describes the program to start.
type Cmd struct {
	// Path is the program, looked up in PATH when it has no slash.
	Path string
	// Args are the program's arguments, not counting the program itself.
	Args []string
	// Env is the child's environment; nil inherits the caller's.
	Env []string
	// Dir is the child's working directory; empty keeps the caller's.
	Dir string
	// Rows and Cols are the terminal's starting size; zero means 24 x 80.
	Rows, Cols int
}

const (
	defaultRows = 24
	defaultCols = 80
)

func (c Cmd) size() (rows, cols int) {
	rows, cols = c.Rows, c.Cols
	if rows <= 0 {
		rows = defaultRows
	}
	if cols <= 0 {
		cols = defaultCols
	}
	return rows, cols
}

// lastSettingWins is env with each name's last setting alone, in env's order otherwise, as os/exec
// keeps a Cmd's environment: a program reads one value per name, and a later setting (a caller's
// over the ones it inherited) is the one meant. caseBlind matches names as Windows does (Path and
// PATH are one name). A name is what comes before the first "=" after its first character, so
// Windows' per-drive entries ("=C:=C:\dir") keep their own names.
func lastSettingWins(env []string, caseBlind bool) []string {
	name := func(kv string) string {
		k := kv
		if len(kv) > 1 {
			if i := strings.IndexByte(kv[1:], '='); i >= 0 {
				k = kv[:i+1]
			}
		}
		if caseBlind {
			k = strings.ToUpper(k)
		}
		return k
	}
	seen := make(map[string]bool, len(env))
	keep := make([]bool, len(env))
	for i := len(env) - 1; i >= 0; i-- {
		if k := name(env[i]); !seen[k] {
			seen[k], keep[i] = true, true
		}
	}
	out := make([]string, 0, len(seen))
	for i, kv := range env {
		if keep[i] {
			out = append(out, kv)
		}
	}
	return out
}
