package postgres_test

import (
	"context"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/dao/postgres"
)

// External-package compile witness: PinnedConn keeps exactly the
// v0.5.6 method set, so an implementation outside this package compiles
// unchanged. If a method is ever added to PinnedConn, this file stops compiling
// — which is the point: that change is not PATCH-additive.
type externalPinned struct{}

func (externalPinned) Send(context.Context, postgres.ExtendedOp) error { return nil }
func (externalPinned) Flush(context.Context) error                     { return nil }
func (externalPinned) Receive(context.Context) (postgres.ExtendedMessage, error) {
	return postgres.ExtendedMessage{}, nil
}
func (externalPinned) Sync(context.Context) (byte, error) { return 'I', nil }
func (externalPinned) BeginSessionTx(context.Context, dao.TxOptions) (dao.ContextTxConn, error) {
	return nil, nil
}
func (externalPinned) Release(context.Context) error { return nil }
func (externalPinned) Discard()                      {}

var _ postgres.PinnedConn = externalPinned{}

// And the capability is a separate, optional interface.
var _ postgres.ParameterStatusReporter = (*struct {
	postgres.ParameterStatusReporter
})(nil)
