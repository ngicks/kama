package partial

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// typeOptions is the option tree entry of a type.
type typeOptions = genopt.TypeOption[TypeOptions, FieldOptions]

// fieldOption returns the options of the field k.
func (s *selection) fieldOption(k fieldKey) FieldOptions {
	return s.loaded.Option.Field(typegraph.NewQualifiedType(k.pkgPath, k.typeName), k.field)
}

// explicitMark returns the source of //kama:partial on the type group of to
// or its type, or of its option file entry.
func explicitMark(to typeOptions) (optionSource, bool) {
	for _, o := range []TypeOptions{to.Group, to.Type} {
		if o.marked {
			return o.markSrc, true
		}
	}

	return optionSource{}, false
}

// ignoreMark returns the source of //kama:partial ignore on the type group
// of to or its type.
func ignoreMark(to typeOptions) (optionSource, bool) {
	for _, o := range []TypeOptions{to.Group, to.Type} {
		if o.Ignore {
			return o.markSrc, true
		}
	}

	return optionSource{}, false
}

// maskMark returns the source of //kama:partial:fieldmask on the type group
// of to or its type.
func maskMark(to typeOptions) (optionSource, bool) {
	for _, o := range []TypeOptions{to.Group, to.Type} {
		if o.FieldMask {
			return o.maskSrc, true
		}
	}

	return optionSource{}, false
}

func (s *selection) ignored(id typegraph.QualifiedType) bool {
	to, _ := s.loaded.Option.Type(id)
	_, ok := ignoreMark(to)

	return ok
}

// marked reports whether the user marked n: by //kama:partial on it, by
// //kama:partial file in its file or by //kama:partial package in its
// package, or by the option file counterparts.
func (s *selection) marked(n *typegraph.Node) bool {
	if s.ignored(n.Id) {
		return false
	}

	to, _ := s.loaded.Option.Type(n.Id)
	if _, ok := explicitMark(to); ok {
		return true
	}

	po := s.loaded.Option.Packages[n.Id.PackagePath]
	allTypes := opiter.MapsValues(po.Files).
		Any(func(p PackageOptions) bool { return p.AllTypes })

	return po.Package.AllTypes || allTypes || po.Files[n.Filename()].file
}

// fieldMasked reports whether the target id has field masks enabled.
func (s *selection) fieldMasked(id typegraph.QualifiedType) bool {
	to, _ := s.loaded.Option.Type(id)
	_, ok := maskMark(to)

	return ok
}

// flagOf returns the flag option of the field k.
func (s *selection) flagOf(k fieldKey) (flagOption, bool) {
	o := s.fieldOption(k)
	if o.Flag == nil {
		return flagOption{}, false
	}
	// checkOptions has reported an invalid flag option before anything asks
	// for one.
	f, err := flagFromOptions(*o.Flag)
	if err != nil {
		return flagOption{}, false
	}

	f.src = o.flagSrc

	return f, true
}

// maskNameOf returns the field mask name option of the field k.
func (s *selection) maskNameOf(k fieldKey) (maskName, bool) {
	o := s.fieldOption(k)
	if o.FieldMask == nil {
		return maskName{}, false
	}
	// checkOptions has reported an invalid field mask name before anything
	// asks for one.
	mn, err := maskNameFromArgs(map[string]string{"name": o.FieldMask.Name})

	return mn, err == nil
}

// checkOptions reports types both marked and ignored, and invalid field
// options.
func (s *selection) checkOptions() error {
	var errs []error

	for id, to := range s.loaded.Option.Types() {
		mark, isMarked := explicitMark(to)
		if ignore, isIgnored := ignoreMark(to); isMarked && isIgnored {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s is both marked and ignored (%s)",
				ErrDirective, mark.origin(), id.String(), ignore.origin(),
			))
		}

		fields, _ := to.Fields.Unpack()
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			errs = append(errs, fieldOptionErrors(fieldKeyOf(id, name), fields[name])...)
		}
	}

	return errors.Join(errs...)
}

func fieldOptionErrors(k fieldKey, o FieldOptions) []error {
	var errs []error

	switch o.Merge {
	case "", string(MergeOverwrite), string(MergeAppend), string(MergeNestedRecursive):
	default:
		errs = append(errs, fmt.Errorf(
			"%w: %s: %s: unknown strategy %q on field %s; want overwrite, append or recursive",
			ErrDirective, o.mergeSrc.origin(), o.mergeSrc.spelling(), o.Merge, k,
		))
	}

	if o.Flag != nil {
		if _, err := flagFromOptions(*o.Flag); err != nil {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on field %s: %w",
				ErrDirective, o.flagSrc.origin(), o.flagSrc.spelling(), k, err,
			))
		}
	}

	if o.FieldMask != nil {
		_, err := maskNameFromArgs(map[string]string{"name": o.FieldMask.Name})
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"%w: %s: %s on field %s: %w",
				ErrDirective, o.maskSrc.origin(), o.maskSrc.spelling(), k, err,
			))
		}
	}

	return errs
}

// allFieldOptions yields the field options of the run ordered by package
// path, type name and field name.
func (s *selection) allFieldOptions() iter.Seq2[fieldKey, FieldOptions] {
	return func(yield func(fieldKey, FieldOptions) bool) {
		for id, to := range s.loaded.Option.Types() {
			fields, _ := to.Fields.Unpack()
			for name, o := range opiter.MapsSorted(fields) {
				if !yield(fieldKeyOf(id, name), o) {
					return
				}
			}
		}
	}
}
