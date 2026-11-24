// Package base is the framework of kama generators. A generator tells what
// to generate; the framework runs it, assembles the Go files, marks the
// generated declarations, finds stale files and stages the result.
//
// # Contract
//
// A generator is a [Generator]: an [Id] and a Generate function taking a
// context, an [Env] and an input of the generator's own type. A generator
// generating from loaded packages takes an [Input]: the packages, the
// options of their directives overlaid with those of the option files, the
// options of the step and the root of the output. The Id is
// the namespace and the name of the generator, such as kama:partial. It
// names the directive of the generator, its option file key, its
// generated mark //kama:generated kama:partial and its generated files. A
// third party takes a namespace of its own; the directive kama:generated
// of the mark is the framework's for every generator.
//
// Generate yields [Output] values: a [Whole] is a file the generator
// finished, staged as it is, such as a file edited in place or a file in
// another language; a [Composed] is a Go file the framework assembles from
// [Part] values, each a [Decl] or a [Text]. Generate keeps yielding after
// an error, so that one run reports every problem. It reports warnings and
// progress through Env.Reporter and reads existing files from Env.Out.
// Paths are slash-separated and relative to the root of the output.
//
// A generator with no declarations, or one editing files in place, yields
// Whole files only. Nothing assumes a file per package or per source file.
//
// # What the framework does
//
// [Drive] takes every output and every error of a run. If there is any
// error, it stages nothing and returns all of them. Otherwise it assembles
// each Composed: the header naming the Id, the Params of the file and the
// time of the run, the build constraint, the package clause, the import
// declaration of Imports, standard library first, and the parts with an
// empty line between each two. It prepends the generated mark to each
// Decl without parsing it. It warns, wrapping [ErrStale], about each file
// of the [Scope] of the run that carries the mark of the generator and
// that the run did not yield, and stages the files with [Stage]. A file
// that would change in the generated: line of its header only is kept.
//
// [Proc] wraps a generator in a workflow proc: it merges the param of a
// step onto its defaults, builds the input of the generator and its stale
// check scope from the param, the in and the out of the step, and drives
// the generator into the out. [SourceProc] is the proc of a generator
// taking an [Input]. It loads the options of the packages its in loaded,
// reports the warnings of loading them, and checks the files of those
// packages for stale ones, as [Input.Scope] tells.
//
// # What the framework does not do
//
// The framework does not format: the output formats a Go file as it stages
// it, and the formatter moves the mark, a directive comment, to the end of
// the doc comment of a Decl. It does not check that a Decl is one
// declaration. It does not delete a stale file, and it leaves a stale
// declaration inside a file the run yields to the generator.
//
// # Example
//
// A generator declaring a constant per name of its input:
//
//	var greetingId = base.Id{Namespace: "acme", Name: "greeting"}
//
//	func init() { base.MustRegister(greetingId) }
//
//	var greeting = base.Generator[[]string]{
//		Id: greetingId,
//		Generate: func(
//			_ context.Context,
//			_ base.Env,
//			names []string,
//		) iter.Seq2[base.Output, error] {
//			return func(yield func(base.Output, error) bool) {
//				var parts []base.Part
//
//				for _, name := range names {
//					if !token.IsExported(name) {
//						// The run goes on, so that every bad name is reported.
//						if !yield(nil, fmt.Errorf("greeting: %q is not exported", name)) {
//							return
//						}
//
//						continue
//					}
//
//					parts = append(parts, base.Decl(fmt.Sprintf(
//						"// %s greets %s.\nconst %s = %q\n", name, name, name, "hello, "+name,
//					)))
//				}
//
//				yield(base.Composed{
//					Path:    "greeting/greeting.go",
//					Package: "greeting",
//					Parts:   parts,
//				}, nil)
//			}
//		},
//	}
//
// A generator generating from loaded packages declares in each package a
// constant counting the files of the package:
//
//	var countId = base.Id{Namespace: "acme", Name: "count"}
//
//	func init() { base.MustRegister(countId) }
//
//	type countInput = base.Input[struct{}, struct{}, struct{}, struct{}]
//
//	var count = base.Generator[countInput]{
//		Id: countId,
//		Generate: func(
//			_ context.Context,
//			_ base.Env,
//			in countInput,
//		) iter.Seq2[base.Output, error] {
//			return func(yield func(base.Output, error) bool) {
//				for _, pkg := range in.Packages {
//					dir, err := in.Rel(filepath.Dir(pkg.GoFiles[0]))
//					if err != nil {
//						if !yield(nil, err) {
//							return
//						}
//
//						continue
//					}
//
//					c := base.Composed{
//						Path:    path.Join(dir, countId.Naming().SharedFile("count")),
//						Package: pkg.Name,
//						Parts: []base.Part{base.Decl(fmt.Sprintf(
//							"// NumFiles counts the files of the package.\nconst NumFiles = %d\n",
//							len(pkg.GoFiles),
//						))},
//					}
//					if !yield(c, nil) {
//						return
//					}
//				}
//			}
//		},
//	}
//
// # Helpers
//
// Beside the framework, the package holds helpers a generator may use. The
// framework does not call them for a generator:
//
//   - [Naming] names generated files, and [HostShared] places declarations
//     several generated files share.
//   - [SourcePackages], [DeclaringPackage], [PackageFile] and
//     [BuildConstraint] read loaded packages.
//   - [Generated], [UnownedDecls] and [DeclDocs] read the generated marks
//     of declarations.
//   - [CheckPredeclared] and [MethodTypeParams] check and choose the names
//     generated code spells.
//   - [TypeGraph] and [MarkedTypeError] build the type graph of the
//     packages without the generated types.
//
// Package github.com/ngicks/kama/kamalib/generator/utils/base/filescope
// spells types for a generator building declarations as dst nodes and
// prints them as Decl parts. The framework itself does not import dst,
// directly or not.
package base
