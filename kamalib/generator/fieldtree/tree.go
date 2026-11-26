package fieldtree

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/types"
	"iter"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/rivo/uniseg"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/fieldtree/ext"
	"github.com/ngicks/kama/kamalib/generator/genext"
)

// tree returns the tree of t.
func tree(in Input, t target) (string, error) {
	typ := types.Unalias(t.obj.Type())
	if _, ok := typ.Underlying().(*types.Struct); !ok {
		return "", fmt.Errorf("%w: %s: %s is no struct type", ErrTarget, t.pos, t.obj.Name())
	}

	w := walker{pkgs: in.Pkgs, reg: in.Registry, key: cmp.Or(t.key, DefaultKey), home: t.obj.Pkg()}

	var b strings.Builder

	b.WriteString(cmp.Or(t.label, t.obj.Name()) + "\n")

	hashAt, descW := w.measure(0, typ, "", "", nil, 0)
	w.render(&b, "", typ, "", "", nil, 0, hashAt, descW)

	return b.String(), nil
}

// walker draws the tree of one target. It holds the settings of the
// target and asks the types for everything else.
type walker struct {
	pkgs []*packages.Package
	reg  genext.Supports
	// key is the struct tag the key paths come from.
	key string
	// home is the package of the target, whose types the tree spells
	// without a package name.
	home *types.Package
}

// line is one line of a tree below a value, as lines yields it. lines
// builds it from the types each time it is asked.
type line struct {
	access, typ, desc, key string
	// note replaces the columns on a line telling why the walk stops.
	note string
	// next is the type the walk continues into below the line, nil for a
	// leaf, and expr is the template expression of that value.
	next types.Type
	expr string
}

// render writes the lines below the value of type t at prefix, and the
// lines below each of them. expr is the template expression of the value
// and key its key path. path holds the types expanded above. depth is the
// nesting depth of the lines, 0 for the fields of the target. The # of
// every line sits at the column hashAt, and the key path follows the
// description padded to descW, as measure returns them for the whole
// tree.
func (w walker) render(
	b *strings.Builder,
	prefix string,
	t types.Type,
	expr, key string,
	path []types.Type,
	depth, hashAt, descW int,
) {
	n, wa, wt := w.columns(t, expr, key, path)
	below := append(slices.Clip(path), deref(t))
	i := 0

	for l := range w.lines(t, expr, key, path) {
		i++

		branch, indent := "├─ ", "│   "
		if i == n {
			branch, indent = "└─ ", "    "
		}

		text := prefix + branch + l.note
		if l.note == "" {
			text = pad(prefix+branch+pad(l.access, wa)+"  "+pad(l.typ, wt), hashAt) +
				"# " + pad(descIndent(depth)+l.desc, descW) + "  (" + l.key + ")"
		}

		b.WriteString(strings.TrimRight(text, " ") + "\n")

		if l.next != nil {
			w.render(b, prefix+indent, l.next, l.expr, l.key, below, depth+1, hashAt, descW)
		}
	}
}

// measure walks the lines below the value of type t, drawn from the
// column at, and the lines below each of them, as render does. It returns
// the column of the # that follows the widest type column plus two
// spaces, and the width of the widest indented description. render
// aligns the whole tree on them, so the widths come from walking the tree
// again instead of from a list of lines kept between the passes.
func (w walker) measure(
	at int,
	t types.Type,
	expr, key string,
	path []types.Type,
	depth int,
) (int, int) {
	_, wa, wt := w.columns(t, expr, key, path)
	below := append(slices.Clip(path), deref(t))

	var hashAt, descW int

	for l := range w.lines(t, expr, key, path) {
		if l.note == "" {
			// 3 is the width of the branch, ├─ or └─ and a space.
			hashAt = max(hashAt, at+3+wa+2+wt+2)
			descW = max(descW, width(descIndent(depth)+l.desc))
		}

		if l.next != nil {
			h, d := w.measure(at+4, l.next, l.expr, l.key, below, depth+1)
			hashAt, descW = max(hashAt, h), max(descW, d)
		}
	}

	return hashAt, descW
}

// columns walks the lines below the value of type t once to count them
// and to find the widths of their access and type columns, which align
// among the lines hanging below one value.
func (w walker) columns(t types.Type, expr, key string, path []types.Type) (n, wa, wt int) {
	for l := range w.lines(t, expr, key, path) {
		n++

		if l.note == "" {
			wa = max(wa, width(l.access))
			wt = max(wt, width(l.typ))
		}
	}

	return n, wa, wt
}

// descIndent indents a description by two spaces per nesting depth, so
// that the descriptions show the nesting of the fields beside the aligned
// # column.
func descIndent(depth int) string { return strings.Repeat("  ", depth) }

// width returns the number of terminal columns s takes, counted per
// grapheme cluster, so a wide character or an emoji sequence counts two
// and a combining mark none. uniseg is used over go-runewidth because
// go-runewidth counts an emoji presentation sequence such as ☀ with VS16
// as one column. uniseg reads no environment, and its ambiguous width is
// left at the default of one, so the box-drawing characters of the tree
// count one column.
func width(s string) int { return uniseg.StringWidth(s) }

func pad(s string, n int) string { return s + strings.Repeat(" ", n-width(s)) }

// lines yields the lines right below the value of type t: a note when the
// walk stops at t, a line per value a type support tells t holds, one
// line for the elements of a slice, an array or a map, or a line per
// field of a struct.
func (w walker) lines(t types.Type, expr, key string, path []types.Type) iter.Seq[line] {
	return func(yield func(line) bool) {
		u := deref(t)

		if tp, ok := u.(*types.TypeParam); ok {
			yield(line{note: fmt.Sprintf(
				"%s is a type parameter; the walk stops until it is instantiated", tp,
			)})

			return
		}

		if onPath(path, u) {
			yield(line{note: fmt.Sprintf(
				"%s repeats; it is expanded above", w.typeString(u),
			)})

			return
		}

		if w.claimed(u) {
			for _, role := range genext.Roles(w.reg, u) {
				if !yield(w.roleLine(u, role, expr, key)) {
					return
				}
			}

			return
		}

		e := cmp.Or(expr, ".")

		switch x := u.Underlying().(type) {
		case *types.Slice:
			yield(w.indexLine(x.Elem(), e, key))
		case *types.Array:
			yield(w.indexLine(x.Elem(), e, key))
		case *types.Map:
			k := indexVar(key, "key")
			yield(line{
				access: "index " + e + " " + k,
				typ:    w.typeString(x.Elem()),
				desc:   "value at " + k + "; range " + e,
				key:    key + "[" + k + "]",
				next:   w.next(x.Elem()),
				expr:   "(index " + e + " " + k + ")",
			})
		case *types.Struct:
			for l := range w.fields(x, expr, key, append(slices.Clip(path), u)) {
				if !yield(l) {
					return
				}
			}
		}
	}
}

func (w walker) indexLine(elem types.Type, e, key string) line {
	i := indexVar(key, "i")

	return line{
		access: "index " + e + " " + i,
		typ:    w.typeString(elem),
		desc:   "element " + i + "; range " + e,
		key:    key + "[" + i + "]",
		next:   w.next(elem),
		expr:   "(index " + e + " " + i + ")",
	}
}

// indexVar names the index or key of an element by the number of indexes
// key holds already, so that nested containers name theirs apart: i, then
// i2, i3 and so on.
func indexVar(key, name string) string {
	if n := strings.Count(key, "["); n > 0 {
		return name + strconv.Itoa(n+1)
	}

	return name
}

// roleLine returns the line of the value of role that a value of type t,
// claimed by a type support, holds.
func (w walker) roleLine(t types.Type, role, expr, key string) line {
	held, _, _, _, segment, _, _ := genext.RoleValue(w.reg, t, nil, role, nil, nil)

	access, note, keySeg, ok := w.reach(t, role, expr)
	if !ok {
		access = "(" + role + ")"
		note = fmt.Sprintf("the %s value of %s; no support tells how a template reaches it",
			role, w.typeString(t))
		keySeg = map[genext.Segment]string{
			genext.SegmentIndex: "[i]",
			genext.SegmentKey:   "[key]",
			genext.SegmentField: "." + role,
		}[segment]
	}

	next := access
	if strings.Contains(access, " ") {
		next = "(" + access + ")"
	}

	return line{
		access: access,
		typ:    w.typeString(held),
		desc:   note,
		key:    key + keySeg,
		next:   w.next(held),
		expr:   next,
	}
}

// reach asks the first ext.Reacher of the registry claiming t.
func (w walker) reach(t types.Type, role, expr string) (string, string, string, bool) {
	for r := range genext.SupportsOf[ext.Reacher](w.reg) {
		if access, note, key, ok := r.Reach(t, role, expr); ok {
			return access, note, key, true
		}
	}

	return "", "", "", false
}

// fields yields the lines of the fields of st. An embedded struct without
// a name in the tag lends its fields, as encoding/json inlines them.
func (w walker) fields(st *types.Struct, expr, key string, path []types.Type) iter.Seq[line] {
	return func(yield func(line) bool) {
		for i := range st.NumFields() {
			f := st.Field(i)

			tag := reflect.StructTag(st.Tag(i)).Get(w.key)
			if tag == "-" {
				continue
			}

			name, _, _ := strings.Cut(tag, ",")

			if f.Embedded() && name == "" {
				et := deref(f.Type())
				if inner, ok := et.Underlying().(*types.Struct); ok {
					if onPath(path, et) {
						continue
					}

					for l := range w.fields(inner, expr, key, append(slices.Clip(path), et)) {
						if !yield(l) {
							return
						}
					}

					continue
				}
			}

			if !f.Exported() {
				continue
			}

			if !yield(line{
				access: "." + f.Name(),
				typ:    w.typeString(f.Type()),
				desc:   w.describe(f),
				key:    joinKey(key, cmp.Or(name, f.Name())),
				next:   w.next(f.Type()),
				expr:   expr + "." + f.Name(),
			}) {
				return
			}
		}
	}
}

func joinKey(key, name string) string {
	if key == "" {
		return name
	}

	return key + "." + name
}

// next returns t when the walk continues below a value of type t, nil for
// a leaf.
func (w walker) next(t types.Type) types.Type {
	u := deref(t)

	switch {
	case isTypeParam(u), w.claimed(u):
		return t
	case marshals(u):
		return nil
	}

	switch x := u.Underlying().(type) {
	case *types.Slice:
		if b, ok := x.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Byte {
			return nil
		}

		return t
	case *types.Array, *types.Map:
		return t
	case *types.Struct:
		if n, ok := u.(*types.Named); ok && !w.sourceLoaded(n) {
			return nil
		}

		return t
	}

	return nil
}

// claimed reports whether a type support of the registry claims t.
func (w walker) claimed(t types.Type) bool {
	if _, ok := genext.HolderOf(w.reg, t); ok {
		return true
	}

	if _, ok := genext.ContainerOf(w.reg, t); ok {
		return true
	}

	_, ok := genext.CompositeOf(w.reg, t)

	return ok
}

// sourceLoaded reports whether the syntax of the package declaring n is
// loaded.
func (w walker) sourceLoaded(n *types.Named) bool {
	if n.Obj().Pkg() == nil {
		return false
	}

	p := loadedPackage(w.pkgs, n.Obj().Pkg().Path())

	return p != nil && len(p.Syntax) > 0
}

// describe returns the first sentence of the doc comment of f, else its
// line comment, else nothing.
func (w walker) describe(f *types.Var) string {
	if f.Pkg() == nil {
		return ""
	}

	pkg := loadedPackage(w.pkgs, f.Pkg().Path())
	if pkg == nil {
		return ""
	}

	field := fieldSyntax(pkg, f)

	switch {
	case field == nil:
		return ""
	case field.Doc != nil:
		if s := firstSentence(field.Doc.Text()); s != "" {
			return s
		}
	}

	if field.Comment == nil {
		return ""
	}

	return strings.Join(strings.Fields(field.Comment.Text()), " ")
}

// firstSentence returns the first sentence of the first paragraph of
// text, its lines joined by spaces. go/doc.Synopsis is not used: it drops
// a text starting with Author or Copyright, which a field doc may well do.
func firstSentence(text string) string {
	para, _, _ := strings.Cut(strings.TrimSpace(text), "\n\n")
	s := strings.Join(strings.Fields(para), " ")

	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}

	return s
}

// fieldSyntax returns the field of the syntax of pkg declaring f, or nil.
func fieldSyntax(pkg *packages.Package, f *types.Var) *ast.Field {
	var found *ast.Field

	for _, file := range pkg.Syntax {
		if f.Pos() < file.FileStart || f.Pos() >= file.FileEnd {
			continue
		}

		ast.Inspect(file, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if found != nil || !ok {
				return found == nil
			}

			if slices.ContainsFunc(
				field.Names,
				func(id *ast.Ident) bool { return id.Pos() == f.Pos() },
			) ||
				(len(field.Names) == 0 && field.Type.Pos() <= f.Pos() && f.Pos() < field.Type.End()) {
				found = field
			}

			return found == nil
		})
	}

	return found
}

// typeString spells t, the types of the package of the target without a
// package name.
func (w walker) typeString(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == w.home {
			return ""
		}

		return p.Name()
	})
}

// deref returns the type pointers to t point to, t itself for another
// type.
func deref(t types.Type) types.Type {
	for {
		t = types.Unalias(t)

		p, ok := t.(*types.Pointer)
		if !ok {
			return t
		}

		t = p.Elem()
	}
}

func isTypeParam(t types.Type) bool {
	_, ok := t.(*types.TypeParam)
	return ok
}

// onPath reports whether path holds the named type t.
func onPath(path []types.Type, t types.Type) bool {
	if _, ok := t.(*types.Named); !ok {
		return false
	}

	return slices.ContainsFunc(path, func(p types.Type) bool { return types.Identical(p, t) })
}

// marshals reports whether a value of type t or a pointer to it encodes
// itself as JSON or text, which makes it a leaf.
func marshals(t types.Type) bool {
	ptr := types.NewPointer(t)

	return slices.ContainsFunc(
		[]string{"MarshalJSON", "MarshalJSONTo", "MarshalText"},
		func(name string) bool {
			obj, _, _ := types.LookupFieldOrMethod(ptr, true, nil, name)
			_, ok := obj.(*types.Func)

			return ok
		},
	)
}
