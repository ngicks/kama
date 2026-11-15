package genhelper_test

import (
	"errors"
	"fmt"
	"go/format"
	"go/types"
	"os"
	"slices"

	"github.com/dave/dst/decorator"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

type step = genhelper.Step[genhelper.BaseContext]

func lookup(s *genhelper.Scope, roles ...string) ([]string, error) {
	lookupRole := func(role string) (string, error) {
		name, ok := s.Lookup(role)
		if !ok {
			return "", fmt.Errorf("role %q is not declared", role)
		}
		return name, nil
	}
	names, err := opiter.TryCollect(opiter.Divide(lookupRole, slices.Values(roles)))
	if err != nil {
		return nil, err
	}
	return names, nil
}

// rangeLevels converts a nested container one level per block. Every level
// declares the same roles. No level knows how deep it is nested.
func rangeLevels(types []string) step {
	last := len(types) == 1
	block := genhelper.Block[genhelper.BaseContext]{
		Declare: func(c genhelper.BaseContext) (genhelper.BaseContext, error) {
			c.Scope().Declare("k")
			c.Scope().Declare("v")
			if !last {
				c.Scope().Declare("inner")
			}
			return c, nil
		},
		Wrap: func(c genhelper.BaseContext, body string) (string, error) {
			outer, err := lookup(c.Scope().Parent(), "v", "inner")
			if err != nil {
				return "", err
			}
			src, dst := outer[0], outer[1]
			if last {
				own, err := lookup(c.Scope(), "k", "v")
				if err != nil {
					return "", err
				}
				return fmt.Sprintf(
					"for %[1]s, %[2]s := range %[3]s {\n%[4]s[%[1]s] = toPartial(%[2]s)\n}\n",
					own[0], own[1], src, dst,
				), nil
			}
			own, err := lookup(c.Scope(), "k", "v", "inner")
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(
				"for %[1]s, %[2]s := range %[4]s {\n"+
					"%[3]s := make(%[6]s, len(%[2]s))\n%[7]s%[5]s[%[1]s] = %[3]s\n}\n",
				own[0], own[1], own[2], src, dst, types[1], body,
			), nil
		},
	}
	if !last {
		block.Body = rangeLevels(types[1:])
	}
	return block
}

func ExampleBlock() {
	types := []string{"map[string][][]Partial", "[][]Partial", "[]Partial"}

	root := genhelper.NewScope()
	root.ReserveUniverse()
	// The destination package declares k.
	root.Reserve("k")

	r := genhelper.Renderer[genhelper.BaseContext]{
		Context: genhelper.NewBaseContext(root, nil),
		Steps: genhelper.Steps[genhelper.BaseContext]{
			genhelper.Block[genhelper.BaseContext]{
				Declare: func(c genhelper.BaseContext) (genhelper.BaseContext, error) {
					c.Scope().Declare("v")
					c.Scope().Declare("inner")
					return c, nil
				},
				Body: rangeLevels(types),
				Wrap: func(c genhelper.BaseContext, body string) (string, error) {
					own, err := lookup(c.Scope(), "v", "inner")
					if err != nil {
						return "", err
					}
					return fmt.Sprintf(
						"func convert(%[1]s map[string][][]T) %[3]s {\n"+
							"%[2]s := make(%[3]s, len(%[1]s))\n%[4]sreturn %[2]s\n}\n",
						own[0], own[1], types[0], body,
					), nil
				},
			},
		},
	}
	_, b, err := r.Render(nil)
	if err != nil {
		panic(err)
	}
	b, err = format.Source(b)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(b))
	// Output:
	// func convert(v map[string][][]T) map[string][][]Partial {
	// 	inner := make(map[string][][]Partial, len(v))
	// 	for k2, v2 := range v {
	// 		inner2 := make([][]Partial, len(v2))
	// 		for k3, v3 := range v2 {
	// 			inner3 := make([]Partial, len(v3))
	// 			for k4, v4 := range v3 {
	// 				inner3[k4] = toPartial(v4)
	// 			}
	// 			inner2[k3] = inner3
	// 		}
	// 		inner[k2] = inner2
	// 	}
	// 	return inner
	// }
}

func ExampleWrap() {
	root := genhelper.NewScope()
	// The destination package declares e.
	root.Reserve("e")

	r := genhelper.Renderer[genhelper.BaseContext]{
		Context: genhelper.NewBaseContext(root.Push(), nil),
		Steps: genhelper.Steps[genhelper.BaseContext]{
			// The innermost step runs first. It refers to the element the
			// last step declares.
			genhelper.Append(func(c genhelper.BaseContext) (string, error) {
				return "und.FromPointer(" + c.Scope().Use("e") + ".X)", nil
			}),
			genhelper.Wrap(func(_ genhelper.BaseContext, inner string) (string, error) {
				return inner + ".CollapseToNull()", nil
			}),
			genhelper.Wrap(func(c genhelper.BaseContext, inner string) (string, error) {
				return fmt.Sprintf(
					"for _, %s := range items {\nout = append(out, %s)\n}\n",
					c.Scope().Declare("e"), inner,
				), nil
			}),
		},
	}
	c, b, err := r.Render(nil)
	if err != nil {
		panic(err)
	}
	b, err = format.Source(b)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(b))
	fmt.Println("unbound:", c.Scope().Unbound())
	// Output:
	// for _, e2 := range items {
	// 	out = append(out, und.FromPointer(e2.X).CollapseToNull())
	// }
	// unbound: []
}

// fieldContext carries generator state next to the scope.
type fieldContext struct {
	genhelper.BaseContext
	field string
}

func (c fieldContext) WithScope(s *genhelper.Scope) fieldContext {
	c.BaseContext = c.BaseContext.WithScope(s)
	return c
}

func ExampleContext() {
	r := genhelper.Renderer[fieldContext]{
		Context: fieldContext{
			BaseContext: genhelper.NewBaseContext(genhelper.NewScope(), nil),
			field:       "Name",
		},
		Steps: genhelper.Steps[fieldContext]{
			genhelper.Block[fieldContext]{
				Declare: func(c fieldContext) (fieldContext, error) {
					c.Scope().Declare("v")
					return c, nil
				},
				Wrap: func(c fieldContext, _ string) (string, error) {
					v, _ := c.Scope().Lookup("v")
					return fmt.Sprintf(
						"func(%[1]s T) string { return %[1]s.%[2]s }",
						v,
						c.field,
					), nil
				},
			},
		},
	}
	_, b, err := r.Render(nil)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
	// Output:
	// func(v T) string { return v.Name }
}

func ExampleScope_Shadow() {
	root := genhelper.NewScope()
	root.ReserveUniverse()

	fn := root.Push()
	fmt.Println(fn.Declare("new"))

	other := root.Push()
	name, err := other.Shadow("new")
	fmt.Println(name, err)
	// A scope nested in the shadowing one still gets a fresh name.
	fmt.Println(other.Push().Declare("new"))

	_, err = other.Shadow("new")
	fmt.Println(err)
	// Output:
	// new2
	// new <nil>
	// new2
	// genhelper: redeclared in this scope: new
}

func ExampleScope_LookupOrDeclare() {
	fn := genhelper.NewScope().Push()

	assign := func(s *genhelper.Scope, call string) string {
		name, declared := s.LookupOrDeclare("err")
		if declared {
			return name + " := " + call
		}
		return name + " = " + call
	}

	fmt.Println(assign(fn, "open()"))
	// The enclosing scope declares err, so the nested one assigns to it.
	fmt.Println(assign(fn.Push(), "read()"))
	// Output:
	// err := open()
	// err = read()
}

func ExampleScope_LinkImports() {
	const (
		destPath = "example.com/dest"
		keyPath  = "example.com/key"
		valPath  = "example.com/val"
	)

	pkg := types.NewPackage(destPath, "dest")
	pkg.SetImports([]*types.Package{
		types.NewPackage(keyPath, "key"),
		types.NewPackage(valPath, "val"),
	})
	imports, err := importdecl.NewParser(pkg).ParseDst(nil)
	if err != nil {
		panic(err)
	}

	fn := genhelper.NewScope().Push()
	fn.LinkImports(imports)

	// A local declared first keeps its name, and the import requested later
	// avoids it.
	fmt.Println(fn.Declare("key"))
	ident, _ := imports.Ident(keyPath)
	fmt.Println(ident)

	// An import requested first keeps its identifier, and the local declared
	// later avoids it.
	ident, _ = imports.Ident(valPath)
	fmt.Println(ident)
	fmt.Println(fn.Declare("val"))
	// Output:
	// key
	// key_2
	// val
	// val2
}

func ExamplePackage() {
	const (
		destPath  = "example.com/dest"
		undPath   = "github.com/ngicks/und"
		otherPath = "example.com/other/und"
	)

	// The destination package can import two packages named und. Its file
	// imports one of them already.
	pkg := types.NewPackage(destPath, "dest")
	pkg.SetImports([]*types.Package{
		types.NewPackage(undPath, "und"),
		types.NewPackage(otherPath, "und"),
	})
	f, err := decorator.Parse("package dest\n\nimport \"" + otherPath + "\"\n")
	if err != nil {
		panic(err)
	}
	imports, err := importdecl.NewParser(pkg).ParseDst(f.Imports)
	if err != nil {
		panic(err)
	}

	r := genhelper.Renderer[genhelper.BaseContext]{
		Context: genhelper.NewBaseContext(
			genhelper.NewScope(),
			&genhelper.Package{Imports: imports, Path: destPath},
		),
		Steps: genhelper.Steps[genhelper.BaseContext]{
			genhelper.Append(func(c genhelper.BaseContext) (string, error) {
				fromPointer, err := c.Package().Qual(undPath, "FromPointer")
				if err != nil {
					return "", err
				}
				return fromPointer + "(" + c.Scope().Use("v") + ".X)", nil
			}),
			genhelper.Wrap(func(c genhelper.BaseContext, inner string) (string, error) {
				// A name of the destination package is spelled unqualified.
				partial, err := c.Package().Qual(destPath, "Partial")
				if err != nil {
					return "", err
				}
				return fmt.Sprintf(
					"func toPartial(%[1]s T) %[2]s {\nreturn %[2]s{X: %[3]s}\n}\n",
					c.Scope().Declare("v"), partial, inner,
				), nil
			}),
		},
	}
	_, b, err := r.Render(nil)
	if err != nil {
		panic(err)
	}
	// Qual recorded the import the file misses, under a name the existing
	// import does not take. AppendDecls adds it to the import declaration
	// of the file along with the function.
	if err := genhelper.AppendDecls(f, r.Context.Package(), b); err != nil {
		panic(err)
	}
	if err := decorator.Fprint(os.Stdout, f); err != nil {
		panic(err)
	}

	_, err = r.Context.Package().Qual("example.com/unknown", "X")
	fmt.Println(errors.Is(err, importdecl.ErrUnknownImport))
	// Output:
	// package dest
	//
	// import (
	// 	"example.com/other/und"
	// 	und_2 "github.com/ngicks/und"
	// )
	//
	// func toPartial(v T) Partial {
	// 	return Partial{X: und_2.FromPointer(v.X)}
	// }
	// true
}

func ExamplePrinter() {
	r := genhelper.Renderer[genhelper.BaseContext]{
		Context: genhelper.NewBaseContext(genhelper.NewScope(), nil),
		Steps: genhelper.Steps[genhelper.BaseContext]{
			genhelper.FuncStep[genhelper.BaseContext](
				func(c genhelper.BaseContext, b []byte) (genhelper.BaseContext, []byte, error) {
					p := c.Printer(b)
					p.Printf("%s := %d\n", c.Scope().Declare("n"), 1)
					p.Printf("%s := %q\n", c.Scope().Declare("s"), "one")
					return c, p.Bytes(), nil
				},
			),
		},
	}
	_, b, err := r.Render([]byte("// values\n"))
	if err != nil {
		panic(err)
	}
	fmt.Print(string(b))
	// Output:
	// // values
	// n := 1
	// s := "one"
}

func ExampleForRange() {
	type ctx = genhelper.BaseContext
	text := func(s string) genhelper.Gen[ctx] {
		return func(ctx) (string, error) { return s, nil }
	}
	// name returns the name a role refers to where the text renders.
	name := func(role string) genhelper.Gen[ctx] {
		return func(c ctx) (string, error) { return c.Scope().Use(role), nil }
	}
	printf := func(format string, roles ...string) genhelper.Step[ctx] {
		return genhelper.Append(func(c ctx) (string, error) {
			use := func(role string) any { return c.Scope().Use(role) }
			names := slices.Collect(opiter.Map(use, slices.Values(roles)))
			return fmt.Sprintf(format, names...), nil
		})
	}

	fn := genhelper.NewScope().Push()
	fn.Declare("v")

	r := genhelper.Renderer[ctx]{
		Context: genhelper.NewBaseContext(fn, nil),
		Steps: genhelper.Steps[ctx]{
			genhelper.ForRange("k", "v", text("items"),
				genhelper.Steps[ctx]{
					genhelper.If(text("skip[k]"),
						printf("continue\n"),
						nil,
					),
					genhelper.Switch(
						name("v"),
						genhelper.Case[ctx]{
							List: text("nil"),
							Body: printf("delete(out, %s)\n", "k"),
						},
						genhelper.Case[ctx]{Body: printf("out[%s] = %s\n", "k", "v")},
					),
				},
			),
		},
	}
	_, b, err := r.Render(nil)
	if err != nil {
		panic(err)
	}
	b, err = format.Source(b)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(b))
	// Output:
	// for k, v2 := range items {
	// 	if skip[k] {
	// 		continue
	// 	}
	// 	switch v2 {
	// 	case nil:
	// 		delete(out, k)
	// 	default:
	// 		out[k] = v2
	// 	}
	// }
}

// The vocabulary takes no argument of the context type, so a consumer
// instantiates it once for its context through thin wrappers.

func expr(format string, args ...any) genhelper.Gen[ctx] {
	return genhelper.Expr[ctx](format, args...)
}

func stmt(format string, args ...any) step {
	return genhelper.Stmt[ctx](format, args...)
}

func ExampleFunc() {
	type (
		decl = genhelper.Decl
		ref  = genhelper.Ref
	)

	root := genhelper.NewScope()
	root.ReserveUniverse()
	// The destination package declares v.
	root.Reserve("v")

	r := genhelper.Renderer[ctx]{
		Context: genhelper.NewBaseContext(root, nil),
		Steps: genhelper.Steps[ctx]{
			// Func, ForRange and If infer the context from their Gen arguments.
			genhelper.Func(
				expr("func (%s Set) Filter(%s func(string) bool) Set", decl("s"), decl("keep")),
				stmt("%s := make(Set, len(%s))", decl("out"), ref("s")),
				genhelper.ForRange("v", "", genhelper.Name[ctx]("s"),
					genhelper.If(expr("%s(%s)", ref("keep"), ref("v")),
						stmt("%s[%s] = true", ref("out"), ref("v")),
						nil,
					),
				),
				stmt("return %s", ref("out")),
			),
		},
	}
	_, b, err := r.Render(nil)
	if err != nil {
		panic(err)
	}
	b, err = format.Source(b)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(b))
	// Output:
	// func (s Set) Filter(keep func(string) bool) Set {
	// 	out := make(Set, len(s))
	// 	for v2 := range s {
	// 		if keep(v2) {
	// 			out[v2] = true
	// 		}
	// 	}
	// 	return out
	// }
}

func ExampleLang() {
	type (
		decl = genhelper.Decl
		ref  = genhelper.Ref
	)
	// The type of l names the context once, so no call instantiates it.
	var l genhelper.Lang[genhelper.BaseContext]

	root := genhelper.NewScope()
	root.ReserveUniverse()
	// The destination package declares n.
	root.Reserve("n")

	s, err := l.Render(
		genhelper.NewBaseContext(root, nil),
		l.DocFunc(
			l.Comment(l.Expr("Count returns the number of %s in %s.", ref("n"), ref("xs"))),
			l.Expr("func Count(%s []int, %s int) int", decl("xs"), decl("n")),
			l.Stmt("var %s int", decl("count")),
			l.ForRange("", "x", l.Name("xs"),
				l.If(l.Expr("%s == %s", ref("x"), ref("n")),
					l.Stmt("%s++", ref("count")),
					nil,
				),
			),
			l.Stmt("return %s", ref("count")),
		),
	)
	if err != nil {
		panic(err)
	}
	b, err := format.Source([]byte(s))
	if err != nil {
		panic(err)
	}
	fmt.Print(string(b))
	// Output:
	// // Count returns the number of n2 in xs.
	// func Count(xs []int, n2 int) int {
	// 	var count int
	// 	for _, x := range xs {
	// 		if x == n2 {
	// 			count++
	// 		}
	// 	}
	// 	return count
	// }
}

func ExampleDocFunc() {
	type (
		decl = genhelper.Decl
		ref  = genhelper.Ref
	)

	root := genhelper.NewScope()
	root.ReserveUniverse()
	// The destination package declares s.
	root.Reserve("s")

	r := genhelper.Renderer[ctx]{
		Context: genhelper.NewBaseContext(root, nil),
		Steps: genhelper.Steps[ctx]{
			genhelper.DocFunc(
				// The doc renders after the signature, so it spells the names
				// the signature declares. The marker line stays in the group.
				genhelper.Comment(
					expr("Has reports whether %s holds %s.", ref("s"), ref("key")),
					expr(""),
					expr("%s", "//example:generated"),
				),
				expr("func (%s Set) Has(%s string) bool", decl("s"), decl("key")),
				stmt("return %s[%s]", ref("s"), ref("key")),
			),
		},
	}
	_, b, err := r.Render(nil)
	if err != nil {
		panic(err)
	}
	b, err = format.Source(b)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(b))
	// Output:
	// // Has reports whether s2 holds key.
	// //
	// //example:generated
	// func (s2 Set) Has(key string) bool {
	// 	return s2[key]
	// }
}
