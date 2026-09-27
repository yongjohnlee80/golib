package dao

import (
	"errors"
	"testing"
)

// countedResult reports a fixed affected count, or its error.
type countedResult struct {
	n   int64
	err error
}

func (r countedResult) RowsAffected() (int64, error) { return r.n, r.err }
func (countedResult) LastInsertId() (int64, error)   { return 0, nil }

func TestUpdateAffected_ReportsTheDriversCount(t *testing.T) {
	t.Parallel()
	for _, n := range []int64{0, 1, 3} {
		conn := newConn()
		conn.result = countedResult{n: n}
		got, err := UpdateAffected(buildSchema(conn).DAO().With(aID, "1").Set(aName, "x"))
		if err != nil || got != n {
			t.Errorf("driver reported %d: UpdateAffected = %d, %v", n, got, err)
		}
		if want := `UPDATE "artist" SET "name" = $1 WHERE artist.id = $2`; conn.lastExec != want {
			t.Errorf("sql = %q, want Update's %q", conn.lastExec, want)
		}
	}
}

// Zero is an ANSWER ("the condition did not hold"), so a driver that cannot
// count must not produce one.
func TestUpdateAffected_ADriverThatCannotCountIsUnsupportedNotZero(t *testing.T) {
	t.Parallel()
	conn := newConn()
	conn.result = countedResult{err: errors.New("not reported")}
	n, err := UpdateAffected(buildSchema(conn).DAO().With(aID, "1").Set(aName, "x"))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("UpdateAffected = %d, %v; want ErrUnsupported", n, err)
	}
}

func TestUpdateAffected_KeepsUpdatesRefusalsAndNoOp(t *testing.T) {
	t.Parallel()
	if _, err := UpdateAffected(buildSchema(newConn()).DAO().Set(aName, "x")); !errors.Is(err, ErrNoConditions) {
		t.Errorf("no predicate: %v, want ErrNoConditions", err)
	}
	conn := newConn()
	n, err := UpdateAffected(buildSchema(conn).DAO().With(aID, "1"))
	if err != nil || n != 0 || conn.lastExec != "" {
		t.Errorf("empty value set: %d, %v, sent %q; want 0, nil and nothing sent", n, err, conn.lastExec)
	}
	conn = newConn()
	conn.execErr = errors.New("boom")
	if _, err := UpdateAffected(buildSchema(conn).DAO().With(aID, "1").Set(aName, "x")); err == nil {
		t.Error("an exec failure was swallowed")
	}
}

// A DAO that does not implement the capability is refused, not guessed at.
func TestUpdateAffected_ADAOWithoutTheCapabilityIsUnsupported(t *testing.T) {
	t.Parallel()
	type plain struct {
		DAO[*artist, artistField, string]
	}
	d := plain{buildSchema(newConn()).DAO().With(aID, "1").Set(aName, "x")}
	if _, err := UpdateAffected[*artist, artistField, string](d); !errors.Is(err, ErrUnsupported) {
		t.Errorf("UpdateAffected on a DAO without the method: %v, want ErrUnsupported", err)
	}
}
