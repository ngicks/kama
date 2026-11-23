package validator

import (
	"errors"
	"fmt"
	"go/types"
	"maps"
	"slices"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// typeOptions is the option tree entry of a type.
type typeOptions = genopt.TypeOption[TypeOptions, FieldOptions]

// explicitMark returns the source of //kama:validate on the type of to or
// its type group, or of its option file entry.
func explicitMark(to typeOptions) (optionSource, bool) {
	for _, o := range []TypeOptions{to.Type, to.Group} {
		if o.marked {
			return o.markSrc, true
		}
	}

	return optionSource{}, false
}

// ignoreMark returns the source of //kama:validate ignore on the type of to
// or its type group.
func ignoreMark(to typeOptions) (optionSource, bool) {
	for _, o := range []TypeOptions{to.Type, to.Group} {
		if o.Ignore {
			return o.markSrc, true
		}
	}

	return optionSource{}, false
}

func (s *selection) ignored(id typegraph.QualifiedType) bool {
	to, _ := s.opts.Loaded.Option.Type(id)
	_, ok := ignoreMark(to)

	return ok
}

// marked reports whether the user marked n: by //kama:validate on it, by
// //kama:validate file in its file or by //kama:validate package in its
// package, or by the option file counterparts.
func (s *selection) marked(n *typegraph.Node) bool {
	if s.ignored(n.Id) {
		return false
	}

	to, _ := s.opts.Loaded.Option.Type(n.Id)
	if _, ok := explicitMark(to); ok {
		return true
	}

	po := s.opts.Loaded.Option.Packages[n.Id.PackagePath]
	allTypes := opiter.WrapSeq(maps.Values(po.Files)).
		Any(func(p PackageOptions) bool { return p.AllTypes })

	return po.Package.AllTypes || allTypes || po.Files[n.Filename()].file
}

// hookOn reports whether Validate of n calls the hook of the field v: as
// the field says, else as the type says, else when n is marked.
func (s *selection) hookOn(n *typegraph.Node, v *types.Var) bool {
	if on := s.opts.Loaded.Option.Field(n.Id, v.Name()).Hook; on != nil {
		return *on
	}

	to, _ := s.opts.Loaded.Option.Type(n.Id)
	for _, o := range []TypeOptions{to.Type, to.Group} {
		if o.Hook != nil {
			return *o.Hook
		}
	}

	return s.marked(n)
}

// checkMarks reports types both marked and ignored, and types marked
// explicitly that cannot get a Validate: aliases, types of other kinds than
// struct, slice and map, and implementors.
func (s *selection) checkMarks(g *typegraph.Graph) error {
	var errs []error

	for id, to := range s.opts.Loaded.Option.Types() {
		mark, isMarked := explicitMark(to)
		ignore, isIgnored := ignoreMark(to)

		switch {
		case !isMarked:
			continue
		case isIgnored:
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s is both marked and ignored (%s)",
				ErrDirective, mark.origin(), id, ignore.origin(),
			))

			continue
		}

		err := base.MarkedTypeError(s.pkgs, s.opts.Markers, g, id, mark.origin(), ErrUnsupported)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if n, ok := g.Get(id); ok && s.implementor(n.Detail.Type) {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s declares Validate itself; remove the mark",
				ErrConflict, mark.origin(), id,
			))
		}
	}

	return errors.Join(errs...)
}

// checkFieldOptions reports hook and validator options on types that get
// no Validate, where they would have no effect, and validators given twice
// to one field.
func (s *selection) checkFieldOptions() error {
	var errs []error

	for id, to := range s.opts.Loaded.Option.Types() {
		isTarget := s.isTargetId(id)

		for _, o := range []TypeOptions{to.Type, to.Group} {
			if o.Hook != nil && !isTarget {
				errs = append(errs, fmt.Errorf(
					"%w: %s: %s on %s, which gets no Validate",
					ErrDirective, o.hookSrc.origin(), o.hookSrc.spelling(), id,
				))
			}
		}

		fields, _ := to.Fields.Unpack()
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			errs = append(errs, s.fieldOptionErrors(id, name, fields[name], isTarget)...)
		}
	}

	return errors.Join(errs...)
}

func (s *selection) fieldOptionErrors(
	id typegraph.QualifiedType,
	field string,
	o FieldOptions,
	isTarget bool,
) []error {
	var errs []error

	if o.Hook != nil && !isTarget {
		errs = append(errs, fmt.Errorf(
			"%w: %s: %s on field %s.%s, whose type gets no Validate",
			ErrDirective, o.hookSrc.origin(), o.hookSrc.spelling(), id, field,
		))
	}

	seen := map[string]bool{}

	for _, v := range o.Validators {
		switch {
		case !isTarget:
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on field %s.%s, whose type gets no Validate; mark the type",
				ErrDirective, v.src.origin(), v.src.spelling(), id, field,
			))
		case seen[v.Name]:
			errs = append(errs, fmt.Errorf(
				"%w: %s: validator %s given twice on field %s.%s",
				ErrDirective, v.src.origin(), v.Name, id, field,
			))
		}

		seen[v.Name] = true
	}

	return errs
}
