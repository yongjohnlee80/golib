package dao

import (
	"context"
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// A dialect without the capabilities: WithRowLock fails the DAO with ErrUnsupported, KeysIn and
// DistanceAs fail their statements with it, and nothing is sent (dao-0025's PostgreSQL-only
// rule). PostgreSQL's renderings and refusals are dao/postgres/typedann_test.go's.
func TestTypedANNCapabilitiesRefuseOnOtherDialects(t *testing.T) {
	ctx := context.Background()
	c := newLS()
	c.d = returningDialect{}
	s := buildSchema(c, SortParam[*artist, artistField, artistSort, string]("near",
		DistanceAs(C("vec"), Cosine, VectorType{Kind: VectorFloat32, Dims: 4})))
	if _, err := s.On(Begin(ctx), WithRowLock(LockForUpdate)).Select(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("WithRowLock: %v, want ErrUnsupported", err)
	}
	keys := KeysIn([]KeyColumn{{Col: "artist.id", Elem: ArrayText}}, [][]any{{"a"}})
	if _, err := s.DAO().WithPredicate(keys).Select(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("KeysIn: %v, want ErrUnsupported", err)
	}
	if _, err := s.DAO().OrderBy(AscBy(artistSort("near"), []float32{1})).Select(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("DistanceAs: %v, want ErrUnsupported", err)
	}
	if len(c.log) != 0 {
		t.Errorf("sent %q, want nothing", c.log)
	}
	if !errors.Is(ErrRowLockNeedsTx, errs.ErrPrecondition) {
		t.Error("ErrRowLockNeedsTx does not wrap errs.ErrPrecondition")
	}
	for _, bad := range []func(){
		func() { DistanceAs(C("vec"), Cosine, VectorType{Kind: VectorFloat32, Dims: 0}) },
		func() { DistanceAs(C("vec"), Cosine, VectorType{Kind: 9, Dims: 4}) },
		func() { DistanceAs(C("vec"), Metric(9), VectorType{Kind: VectorFloat32, Dims: 4}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("a bad DistanceAs declaration did not panic")
				}
			}()
			bad()
		}()
	}
}
