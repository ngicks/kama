package partial

import (
	"errors"
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// fieldMaskMethodNames are the methods a partial type with field masks
// gets.
var fieldMaskMethodNames = []string{"FieldMask", "FromValueWithMask"}

// maskName is the field mask name of a field.
type maskName struct {
	name string
	src  optionSource
}

// maskNameFromArgs validates the args of //kama:partial:fieldmask on a
// field, keyed by name.
func maskNameFromArgs(kv map[string]string) (maskName, error) {
	k := opiter.Min(
		opiter.WrapSeq(maps.Keys(kv)).
			Filter(func(key string) bool { return key != "name" }),
	)

	if k.IsSome() {
		return maskName{}, fmt.Errorf("unknown key %q; want name", k.Value())
	}

	o := maskName{name: kv["name"]}
	switch {
	case o.name == "":
		return maskName{}, errors.New("name is required")
	case strings.Contains(o.name, "."):
		return maskName{}, fmt.Errorf("name %q; want a path segment without a dot", o.name)
	}

	return o, nil
}

// fieldMasked reports whether the target id has field masks enabled.
func (s *selection) fieldMasked(id typegraph.QualifiedType) bool {
	_, ok := s.masked[id]
	return ok
}

// checkFieldMaskTargets reports //kama:partial:fieldmask on types without
// struct partial variants and on fields of types without field masks, of
// blank fields and of embedded partial values.
func (s *selection) checkFieldMaskTargets() error {
	var errs []error

	ids := slices.SortedFunc(maps.Keys(s.masked), typegraph.QualifiedType.Compare)
	for _, id := range ids {
		src := s.masked[id]

		n := s.targets[id]
		switch {
		case n == nil:
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on type %s.%s; %s has no partial variant",
				ErrDirective,
				src.origin(),
				src.spelling(),
				id.PackagePath,
				id.TypeName,
				id.TypeName,
			))
		case !isStruct(n.Detail.Type):
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on type %s.%s; field masks apply to struct targets only",
				ErrDirective, src.origin(), src.spelling(), id.PackagePath, id.TypeName,
			))
		}
	}

	for _, k := range slices.SortedFunc(maps.Keys(s.maskNames), fieldKey.compare) {
		src := s.maskNames[k].src
		id := typegraph.NewQualifiedType(k.pkgPath, k.typeName)
		bad := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on field %s; "+format,
				slices.Concat([]any{ErrDirective, src.origin(), src.spelling(), k}, args)...,
			))
		}

		n := s.targets[id]
		switch {
		case n == nil:
			bad("%s has no partial variant", k.typeName)
			continue
		case !s.fieldMasked(id):
			bad("%s does not have field masks enabled", k.typeName)
			continue
		case k.field == "_":
			bad("a blank field has no path")
			continue
		}

		st, ok := n.Detail.Type.Underlying().(*types.Struct)
		if !ok {
			continue
		}
		// Field names other than _ are unique, so at most one field matches.
		embedsPartial := opiter.WrapSeq(st.Fields()).
			ContainsFunc(
				func(v *types.Var) bool { return v.Name() == k.field && s.embeddedPartial(n, v) != nil },
			)

		if embedsPartial {
			bad("the paths of the fields of an embedded partial value are those of " +
				"the parent, so the field has no path segment of its own")
		}
	}

	return errors.Join(errs...)
}

// embeddedPartial returns the target the field v of the target n embeds as
// an embedded partial value, or nil. It returns nil for an implementor.
func (s *selection) embeddedPartial(n *typegraph.Node, v *types.Var) *typegraph.Node {
	if !v.Embedded() {
		return nil
	}

	named, ok := s.partialOf(deref(v.Type()), n.Id.PackagePath)
	if !ok || !isStruct(named) {
		return nil
	}

	return s.node(named)
}

// maskSegment returns the path segment of the field v of the target n: its
// name, or the name its field mask option gives.
func (s *selection) maskSegment(n *typegraph.Node, v *types.Var) string {
	if o, ok := s.maskNames[fieldKeyOf(n.Id, v.Name())]; ok {
		return o.name
	}

	return v.Name()
}

// walkMaskSegments calls fn for every path segment of the struct target n
// in field order, with the field v giving the segment seg and the target
// owner declaring it. The segments of an embedded partial value take the
// place of the embedding field, so owner is n or a target embedded in it.
// An embedded partial value whose type does not have field masks enabled
// is an error, and so is an embedded implementor.
func (s *selection) walkMaskSegments(
	n *typegraph.Node,
	fn func(owner *typegraph.Node, v *types.Var, seg string) error,
) error {
	st := n.Detail.Type.Underlying().(*types.Struct)

	for v := range st.Fields() {
		if v.Name() == "_" {
			continue
		}

		if m := s.matchField(n, v); m.embedded && m.implementor {
			return fmt.Errorf(
				"%w: %s (%s): embedded %s is declared outside the packages of the run, "+
					"and the paths of its fields cannot be paths of %s",
				ErrUnsupported, n.Id.String(), n.Detail.Pkg.Fset.Position(v.Pos()),
				types.TypeString(m.named, nil), n.Id.TypeName,
			)
		}

		child := s.embeddedPartial(n, v)
		if child == nil {
			if err := fn(n, v, s.maskSegment(n, v)); err != nil {
				return err
			}

			continue
		}

		if !s.fieldMasked(child.Id) {
			return fmt.Errorf(
				"%w: %s (%s): embedded %s does not have field masks enabled, "+
					"and the paths of its fields are paths of %s",
				ErrDirective, n.Id.String(), n.Detail.Pkg.Fset.Position(v.Pos()),
				child.Id.String(), n.Id.TypeName,
			)
		}

		if err := s.walkMaskSegments(child, fn); err != nil {
			return err
		}
	}

	return nil
}

// checkFieldMaskSegments reports two fields of one struct target with field
// masks, the fields of embedded partial values included, that share a
// segment. It also reports a struct target without field masks embedding a
// partial value of a target with field masks.
func (s *selection) checkFieldMaskSegments() error {
	var errs []error

	for _, n := range s.sorted() {
		if !s.fieldMasked(n.Id) {
			if err := s.checkUnmaskedEmbedding(n); err != nil {
				errs = append(errs, err)
			}

			continue
		}

		var (
			fields = map[string]*types.Var{}
			owners = map[string]*typegraph.Node{}
			shared error
		)

		position := func(owner *typegraph.Node, v *types.Var) string {
			return owner.Detail.Pkg.Fset.Position(v.Pos()).String()
		}

		err := s.walkMaskSegments(n, func(owner *typegraph.Node, v *types.Var, seg string) error {
			prev, ok := fields[seg]
			if !ok {
				fields[seg], owners[seg] = v, owner
				return nil
			}
			// Only the first shared segment of a target is reported.
			if shared == nil {
				shared = fmt.Errorf(
					"%w: %s (%s): field mask path segment %q of field %s.%s is taken by field %s.%s (%s)",
					ErrDirective,
					n.Id.String(),
					position(owner, v),
					seg,
					owner.Id.TypeName,
					v.Name(),
					owners[seg].Id.TypeName,
					prev.Name(),
					position(owners[seg], prev),
				)
			}

			return nil
		})
		if err == nil {
			err = shared
		}

		if err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// checkUnmaskedEmbedding reports each embedded partial value of the target
// n whose type has field masks. The caller passes a target without field
// masks. Go promotes FieldMask and FromValueWithMask of the embedded
// partial value to the partial type of n. The promoted methods ignore the
// fields of n.
func (s *selection) checkUnmaskedEmbedding(n *typegraph.Node) error {
	st, ok := n.Detail.Type.Underlying().(*types.Struct)
	if !ok {
		return nil
	}

	maskedEmbeddingErr := func(v *types.Var) error {
		// The run knows no option of an implementor, so the methods of its
		// partial variant tell whether it has field masks.
		m := s.matchField(n, v)
		if m.embedded && m.implementor && declaresFieldMask(m.named) {
			return fmt.Errorf(
				"%w: %s (%s): embedded %s is declared outside the packages of the run and "+
					"has field masks, whose methods %s would promote without its own fields",
				ErrUnsupported, n.Id.String(), n.Detail.Pkg.Fset.Position(v.Pos()),
				types.TypeString(m.named, nil), partialName(n.Id.TypeName),
			)
		}

		child := s.embeddedPartial(n, v)
		if child == nil || !s.fieldMasked(child.Id) {
			return nil
		}

		return fmt.Errorf(
			"%w: %s (%s): embedded %s has field masks enabled, "+
				"so %s needs //kama:partial:fieldmask too",
			ErrDirective, n.Id.String(), n.Detail.Pkg.Fset.Position(v.Pos()),
			child.Id.String(), n.Id.TypeName,
		)
	}

	// errors.Join drops the nil errors of fields that are fine.
	return errors.Join(
		opiter.WrapSeq(st.Fields()).
			Map(maskedEmbeddingErr).
			CollectSlice()...,
	)
}

// declaresFieldMask reports whether the partial variant of the implementor
// named, as its package declares it, has the FieldMask method.
func declaresFieldMask(named *types.Named) bool {
	obj := named.Obj().Pkg().Scope().Lookup(partialName(named.Obj().Name()))
	if obj == nil {
		return false
	}

	m, _, _ := types.LookupFieldOrMethod(obj.Type(), false, obj.Pkg(), "FieldMask")
	_, ok := m.(*types.Func)

	return ok
}
