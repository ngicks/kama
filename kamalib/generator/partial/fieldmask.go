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

// checkFieldMaskTargets reports //kama:partial:fieldmask on types without
// struct partial variants and on fields of types without field masks, of
// blank fields and of embedded partial values. targets are the targets
// keyed by id.
func (s *selection) checkFieldMaskTargets(
	targets map[typegraph.QualifiedType]*typegraph.Node,
) error {
	var errs []error

	for id, to := range s.opts.Loaded.Option.Types() {
		src, ok := maskMark(to)
		if !ok {
			continue
		}

		n := targets[id]
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

	for k, o := range s.allFieldOptions() {
		if o.FieldMask == nil {
			continue
		}

		src := o.maskSrc
		id := typegraph.NewQualifiedType(k.pkgPath, k.typeName)
		bad := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on field %s; "+format,
				slices.Concat([]any{ErrDirective, src.origin(), src.spelling(), k}, args)...,
			))
		}

		n := targets[id]
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
				func(v *types.Var) bool {
					return v.Name() == k.field && s.embeddedPartial(n.Detail.Type, v) != nil
				},
			)

		if embedsPartial {
			bad("the paths of the fields of an embedded partial value are those of " +
				"the parent, so the field has no path segment of its own")
		}
	}

	return errors.Join(errs...)
}

// embeddedPartial returns the origin of the target the field v of the
// target n embeds as an embedded partial value, or nil. It returns nil for
// an implementor.
func (s *selection) embeddedPartial(n *types.Named, v *types.Var) *types.Named {
	if !v.Embedded() {
		return nil
	}

	named, ok := s.valueChild(n, deref(v.Type()))
	if !ok || !isStruct(named) {
		return nil
	}

	return named
}

// maskSegment returns the path segment of the field v of the target id:
// its name, or the name its field mask option gives.
func (s *selection) maskSegment(id typegraph.QualifiedType, v *types.Var) string {
	if o, ok := s.maskNameOf(fieldKeyOf(id, v.Name())); ok {
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
	n *types.Named,
	fn func(owner *types.Named, v *types.Var, seg string) error,
) error {
	id := typegraph.QualifiedTypeFromObject(n.Obj())
	st := n.Underlying().(*types.Struct)

	for v := range st.Fields() {
		if v.Name() == "_" {
			continue
		}

		if m := s.matchField(n, v); m.embedded && m.implementor {
			return fmt.Errorf(
				"%w: %s (%s): embedded %s is declared outside the packages of the run, "+
					"and the paths of its fields cannot be paths of %s",
				ErrUnsupported, id.String(), s.position(n, v.Pos()),
				types.TypeString(m.named, nil), id.TypeName,
			)
		}

		child := s.embeddedPartial(n, v)
		if child == nil {
			if err := fn(n, v, s.maskSegment(id, v)); err != nil {
				return err
			}

			continue
		}

		childId := typegraph.QualifiedTypeFromObject(child.Obj())
		if !s.fieldMasked(childId) {
			return fmt.Errorf(
				"%w: %s (%s): embedded %s does not have field masks enabled, "+
					"and the paths of its fields are paths of %s",
				ErrDirective, id.String(), s.position(n, v.Pos()),
				childId.String(), id.TypeName,
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
// partial value of a target with field masks. sorted are the targets
// ordered by id.
func (s *selection) checkFieldMaskSegments(sorted []*typegraph.Node) error {
	var errs []error

	for _, n := range sorted {
		if !s.fieldMasked(n.Id) {
			if err := s.checkUnmaskedEmbedding(n); err != nil {
				errs = append(errs, err)
			}

			continue
		}

		var (
			fields = map[string]*types.Var{}
			owners = map[string]*types.Named{}
			shared error
		)

		err := s.walkMaskSegments(
			n.Detail.Type,
			func(owner *types.Named, v *types.Var, seg string) error {
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
						s.position(owner, v.Pos()),
						seg,
						owner.Obj().Name(),
						v.Name(),
						owners[seg].Obj().Name(),
						prev.Name(),
						s.position(owners[seg], prev.Pos()),
					)
				}

				return nil
			},
		)
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
		m := s.matchField(n.Detail.Type, v)
		if m.embedded && m.implementor && declaresFieldMask(m.named) {
			return fmt.Errorf(
				"%w: %s (%s): embedded %s is declared outside the packages of the run and "+
					"has field masks, whose methods %s would promote without its own fields",
				ErrUnsupported, n.Id.String(), n.Detail.Pkg.Fset.Position(v.Pos()),
				types.TypeString(m.named, nil), partialName(n.Id.TypeName),
			)
		}

		child := s.embeddedPartial(n.Detail.Type, v)
		if child == nil {
			return nil
		}

		childId := typegraph.QualifiedTypeFromObject(child.Obj())
		if !s.fieldMasked(childId) {
			return nil
		}

		return fmt.Errorf(
			"%w: %s (%s): embedded %s has field masks enabled, "+
				"so %s needs //kama:partial:fieldmask too",
			ErrDirective, n.Id.String(), n.Detail.Pkg.Fset.Position(v.Pos()),
			childId.String(), n.Id.TypeName,
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
