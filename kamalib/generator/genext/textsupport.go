package genext

import (
	"errors"
	"fmt"
	"go/types"
	"iter"
	"strconv"
	"strings"
)

// textSupport is the Holder, Container and Composite a type of a text
// extension file describes. A part the file leaves out claims nothing.
type textSupport struct{ scope TextScope }

var (
	_ Holder    = textSupport{}
	_ Container = textSupport{}
	_ Composite = textSupport{}
	_ Binder    = textSupport{}
)

// Name returns the name of the file and the key of the type, joined by #.
func (s textSupport) Name() string { return s.scope.file + "#" + s.scope.key }

func (s textSupport) Bind(out Output, helperPrefix string) TypeSupport {
	return textSupport{scope: s.scope.Bind(out, helperPrefix)}
}

func (s textSupport) entry() TextType { return s.scope.doc.Types[s.scope.key] }

func (s textSupport) Held(t types.Type, v Gen) (string, types.Type, Gen, Gen, bool) {
	h := s.entry().Holder
	if h == nil || !s.scope.Claims(t) {
		return "", nil, nil, nil, false
	}

	held, err := s.scope.ValueType(t, h.Type)
	if err != nil {
		return "", nil, nil, nil, false
	}

	return h.Role, held,
		s.scope.Gen([]string{"holder", "guard"}, h.Guard, v, nil),
		s.scope.Gen([]string{"holder", "value"}, h.Value, v, nil),
		true
}

func (s textSupport) Elements(t types.Type, v Gen) (string, types.Type, Gen, Segment, bool) {
	c := s.entry().Container
	if c == nil || !s.scope.Claims(t) {
		return "", nil, nil, SegmentNone, false
	}

	elem, err := s.scope.ValueType(t, c.Type)
	if err != nil {
		return "", nil, nil, SegmentNone, false
	}

	over := s.scope.Gen([]string{"container", "over"}, c.Over, v, nil)

	if c.Yields == "pairs" {
		return c.Role, elem, over, segmentOf(c.Segment), true
	}

	// A range statement of a generator declares an index and an element,
	// so a sequence of elements alone is ranged over through a function
	// literal counting them.
	return c.Role, elem, func(ctx Context) (string, error) {
		typ, err := ctx.Package().Type(elem)
		if err != nil {
			return "", err
		}

		seq, err := over(ctx)
		if err != nil {
			return "", err
		}

		return lang.Expr(
			"func(kamaExtYield func(int, %s) bool) { kamaExtI := 0; "+
				"for kamaExtE := range %s { "+
				"if !kamaExtYield(kamaExtI, kamaExtE) { return }; kamaExtI++ } }",
			typ, seq,
		)(ctx)
	}, SegmentIndex, true
}

func (s textSupport) Roles(t types.Type) []string {
	c := s.entry().Composite
	if c == nil || !s.scope.Claims(t) {
		return nil
	}

	roles := make([]string, 0, len(c.Parts))
	for _, p := range c.Parts {
		roles = append(roles, p.Role)
	}

	return roles
}

func (s textSupport) Part(
	t types.Type,
	v Gen,
	role string,
) (types.Type, Gen, Gen, Segment, Gen, bool) {
	c := s.entry().Composite
	if c == nil || !s.scope.Claims(t) {
		return nil, nil, nil, SegmentNone, nil, false
	}

	for i, p := range c.Parts {
		if p.Role != role {
			continue
		}

		part, err := s.scope.ValueType(t, p.Type)
		if err != nil {
			return nil, nil, nil, SegmentNone, nil, false
		}

		at := []string{"composite", "parts", strconv.Itoa(i)}

		var guard, label Gen
		if p.Guard != "" {
			guard = s.scope.Gen(append(at, "guard"), p.Guard, v, nil)
		}

		if p.Label != "" {
			label = s.scope.Gen(append(at, "label"), p.Label, v, nil)
		}

		return part, guard, s.scope.Gen(append(at, "value"), p.Value, v, nil), segmentOf(p.Segment),
			label, true
	}

	return nil, nil, nil, SegmentNone, nil, false
}

func segmentOf(word string) Segment {
	switch word {
	case "index":
		return SegmentIndex
	case "key":
		return SegmentKey
	case "field":
		return SegmentField
	}

	return SegmentNone
}

// CheckText reports the type supports of ss from text extension files that
// describe a type pkgs reach and do not resolve: a type missing from its
// package, and a type spec naming no type parameter, field or method. A
// type of a package pkgs do not reach is left alone, since a file may
// describe types of other programs too. pkgs are the loaded packages, and
// the packages they import are reached through them.
func CheckText(ss iter.Seq[TypeSupport], pkgs []*types.Package) error {
	reached := map[string]*types.Package{}

	var reach func(p *types.Package)

	reach = func(p *types.Package) {
		if p == nil || reached[p.Path()] != nil {
			return
		}

		reached[p.Path()] = p
		for _, imp := range p.Imports() {
			reach(imp)
		}
	}

	for _, p := range pkgs {
		reach(p)
	}

	var errs []error

	for s := range ss {
		ts, ok := s.(textSupport)
		if !ok {
			continue
		}

		pkgPath, name, _ := splitTypeKey(ts.scope.key)

		p := reached[pkgPath]
		if p == nil {
			continue
		}

		obj, _ := p.Scope().Lookup(name).(*types.TypeName)
		if obj == nil {
			errs = append(
				errs,
				ts.scope.Error(nil, "package %s declares no type %s", pkgPath, name),
			)

			continue
		}

		named, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok {
			errs = append(errs, ts.scope.Error(nil, "%s is no defined type", ts.scope.key))
			continue
		}

		for path, spec := range ts.specs() {
			if _, err := resolveSpec(named, spec); err != nil {
				errs = append(errs, ts.scope.Error(path, "%v", err))
			}
		}
	}

	return errors.Join(errs...)
}

// specs yields the type specs of the description of s with their key paths
// below the entry of the type.
func (s textSupport) specs() iter.Seq2[[]string, string] {
	return func(yield func([]string, string) bool) {
		e := s.entry()

		if e.Holder != nil && !yield([]string{"holder", "type"}, e.Holder.Type) {
			return
		}

		if e.Container != nil && !yield([]string{"container", "type"}, e.Container.Type) {
			return
		}

		if e.Composite != nil {
			for i, p := range e.Composite.Parts {
				if !yield([]string{"composite", "parts", strconv.Itoa(i), "type"}, p.Type) {
					return
				}
			}
		}
	}
}

// resolveSpec returns the type spec names for named: the type argument of
// the type parameter spec names, as "T", or, for ".Name", the type of the
// field Name of named or of the first result of its method Name. For a
// generic type not instantiated, a type parameter stands for its argument.
func resolveSpec(named *types.Named, spec string) (types.Type, error) {
	if sel, ok := strings.CutPrefix(spec, "."); ok {
		obj, _, _ := types.LookupFieldOrMethod(named, true, named.Obj().Pkg(), sel)

		switch obj := obj.(type) {
		case *types.Var:
			return obj.Type(), nil
		case *types.Func:
			if res := obj.Signature().Results(); res.Len() > 0 {
				return res.At(0).Type(), nil
			}

			return nil, fmt.Errorf("method %s of %s returns nothing", sel, named.Obj().Name())
		}

		return nil, fmt.Errorf("%s has no field or method %s", named.Obj().Name(), sel)
	}

	tparams := named.Origin().TypeParams()
	for i := range tparams.Len() {
		if tparams.At(i).Obj().Name() != spec {
			continue
		}

		if args := named.TypeArgs(); args.Len() > i {
			return args.At(i), nil
		}

		return tparams.At(i), nil
	}

	return nil, fmt.Errorf(
		"%s has no type parameter %s; want a type parameter name or .<field or method>",
		named.Obj().Name(), spec,
	)
}
