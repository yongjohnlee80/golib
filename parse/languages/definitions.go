package languages

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/golang"
	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/lua"
	"github.com/yongjohnlee80/golib/parse/python"
	"github.com/yongjohnlee80/golib/parse/rust"
	"github.com/yongjohnlee80/golib/parse/shell"
	"github.com/yongjohnlee80/golib/parse/typescript"
	"github.com/yongjohnlee80/golib/parse/yaml"
)

// Definitions returns independent metadata for the standard source providers.
func Definitions() []highlight.Definition {
	return []highlight.Definition{golang.Definition(), rust.Definition(), js.Definition(), typescript.Definition(), python.Definition(), lua.Definition(), shell.Definition(), yaml.Definition()}
}
