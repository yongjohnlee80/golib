package partial

import "github.com/yongjohnlee80/golib/dao"

// ApplyOption configures ApplyRules.
type ApplyOption func(*applyConfig)

type applyConfig struct {
	rename func(string) string
}

// WithRename installs a field-name translation for entities whose dao field
// enums diverge from their wire names. The default is identity — the
// convention (dao ADR-0010 §2.6) is that an entity's field-enum values ARE its
// wire names, so the adapter is a type cast.
func WithRename(fn func(string) string) ApplyOption {
	return func(c *applyConfig) { c.rename = fn }
}

// ApplyRules stages the patch onto a DAO via dao ADR-0010's SetRules: canonical
// field names cast to the DAO's field enum C, kinds mapped 1:1. The returned
// error is the patch's (bind/mutator state) — check it before executing the
// verb. dao's SetRules is lenient on keys that don't resolve to writable
// fields and its Field declarations own clearability, so a patch may carry more
// than the entity writes without ceremony (ADR-0001 §2.9). This is the only
// file in the package that imports golib/dao.
func ApplyRules[R any, C ~string, ID any, T any](
	d dao.DAO[R, C, ID], p *Patch[T], opts ...ApplyOption,
) (dao.DAO[R, C, ID], error) {
	var cfg applyConfig
	for _, o := range opts {
		o(&cfg)
	}
	rules, err := p.Rules()
	if err != nil {
		return d, err
	}
	m := make(map[C]dao.Rule, len(rules))
	for name, r := range rules {
		if cfg.rename != nil {
			name = cfg.rename(name)
		}
		switch r.Kind {
		case RuleWrite:
			m[C(name)] = dao.Write(r.Value)
		case RuleClear:
			m[C(name)] = dao.Clear()
		}
	}
	return d.SetRules(m), nil
}
