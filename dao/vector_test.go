package dao

import (
	"errors"
	"reflect"
	"testing"
)

type vectorDialect struct{ returningDialect }

func (vectorDialect) VectorDistance(col string, m Metric, ph string) string {
	return col + " <" + m.String() + "> " + ph
}

func TestDistance_BindsTheQueryVector(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{d: vectorDialect{}}
	_, err := buildSchema(conn, SortParam[*artist, artistField, artistSort, string]("near", Distance(C("emb"), L2))).DAO().
		With(aName, "x").OrderBy(AscBy("near", "vec")).Limit(3).Select(aID)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT artist.id FROM "artist" WHERE artist.name = $1 ORDER BY "emb" <L2> $2 ASC LIMIT $3`; conn.lastQuery != want {
		t.Errorf("sql = %s", conn.lastQuery)
	}
	if !reflect.DeepEqual(conn.lastArgs, []any{"x", "vec", int64(3)}) {
		t.Errorf("args = %v", conn.lastArgs)
	}
}

func TestDistance_Refusals(t *testing.T) {
	t.Parallel()
	mustFatal(t, "a zero column", func() { Distance(Expr{}, Cosine) })
	mustFatal(t, "an unknown metric", func() { Distance(C("emb"), Metric(9)) })
	conn := newConn()
	s := buildSchema(conn, SortParam[*artist, artistField, artistSort, string]("near", Distance(C("emb"), Cosine)))
	if _, err := s.DAO().OrderBy(AscBy("near", "v")).Select(aID); !errors.Is(err, ErrUnsupported) {
		t.Errorf("no vectors: %v, want ErrUnsupported", err)
	}
	if conn.lastQuery != "" {
		t.Errorf("sent %s", conn.lastQuery)
	}
	if Metric(9).String() != "Metric(9)" || InnerProduct.String() != "inner product" {
		t.Error("metric names")
	}
}
