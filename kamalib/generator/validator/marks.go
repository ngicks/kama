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
	to, _ := s.loadedOptions.Option.Type(id)
	_, ok := ignoreMark(to)

	return ok
}

// typeValidators returns the validators the directives or the option file
// entry of the type id name for the value of the type, those of its type
// group first.
func (s *selection) typeValidators(id typegraph.QualifiedType) []ValidatorOptions {
	to, _ := s.loadedOptions.Option.Type(id)
	return slices.Concat(to.Group.Validators, to.Type.Validators)
}

// marked reports whether the user marked n: by //kama:validate on it, by
// //kama:validate file in its file or by //kama:validate package in its
// package, or by the option file counterparts.
func (s *selection) marked(n *typegraph.Node) bool {
	if s.ignored(n.Id) {
		return false
	}

	to, _ := s.loadedOptions.Option.Type(n.Id)
	if _, ok := explicitMark(to); ok {
		return true
	}

	po := s.loadedOptions.Option.Packages[n.Id.PackagePath]
	allTypes := opiter.WrapSeq(maps.Values(po.Files)).
		Any(func(p PackageOptions) bool { return p.AllTypes })

	return po.Package.AllTypes || allTypes || po.Files[n.Filename()].file
}

// hookOn reports whether Validate of n calls the hook of the field v: as
// the field says, else as the type says, else when n is marked.
func (s *selection) hookOn(n *typegraph.Node, v *types.Var) bool {
	if on := s.loadedOptions.Option.Field(n.Id, v.Name()).Hook; on != nil {
		return *on
	}

	to, _ := s.loadedOptions.Option.Type(n.Id)
	for _, o := range []TypeOptions{to.Type, to.Group} {
		if o.Hook != nil {
			return *o.Hook
		}
	}

	return s.marked(n)
}

// checkMarks reports types both marked and ignored, and types marked
// explicitly that cannot get a Validate: aliases, types of other kinds than
// struct, slice and map without validators of their own, and implementors.
func (s *selection) checkMarks(g *typegraph.Graph) error {
	var errs []error

	for id, to := range s.loadedOptions.Option.Types() {
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

		n, ok := g.Get(id)
		if !ok || !s.validatesValue(n) {
			err := base.MarkedTypeError(
				s.pkgs,
				s.markers,
				g,
				id,
				mark.origin(),
				ErrUnsupported,
			)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		}

		if ok && s.implementor(n.Detail.Type) {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s declares Validate itself; remove the method and move its checks"+
					" into a function named by pre=\"<func>\" or post=\"<func>\" of //kama:validate"+
					" on the type, or into a field hook",
				ErrConflict, mark.origin(), id,
			))
		}
	}

	return errors.Join(errs...)
}

// checkFieldOptions reports hook and validator options on types that get
// no Validate, where they would have no effect, and validators given twice
// to one type or one field.
func (s *selection) checkFieldOptions() error {
	var errs []error

	for id, to := range s.loadedOptions.Option.Types() {
		isTarget := s.isTargetId(id)

		for _, o := range []TypeOptions{to.Type, to.Group} {
			if o.Hook != nil && !isTarget {
				errs = append(errs, fmt.Errorf(
					"%w: %s: %s on %s, which gets no Validate",
					ErrDirective, o.hookSrc.origin(), o.hookSrc.spelling(), id,
				))
			}
		}

		errs = append(errs, funcOptionErrors(id, to, isTarget)...)

		vs := s.typeValidators(id)
		if !isTarget {
			for _, v := range vs {
				errs = append(errs, fmt.Errorf(
					"%w: %s: %s on %s, which gets no Validate",
					ErrDirective, v.src.origin(), v.src.spelling(), id,
				))
			}
		} else {
			errs = append(errs, duplicateValidators(vs, id.String())...)
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

	if isTarget {
		return append(errs, duplicateValidators(o.Validators, "field "+id.String()+"."+field)...)
	}

	for _, v := range o.Validators {
		errs = append(errs, fmt.Errorf(
			"%w: %s: %s on field %s.%s, whose type gets no Validate; mark the type",
			ErrDirective, v.src.origin(), v.src.spelling(), id, field,
		))
	}

	return errs
}

// funcOptionErrors reports pre and post of the type id, whose options are
// to, on its type group or on a type that gets no Validate. A function
// takes the value of one type, so a type group cannot share it.
func funcOptionErrors(id typegraph.QualifiedType, to typeOptions, isTarget bool) []error {
	var errs []error

	for _, src := range []optionSource{to.Group.preSrc, to.Group.postSrc} {
		if src.arg != "" {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on a type group; write it in the doc comment of each type",
				ErrDirective, src.origin(), src.spelling(),
			))
		}
	}

	for _, src := range []optionSource{to.Type.preSrc, to.Type.postSrc} {
		if src.arg != "" && !isTarget {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on %s, which gets no Validate",
				ErrDirective, src.origin(), src.spelling(), id,
			))
		}
	}

	return errs
}

// duplicateValidators reports the validators of vs named twice. place
// names what vs apply to.
func duplicateValidators(vs []ValidatorOptions, place string) []error {
	var (
		errs []error
		seen = map[string]bool{}
	)

	for _, v := range vs {
		key := v.src.arg
		if v.Item.Name != "" {
			key = v.Item.Name + "="
		}

		if seen[key] {
			errs = append(errs, fmt.Errorf(
				"%w: %s: validator %s given twice on %s",
				ErrDirective, v.src.origin(), key, place,
			))
		}

		seen[key] = true
	}

	return errs
}
