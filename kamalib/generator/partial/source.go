package partial

import (
	"go/ast"
	"go/types"
	"slices"
	"strconv"

	"github.com/ngicks/opiter"

	"github.com/dave/dst"

	"github.com/ngicks/kama/kamalib/pkg/dstreplace"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// sourceStruct returns the partial struct type of the target n built on a
// copy of its source struct literal, as dstreplace.Struct builds it, or
// nil when n is not declared by a struct literal. synth holds the
// synthesized field of each field of s, nil for a blank field. The copy
// takes the type and the tag of each field from there.
//
// A source the decorator cannot read, or whose fields do not match s, is
// not an error: the struct built from the types holds the same fields,
// without the layout of the source.
func (p *mapper) sourceStruct(
	n *typegraph.Node,
	s *types.Struct,
	synth []*dst.Field,
) (*dst.StructType, error) {
	if _, ok := n.Detail.Ts.Type.(*ast.StructType); !ok {
		return nil, nil
	}

	ts, err := p.sel.sources.TypeSpec(n.Detail.Pkg.Fset, n.Detail.File, n.Detail.Ts)
	if err != nil {
		return nil, nil //nolint:nilerr // The caller builds the struct from the types instead.
	}

	src, ok := ts.Type.(*dst.StructType)
	if !ok {
		return nil, nil
	}

	fields, err := dstreplace.Fields(src)
	if err != nil || !matchesFields(fields, s) {
		return nil, nil //nolint:nilerr // The caller builds the struct from the types instead.
	}

	return dstreplace.Struct(src, func(f dstreplace.Field) (dstreplace.Replacement, error) {
		sf := synth[f.Index]
		if sf == nil {
			return dstreplace.Replacement{Drop: true}, nil
		}

		r := dstreplace.Replacement{Type: sf.Type}
		if len(sf.Names) > 0 {
			r.Name = sf.Names[0].Name
		}

		if sf.Tag != nil {
			// withTag writes a valid literal.
			r.Tag, _ = strconv.Unquote(sf.Tag.Value)
		}

		return r, nil
	})
}

// matchesFields reports whether fields, the fields of a struct literal,
// are the fields of s one by one.
func matchesFields(fields []dstreplace.Field, s *types.Struct) bool {
	if len(fields) != s.NumFields() {
		return false
	}

	matchesField := func(i int, f dstreplace.Field) bool {
		v := s.Field(i)
		return f.Name == v.Name() && f.Embedded == v.Embedded()
	}

	return opiter.WrapSeq2(
		slices.All(fields),
	).
		Every2(matchesField)
}
