package core

import (
	"context"
	"io"
)

// ImageResolver opens a resource an HTMLView's page names, an image's src or a linked
// stylesheet's href, for a layout that loads them. It is the application's: the view loads
// nothing on its own, so a page cannot reach a file, or the network, the application did not hand
// it. data: URIs never reach it; a layout decodes them itself.
type ImageResolver func(ctx context.Context, src string) (io.ReadCloser, error)
