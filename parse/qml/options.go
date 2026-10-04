package qml

// Option configures a [QML] parser built by [New].
type Option func(*QML)

// New returns a QML parser configured by opts.
//
//	spec, err := qml.New(qml.WithName("QuitDialog.qml")).Parse(src)
func New(opts ...Option) QML {
	var q QML
	for _, o := range opts {
		if o != nil {
			o(&q)
		}
	}
	return q
}

// MaxDepth bounds node nesting. Without it, or with n <= 0, the parser applies
// [DefaultQMLMaxDepth].
func MaxDepth(n int) Option { return func(q *QML) { q.MaxDepth = n } }

// WithName names the source in every position of the tree and its errors, as in
// "QuitDialog.qml:3:5".
func WithName(file string) Option { return func(q *QML) { q.File = file } }
