# parse/python

Install: `go get github.com/yongjohnlee80/golib/parse/python`.

`python.Definition().NewSource(nil)` supplies Python lexical highlighting,
triple-quoted/raw literals, and colon-suite/bracket indentation. The default unit
is four spaces; comments and string contents do not open suites.

Register the definition for `*.py`/`*.pyw` and `python`/`py` fences. The provider
tolerates incomplete editing text; it is not a Python interpreter or formatter.
See [languages](../languages/README.md) and [LICENSE](../../LICENSE).
