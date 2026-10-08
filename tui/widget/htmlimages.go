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

// ImageResolver opens an image an HTMLView's page names (an img's src) for a layout that draws
// images. It is the application's: the view loads nothing on its own, so a page cannot reach a
// file, or the network, the application did not hand it. data: URIs never reach it; a layout
// decodes them itself.
type ImageResolver func(ctx context.Context, src string) (io.ReadCloser, error)

// ErrImageRefused is an image source a resolver will not open.
var ErrImageRefused = errors.New("widget: image source refused")

// DirImages resolves relative paths under root, the folder of the page's source: "img/a.png",
// "./a.png". It refuses an absolute path, a URL of any scheme (file:, http:), a path that climbs
// out of root (".."), and a symlink whose target leaves root, with ErrImageRefused. A query or a
// fragment on the path is ignored. A symlink is followed only by a relative target that stays
// inside root; an absolute target is refused even when it points inside, as os.Root refuses it.
func DirImages(root string) ImageResolver {
	return func(ctx context.Context, src string) (io.ReadCloser, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		u, err := url.Parse(strings.TrimSpace(src))
		if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" {
			return nil, ErrImageRefused
		}
		// a valid fs path is relative, never climbs ("..") and is not the root itself
		p := path.Clean(u.Path)
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
