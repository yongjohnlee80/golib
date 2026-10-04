package view

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/vfs"
)

// ErrNoExport is returned by [Invocation.Export] for a view that declares no export file name.
var ErrNoExport = errors.New("view: the view declares no export")

// Export runs the bound view on conn and writes each row's document to dst, at the name the view's
// export template renders for that row; a name with directories creates them. It returns how many
// documents it wrote.
//
// Each document is written atomically by dst, and the first error stops the run: the documents
// already written stay, and the one in flight is never left half written. An empty export name is an
// error, and a name that would leave dst (an absolute path, "..") is refused by dst itself, so an
// entity id from the database cannot place a file outside it.
//
// dst is whatever filesystem the caller opened for the view's [View.Destination]: vfs/local for a
// file:// destination, another vfs driver for a bucket.
func (inv Invocation) Export(ctx context.Context, conn dao.DataConn, dst vfs.FS) (int, error) {
	v := inv.view
	if v.exportTo == nil {
		return 0, fmt.Errorf("%w: %s", ErrNoExport, v.name)
	}
	rows, err := inv.Query(ctx, conn)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	written := 0
	for {
		x, err := rows.Next()
		if err == io.EOF {
			return written, nil
		}
		if err != nil {
			return written, err
		}
		if err := v.exportRow(ctx, dst, x); err != nil {
			return written, fmt.Errorf("view %s: row %d: %w", v.name, rows.n, err)
		}
		written++
	}
}

func (v *View) exportRow(ctx context.Context, dst vfs.FS, x []byte) error {
	data, err := decode(x)
	if err != nil {
		return err
	}
	name, err := v.exportName(data)
	if err != nil {
		return fmt.Errorf("export name: %w", err)
	}
	if name == "" {
		return errors.New("export name is empty")
	}
	if dir := path.Dir(name); dir != "." {
		if err := dst.MkdirAll(ctx, dir); err != nil {
			return err
		}
	}
	rdr, err := v.Read(x)
	if err != nil {
		return err
	}
	defer rdr.Close()
	_, err = dst.WriteFile(ctx, name, rdr)
	return err
}
