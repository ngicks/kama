package jsonschema

import (
	"slices"
	"strings"

	"github.com/ngicks/opiter"
	"github.com/ngicks/option/opt"

	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
)

// The functions of this file resolve $dynamicRef at generation time. The
// generated validators carry no dynamic scope, so the generator follows
// the schema resources evaluation enters along each path the validators of
// a type take, and resolves each $dynamicRef on that path. A type of
// another schema named on that path validates in the scope of its own
// place in the document, which scopeKeeps compares with the scope of the
// path.

// dynamicTarget returns the pointer of the schema the $dynamicRef of the
// schema at ptr names in scope, the pointers of the schema resources
// evaluation entered, outermost first. initial is the target the
// reference names resolved as a $ref is. When the fragment of the
// reference is a plain name that a $dynamicAnchor of initial declares, the
// outermost resource of scope declaring that $dynamicAnchor holds the
// target.
func dynamicTarget(doc *jsonschema.Schema, ptr, initial string, scope []string) (string, error) {
	s, err := at(doc, ptr)
	if err != nil {
		return "", err
	}

	name := dynamicName(s.DynamicRef.Value())
	if name == "" {
		return initial, nil
	}

	t, err := at(doc, initial)
	if err != nil || t.DynamicAnchor != opt.Some(name) {
		return initial, err
	}

	if found := outermost(doc, scope, name); found.IsSome() {
		return normalPointer(found.Value())
	}

	return initial, nil
}

// dynamicName returns the plain name the fragment of the reference ref
// gives, which a $dynamicAnchor may declare, or "" for none.
func dynamicName(ref string) string {
	if _, frag, _ := strings.Cut(ref, "#"); !strings.HasPrefix(frag, "/") {
		return frag
	}

	return ""
}

// lexicalScope returns the pointers of the schema resources holding the
// schema at ptr, the root first. The validators of the type of a schema
// resolve a $dynamicRef in that scope.
func lexicalScope(doc *jsonschema.Schema, ptr string) []string {
	along, _ := jsonschema.ResourcesAlong(doc, ptr)

	return along
}

// enter returns scope with the schema resource holding the schema at ptr
// appended, unless scope holds it already: the outermost place of a
// resource decides where a $dynamicRef resolves.
func enter(doc *jsonschema.Schema, scope []string, ptr string) []string {
	along := lexicalScope(doc, ptr)
	if len(along) == 0 || slices.Contains(scope, along[len(along)-1]) {
		return scope
	}

	return append(slices.Clip(scope), along[len(along)-1])
}

// scopeKeeps reports whether naming the type of the schema at target from
// scope resolves every $dynamicRef as entering target from scope does: for
// each name a $dynamicRef of doc gives, the outermost resource declaring
// it is the same in scope with the resource of target entered and in the
// lexical scope of target, where the validators of the type resolve.
func scopeKeeps(doc *jsonschema.Schema, scope []string, target string) bool {
	entered := enter(doc, scope, target)
	own := lexicalScope(doc, target)

	// Equal scopes resolve alike, which spares walking doc for the names
	// on every reference within one resource.
	if slices.Equal(entered, own) {
		return true
	}

	return opiter.SlicesValues(dynamicNames(doc)).
		Every(func(name string) bool {
			return outermost(doc, entered, name) == outermost(doc, own, name)
		})
}

// refAloneAt reports whether the schema s at ptr is a $ref alone, as
// refAlone tells, that takes the type of its target: the dynamic scope of
// the schema keeps the target resolving as its type does.
func refAloneAt(doc *jsonschema.Schema, ptr string, s *jsonschema.Schema) bool {
	if !refAlone(s) {
		return false
	}

	target, err := refTarget(doc, ptr)

	return err != nil || scopeKeeps(doc, lexicalScope(doc, ptr), target)
}

// dynamicNames returns the plain names the fragments of the $dynamicRef of
// the schemas of doc give.
func dynamicNames(doc *jsonschema.Schema) []string {
	return slices.Compact(
		opiter.WrapSeq2(schemas(doc)).
			OmitF().
			Filter(func(s *jsonschema.Schema) bool { return s != nil && s.DynamicRef.IsSome() }).
			Map(func(s *jsonschema.Schema) string { return dynamicName(s.DynamicRef.Value()) }).
			Filter(func(n string) bool { return n != "" }).
			Collect(slices.Sorted),
	)
}

// outermost returns the pointer of the schema declaring the $dynamicAnchor
// name in the outermost resource of scope declaring it.
func outermost(doc *jsonschema.Schema, scope []string, name string) opt.Option[string] {
	return opiter.SlicesValues(scope).
		Map(func(res string) opt.Option[string] {
			return opt.FromOk(jsonschema.DynamicAnchor(doc, res, name))
		}).
		Filter(opt.Option[string].IsSome).
		First().
		UnwrapOr(opt.None[string]())
}
