package kit

import "strings"

// WithArg fills a message's "%1" with a, as Qt's QString::arg does. A message is translated
// whole and then filled, so no language ever sees its sentence assembled from fragments.
func WithArg(text, a string) string { return strings.ReplaceAll(text, "%1", a) }
