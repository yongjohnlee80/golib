package widget

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"strings"
)

// ImageResolver opens a resource an HTMLView's page names, an image's src or a linked
// stylesheet's href, for a layout that loads them. It is the application's: the view loads
// nothing on its own, so a page cannot reach a file, or the network, the application did not hand
// it. data: URIs never reach it; a layout decodes them itself.
type ImageResolver func(ctx context.Context, src string) (io.ReadCloser, error)

// ErrImageRefused is a resource a resolver will not open: outside its root, absolute, or a URL.
var ErrImageRefused = errors.New("widget: resource refused")

// DirImages resolves relative paths under root: DirResources(root, "").
func DirImages(root string) ImageResolver { return DirResources(root, "") }

// DirResources resolves a page's relative paths against base, the page's folder as a slash path
// under root ("" is root itself), and opens them inside root: from base "Aesop",
// "../assets/a.svg" is root's assets/a.svg and "img/a.png" Aesop/img/a.png. It refuses an
// absolute path, a URL of any scheme (file:, http:) or host, a path that climbs out of root, and a
// symlink whose target leaves root, with ErrImageRefused; a base that is not a folder under root
// refuses everything. A query or a fragment on the path is ignored. A symlink is followed only by a
// relative target that stays inside root; an absolute target is refused even when it points
// inside, as os.Root refuses it.
func DirResources(root, base string) ImageResolver {
	base = path.Clean(strings.TrimSpace(base))
	if base == "." {
		base = ""
	}
	badBase := base != "" && !fs.ValidPath(base) // absolute, or climbing out of root
	return func(ctx context.Context, src string) (io.ReadCloser, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if badBase {
			return nil, ErrImageRefused
		}
		u, err := url.Parse(strings.TrimSpace(src))
		if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" || strings.HasPrefix(u.Path, "/") {
			return nil, ErrImageRefused
		}
		// a valid fs path is relative, never climbs ("..") and is not the root itself
		p := path.Clean(path.Join(base, u.Path))
		if p == "." || !fs.ValidPath(p) {
			return nil, ErrImageRefused
		}
		// os.Root refuses any name, a symlink included, that resolves outside root
		r, err := os.OpenRoot(root)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		f, err := r.Open(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			return nil, errors.Join(ErrImageRefused, err)
		}
		if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
			_ = f.Close()
			return nil, ErrImageRefused
		}
		return f, nil
	}
}
