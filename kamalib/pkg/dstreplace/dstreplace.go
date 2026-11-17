package dstreplace

import (
	"errors"
	"fmt"
	"go/token"
	"reflect"
	"slices"
	"strconv"

	"github.com/dave/dst"
	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/gofragment"
)

var (
	// ErrNotFound reports a source node the decorated file does not hold.
	ErrNotFound = errors.New("dstreplace: node not found")
	// ErrReplacement reports a Replacement Struct cannot apply.
	ErrReplacement = errors.New("dstreplace: invalid replacement")
)

// Field is a name of a field of a source struct literal, or an embedded
// field.
type Field struct {
	// Index is the index of the field in the struct type go/types gives the
	// literal: names count one each, and an embedded field counts one.
	Index int
	// Name is the field name. For an embedded field it is the name of the
	// embedded type, as go/types names the field.
	Name     string
	Embedded bool
	// Tag is the unquoted tag of the source field.
	Tag string
}

// Fields returns the fields of st in the order go/types numbers them.
//
// Fields returns an error wrapping ErrNotFound for an embedded field whose
// type names no type, and the error of unquoting a tag literal.
func Fields(st *dst.StructType) ([]Field, error) {
	var out []Field

	for _, f := range st.Fields.List {
		tag, err := tagValue(f.Tag)
		if err != nil {
			return nil, err
		}

		if len(f.Names) == 0 {
			name, ok := embeddedName(f.Type)
			if !ok {
				return nil, fmt.Errorf("%w: name of embedded field", ErrNotFound)
			}

			out = append(out, Field{Index: len(out), Name: name, Embedded: true, Tag: tag})

			continue
		}

		for _, n := range f.Names {
			out = append(out, Field{Index: len(out), Name: n.Name, Tag: tag})
		}
	}

	return out, nil
}

func tagValue(lit *dst.BasicLit) (string, error) {
	if lit == nil {
		return "", nil
	}

	tag, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", fmt.Errorf("dstreplace: tag %s: %w", lit.Value, err)
	}

	return tag, nil
}

func embeddedName(e dst.Expr) (string, bool) {
	switch e := e.(type) {
	case *dst.Ident:
		return e.Name, true
	case *dst.SelectorExpr:
		return e.Sel.Name, true
	case *dst.StarExpr:
		return embeddedName(e.X)
	case *dst.IndexExpr:
		return embeddedName(e.X)
	case *dst.IndexListExpr:
		return embeddedName(e.X)
	case *dst.ParenExpr:
		return embeddedName(e.X)
	}

	return "", false
}

// Replacement is the field a Replacer derives from a source field name.
type Replacement struct {
	// Drop leaves the name out. The other fields are ignored.
	Drop bool
	// Name turns an embedded field into a field of that name. It is
	// ignored for a named field, which keeps its name.
	Name string
	// Type is the type of the new field. It must not be nil. Struct places
	// a copy in the result, so a Replacer may return one node for several
	// names.
	Type dst.Expr
	// Tag is the unquoted tag of the new field, empty for none.
	Tag string
}

// Replacer derives the field of the new struct from a source field name.
type Replacer func(f Field) (Replacement, error)

// Struct returns a copy of src, a decorated struct literal, with every
// field replaced as replace tells and every comment erased. src is not
// changed. The package documentation describes how names stay grouped or
// are split and how tags are written.
//
// Struct returns the errors of Fields, the error of replace, wrapped, an
// error wrapping ErrReplacement for a Replacement without Type, and the
// error of printing a replacement type, which decides whether names stay
// grouped.
func Struct(src *dst.StructType, replace Replacer) (*dst.StructType, error) {
	fields, err := Fields(src)
	if err != nil {
		return nil, err
	}

	st := dst.Clone(src).(*dst.StructType)
	eraseComments(st)
	st.Decs.NodeDecs = dst.NodeDecs{}

	var out []*dst.Field

	i := 0

	for _, f := range st.Fields.List {
		count := max(len(f.Names), 1)

		got, err := replaceField(f, fields[i:i+count], replace)
		if err != nil {
			return nil, err
		}

		out = append(out, got...)
		i += count
	}
	// A dropped field leaves the line breaks around it to its neighbors,
	// which hold them in their own Before and After.
	if len(out) > 0 {
		if out[0].Decs.Before == dst.EmptyLine {
			out[0].Decs.Before = dst.NewLine
		}

		if out[len(out)-1].Decs.After == dst.EmptyLine {
			out[len(out)-1].Decs.After = dst.NewLine
		}
	}

	st.Fields.List = out

	return st, nil
}

// replaceField returns the fields replacing f, a field of the copy, whose
// names are fields.
func replaceField(f *dst.Field, fields []Field, replace Replacer) ([]*dst.Field, error) {
	var (
		names []*dst.Ident
		reps  []Replacement
		keys  []string
	)

	for k, field := range fields {
		r, err := replace(field)
		if err != nil {
			return nil, fmt.Errorf("dstreplace: field %s: %w", field.Name, err)
		}

		if r.Drop {
			continue
		}

		if r.Type == nil {
			return nil, fmt.Errorf("%w: field %s has no type", ErrReplacement, field.Name)
		}

		b, err := gofragment.PrintDstExpr(r.Type)
		if err != nil {
			return nil, fmt.Errorf("dstreplace: field %s: %w", field.Name, err)
		}

		if len(f.Names) > 0 {
			names = append(names, f.Names[k])
		} else if r.Name != "" {
			names = append(names, dst.NewIdent(r.Name))
		}

		reps = append(reps, r)
		keys = append(keys, string(b)+"\x00"+r.Tag)
	}

	if len(reps) == 0 {
		return nil, nil
	}

	allSame := opiter.WrapSeq(
		slices.Values(keys[1:]),
	).
		Every(
			func(k string) bool { return k == keys[0] },
		)

	if allSame {
		f.Names = names
		f.Type = dst.Clone(reps[0].Type).(dst.Expr)
		f.Tag = tagLit(f.Tag, fields[0].Tag, reps[0].Tag)

		return []*dst.Field{f}, nil
	}

	splitField := func(k int, r Replacement) *dst.Field {
		var lit *dst.BasicLit
		if f.Tag != nil {
			lit = dst.Clone(f.Tag).(*dst.BasicLit)
		}

		nf := &dst.Field{
			Names: []*dst.Ident{names[k]},
			// A dst node belongs to one tree, and a Replacer may return one
			// node for several names.
			Type: dst.Clone(r.Type).(dst.Expr),
			Tag:  tagLit(lit, fields[0].Tag, r.Tag),
		}
		nf.Decs.Before, nf.Decs.After = dst.NewLine, dst.NewLine

		return nf
	}

	out := opiter.WrapSeq2(
		slices.All(reps),
	).
		Assemble(splitField).
		Collect()

	out[0].Decs.Before = f.Decs.Before
	out[len(out)-1].Decs.After = f.Decs.After

	return out, nil
}

// tagLit returns the literal of tag: lit, the source literal of src, when
// tag equals src, and lit with its value rewritten otherwise.
func tagLit(lit *dst.BasicLit, src, tag string) *dst.BasicLit {
	// An empty tag gives no literal, even for a source literal spelling
	// the empty tag.
	if tag == "" {
		return nil
	}

	if tag == src {
		return lit
	}

	if lit == nil {
		lit = &dst.BasicLit{Kind: token.STRING}
	}

	lit.Value = strconv.Quote(tag)
	if strconv.CanBackquote(tag) {
		lit.Value = "`" + tag + "`"
	}

	return lit
}

var decorationsType = reflect.TypeFor[dst.Decorations]()

// eraseComments empties every decoration of root and the nodes under it.
// Line breaks between nodes are kept in Before and After, which are not
// decorations.
func eraseComments(root dst.Node) {
	dst.Inspect(root, func(n dst.Node) bool {
		if n == nil {
			return false
		}

		if decs := reflect.ValueOf(n).Elem().FieldByName("Decs"); decs.IsValid() {
			eraseDecorations(decs)
		}

		return true
	})
}

func eraseDecorations(v reflect.Value) {
	for _, f := range v.Fields() {
		switch {
		case f.Type() == decorationsType:
			f.SetZero()
		case f.Kind() == reflect.Struct:
			eraseDecorations(f)
		}
	}
}
