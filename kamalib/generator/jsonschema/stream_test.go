package jsonschema

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"gotest.tools/v3/assert"
)

// TestFixturesBufferOnlyWhereNeeded keeps the generated decoders
// streaming: of the decoders of the committed fixtures, only those of a
// union holder and of a product of allOf read a whole value or decode from
// read bytes, and no wire struct holds a member as raw JSON text to decode
// it again.
func TestFixturesBufferOnlyWhereNeeded(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(fixtureDir(name), name+".kama_jsonschema.go")

			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
			assert.NilError(t, err)

			types := map[string]*ast.TypeSpec{}

			for _, d := range f.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.TYPE {
					continue
				}

				for _, s := range g.Specs {
					ts := s.(*ast.TypeSpec)
					types[ts.Name.Name] = ts
				}
			}

			for name, ts := range types {
				if st, ok := ts.Type.(*ast.StructType); ok && wireStruct(name) {
					for _, field := range st.Fields.List {
						assert.Assert(t, !isJsontextValue(field.Type),
							"%s: wire struct %s holds raw JSON text", file, name)
					}
				}
			}

			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "UnmarshalJSONFrom" || !buffers(fn.Body) {
					continue
				}

				recv := receiverName(fn)
				ts := types[recv]
				assert.Assert(t, ts != nil && (holder(ts) || productStruct(ts)),
					"%s: UnmarshalJSONFrom of %s reads a whole value", file, recv)
			}
		})
	}
}

// TestFixtureHelpersHoldNoState keeps the helpers of the generated
// decoders free of state shared by calls: of the helper files of the
// committed fixtures, none imports package sync or sync/atomic, and no
// generated file assigns a package-level variable of the helpers after its
// declaration.
func TestFixtureHelpersHoldNoState(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file := filepath.Join(fixtureDir(name), "zz_gen.helper.kama_jsonschema.go")

			f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
			assert.NilError(t, err)

			var vars []string

			for _, imp := range f.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				assert.NilError(t, err)
				assert.Assert(
					t,
					path != "sync" && path != "sync/atomic",
					"%s imports %s",
					file,
					path,
				)
			}

			for _, d := range f.Decls {
				if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.VAR {
					for _, s := range g.Specs {
						for _, n := range s.(*ast.ValueSpec).Names {
							vars = append(vars, n.Name)
						}
					}
				}
			}

			generated := []string{"zz_gen.helper.kama_jsonschema.go", name + ".kama_jsonschema.go"}
			for _, other := range generated {
				g, err := parser.ParseFile(
					fset, filepath.Join(fixtureDir(name), other), nil, parser.SkipObjectResolution,
				)
				assert.NilError(t, err)

				ast.Inspect(g, func(n ast.Node) bool {
					var lhs []ast.Expr

					switch n := n.(type) {
					case *ast.AssignStmt:
						lhs = n.Lhs
					case *ast.IncDecStmt:
						lhs = []ast.Expr{n.X}
					case *ast.UnaryExpr:
						if n.Op == token.AND {
							lhs = []ast.Expr{n.X}
						}
					}

					for _, e := range lhs {
						if id, ok := e.(*ast.Ident); ok {
							assert.Assert(t, !slices.Contains(vars, id.Name),
								"%s assigns or takes the address of %s", other, id.Name)
						}
					}

					return true
				})
			}
		})
	}
}

// buffers reports whether body reads a whole value of the decoder or
// decodes from read bytes.
func buffers(body *ast.BlockStmt) bool {
	found := false

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			found = found || fn.Sel.Name == "ReadValue" || fn.Sel.Name == "Unmarshal"
		case *ast.Ident:
			found = found || fn.Name == "kamaJsonschemaUnmarshal" ||
				fn.Name == "kamaJsonschemaUnmarshalObject"
		}

		return true
	})

	return found
}

func wireStruct(name string) bool {
	return len(name) > len("Wire") && name[len(name)-len("Wire"):] == "Wire"
}

func isJsontextValue(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	x, ok := sel.X.(*ast.Ident)

	return ok && x.Name == "jsontext" && sel.Sel.Name == "Value"
}

// holder reports whether ts is the holder of a union: a struct of the one
// field Kind.
func holder(ts *ast.TypeSpec) bool {
	st, ok := ts.Type.(*ast.StructType)

	return ok && len(st.Fields.List) == 1 && len(st.Fields.List[0].Names) == 1 &&
		st.Fields.List[0].Names[0].Name == "Kind"
}

// productStruct reports whether ts is a product of allOf: a struct with a
// member field tagged json:"-".
func productStruct(ts *ast.TypeSpec) bool {
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		return false
	}

	for _, f := range st.Fields.List {
		if f.Tag == nil {
			continue
		}

		tag, err := strconv.Unquote(f.Tag.Value)
		if err == nil && tag == `json:"-"` {
			return true
		}
	}

	return false
}
