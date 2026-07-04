package partial

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// plan maps canonical effective JSON names to the fields of a model type T.
// One plan is built per T and cached (ADR-0001 §2.4); it is the single
// source of the name space shared by presence tracking, mutation, rules, and
// the typed decode.
type plan struct {
	byName map[string]planField // canonical name -> field
	lower  map[string]string    // lower(name) -> canonical (case-insensitive fallback)
	order  []string             // canonical names in stable (sorted) order
}

type planField struct {
	name string // effective JSON name: tag name, else Go field name
	typ  reflect.Type
}

// canonical resolves a wire key to its canonical field name: an exact match
// first, then the case-insensitive fallback encoding/json applies. The
// second return is false for a key that names no field of T.
func (p *plan) canonical(key string) (string, bool) {
	if _, ok := p.byName[key]; ok {
		return key, true
	}
	if name, ok := p.lower[strings.ToLower(key)]; ok {
		return name, true
	}
	return "", false
}

var plans sync.Map // reflect.Type -> *plan

// planFor returns the cached plan for T, building it once. It panics (loud,
// first-use) on a model that cannot be a partial payload: an anonymous
// pointer embed (silent-presence cliff) or a field whose canonical name is
// the reserved ClearKey (ADR-0001 §2.4).
func planFor[T any]() *plan {
	var zero T
	typ := reflect.TypeOf(zero)
	if typ == nil { // T is an interface type with a nil zero value
		panic("partial: model type must be a struct, got nil interface")
	}
	if cached, ok := plans.Load(typ); ok {
		return cached.(*plan)
	}
	rt := typ
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		panic(fmt.Sprintf("partial: model type %s must be a struct", typ))
	}
	pl := &plan{byName: map[string]planField{}, lower: map[string]string{}}
	collectFields(rt, pl)
	pl.order = make([]string, 0, len(pl.byName))
	for n := range pl.byName {
		pl.order = append(pl.order, n)
	}
	sort.Strings(pl.order)
	actual, _ := plans.LoadOrStore(typ, pl)
	return actual.(*plan)
}

// collectFields walks rt, honoring JSON naming and flattening anonymous value
// embeds (shallowest wins; deeper same-name fields do not override, matching
// encoding/json). It records the field on the plan.
func collectFields(rt reflect.Type, pl *plan) {
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Anonymous {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				panic(fmt.Sprintf(
					"partial: model %s has an anonymous pointer embed %s; use a value embed",
					rt, f.Name))
			}
			if ft.Kind() == reflect.Struct {
				collectFields(ft, pl)
				continue
			}
		}
		if f.PkgPath != "" { // unexported
			continue
		}
		name, ok := effectiveName(f)
		if !ok {
			continue // json:"-"
		}
		if name == ClearKey {
			panic(fmt.Sprintf(
				"partial: model %s field %s has reserved JSON name %q", rt, f.Name, ClearKey))
		}
		if _, exists := pl.byName[name]; exists {
			continue // shallower field already claimed this name
		}
		pl.byName[name] = planField{name: name, typ: f.Type}
		pl.lower[strings.ToLower(name)] = name
	}
}

// effectiveName returns f's JSON name (tag name, else the Go field name). The
// second return is false for json:"-" (excluded). Tag options (omitempty,
// string) are irrelevant to naming and ignored.
func effectiveName(f reflect.StructField) (string, bool) {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return f.Name, true
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		// json:"-" excludes; json:"-," names the field literally "-".
		if tag == "-" {
			return "", false
		}
		return "-", true
	}
	if name == "" {
		return f.Name, true
	}
	return name, true
}
