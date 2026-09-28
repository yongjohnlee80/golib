// Package deploy applies a product's schema as numbered SQL scripts and keeps
// a ledger of what each database has applied.
//
// The scripts live in a file tree, one directory per engine, named for the
// dialect (sqlite, postgres, mysql):
//
//	sqlite/000001_update_initialize_tables.sql   the baseline: no revert
//	sqlite/000002_update_<slug>.sql              a change …
//	sqlite/000002_revert_<slug>.sql              … and its undo
//
// A product embeds the tree and hands it to [New]. Numbers are dense and never
// reused. A released script never changes: the ledger records the digest each
// script was applied with, and a later digest that differs is reported, never
// re-run.
//
// The scripts are the DDL. Reading and writing rows is the product's DAO; the
// only statements this package runs itself are the scripts, the ledger's
// creation, and the engine's lock.
package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
	gsql "github.com/yongjohnlee80/golib/parse/sql"
)

// Kind is what a script does.
type Kind string

const (
	Update Kind = "update"
	Revert Kind = "revert"
)

// Script is one file.
type Script struct {
	Number int
	Kind   Kind
	Slug   string
	// Name is the file name, which the ledger records: 000001_update_initialize_tables.sql.
	Name string
	// Body is the file as written; SHA256 its digest, hex.
	Body   []byte
	SHA256 string
	engine string
}

// Statement is one statement of a script, and the line it starts on.
type Statement struct {
	Text string
	Line int
}

var nameRE = regexp.MustCompile(`^(\d{6})_(update|revert)_([a-z0-9_]+)\.sql$`)

// Load reads the scripts for engine (a dialect name, and so a directory of
// fsys), updates and reverts, by number then kind (update first), and refuses
// a set the runner could not apply in the order it was written:
//
//   - a file not named NNNNNN_(update|revert)_<slug>.sql, which would never run;
//   - update numbers that are not dense from 000001, or two updates under one
//     number: a script shipped later into a gap would run after the ones above
//     it, and the ledger would record an order no release applied;
//   - a revert with no update of the same number and slug, a second revert of
//     one number, or a revert of the baseline, which would drop everything.
func Load(fsys fs.FS, engine string) ([]Script, error) {
	if _, err := lexer(engine); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(fsys, engine)
	if err != nil {
		return nil, fmt.Errorf("deploy: no scripts for engine %q: %w", engine, err)
	}
	var out []Script
	for _, e := range entries {
		m := nameRE.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("deploy: %s/%s is not named NNNNNN_(update|revert)_<slug>.sql", engine, e.Name())
		}
		n, _ := strconv.Atoi(m[1])
		body, err := fs.ReadFile(fsys, path.Join(engine, e.Name()))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		out = append(out, Script{Number: n, Kind: Kind(m[2]), Slug: m[3], Name: e.Name(),
			Body: body, SHA256: hex.EncodeToString(sum[:]), engine: engine})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Number != out[j].Number {
			return out[i].Number < out[j].Number
		}
		return out[i].Kind == Update && out[j].Kind == Revert
	})
	if err := validate(engine, out); err != nil {
		return nil, err
	}
	return out, nil
}

// validate is Load's check of the whole set, before any of it can run.
func validate(engine string, all []Script) error {
	updates := map[int]Script{}
	reverts := map[int]Script{}
	for _, s := range all {
		m := updates
		if s.Kind == Revert {
			m = reverts
		}
		if prev, dup := m[s.Number]; dup {
			return errs.Wrap(errs.ErrInvalidArgument, "deploy: %s/%s and %s are both %s script %06d", engine, prev.Name, s.Name, s.Kind, s.Number)
		}
		m[s.Number] = s
	}
	for n := 1; n <= len(updates); n++ {
		if _, ok := updates[n]; !ok {
			return errs.Wrap(errs.ErrInvalidArgument, "deploy: %s has %d update scripts but none numbered %06d: numbers are dense from 000001", engine, len(updates), n)
		}
	}
	for n, r := range reverts {
		u, ok := updates[n]
		switch {
		case n == 1:
			return errs.Wrap(errs.ErrInvalidArgument, "deploy: %s/%s reverts the baseline, which would drop the schema; the baseline has no revert", engine, r.Name)
		case !ok:
			return errs.Wrap(errs.ErrInvalidArgument, "deploy: %s/%s has no update script numbered %06d", engine, r.Name, n)
		case u.Slug != r.Slug:
			return errs.Wrap(errs.ErrInvalidArgument, "deploy: %s/%s does not revert %s: the slugs differ", engine, r.Name, u.Name)
		}
	}
	return nil
}

// Statements splits the script into statements with its engine's lexical
// rules, each with the line it starts on, so an error names where in the file
// it failed. A comment never begins a statement, so a script that is only
// comments (one with no work on this engine, saying why) runs nothing.
func (s Script) Statements() ([]Statement, error) {
	lx, err := lexer(s.engine)
	if err != nil {
		return nil, err
	}
	stmts, err := lx.Parse(s.Body)
	if err != nil {
		return nil, fmt.Errorf("deploy: %s/%s: %w", s.engine, s.Name, err)
	}
	out := make([]Statement, 0, len(stmts))
	for _, st := range stmts {
		out = append(out, Statement{Text: st.Text, Line: st.Pos.Line})
	}
	return out, nil
}

// lexer is how an engine's scripts split into statements.
func lexer(engine string) (gsql.SQL, error) {
	switch engine {
	case dao.DialectSQLite:
		return gsql.SQL{TriggerBodies: true}, nil
	case dao.DialectPostgres:
		return gsql.SQL{DollarQuotes: true, NestedBlockComments: true, EStringEscapes: true}, nil
	case dao.DialectMySQL:
		return gsql.SQL{Backticks: true}, nil
	}
	return gsql.SQL{}, fmt.Errorf("%w: schema scripts for engine %q", dao.ErrUnsupported, engine)
}
