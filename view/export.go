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

// Export executes the bound view on conn and writes each resulting document to dst.
// The file path for each document is evaluated by rendering the view's export template
// using that row's JSON data. Intermediate directories are created automatically via [vfs.FS.MkdirAll].
//
// Operational and Atomicity Guarantees:
//   - Document Atomicity: Each document is written using [vfs.FS.WriteFile], ensuring writes
//     are atomic and readers never observe partial files.
//   - Failure Semantics: If an error occurs on row N, export terminates immediately: rows 1 through N-1
//     remain written in dst, while row N is not committed. The total count of successfully written
//     documents is returned alongside the error.
//   - Path Traversal Containment: An empty export name produces an error. Any rendered path attempting
//     lexical escape outside dst (such as "../" or absolute paths) is rejected by dst's [vfs.CheckName].
//   - Storage Independence: dst can be any [vfs.FS] implementation, including local disk (vfs/local),
//     memory (vfs/memfs), or remote cloud storage drivers matching the view's [View.Destination] URI.
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
