// Package base is the framework of kama generators. A generator tells what
// to generate; the framework runs it, assembles the Go files, marks the
// generated declarations, checks the result, finds stale files and stages
// the files all at once or not at all. A third party writes its own
// generators on it with the public API of this package.
//
// # Contract
//
// A generator is a [Generator]: an [Id] and a Generate function taking a
// context, an [Env] and an input of the generator's own type. The Id is
// the namespace and the name of the generator, such as kama:partial. It
// names the directive of the generator (//kama:partial), its option file
// key (partial), its generated mark (//kama:generated kama:partial), its
// generated files (x.kama_partial.go) and its proc kind (partial.generate;
// acme.greet.generate for a namespace other than kama). A third party
// takes a namespace of its own. The directive kama:generated of the mark is
// the framework's for every generator. [Id.Validate] tells the Ids that
// can name a generator.
//
// A generator generating from a document takes an input of its own. A
// generator generating from loaded packages takes an [Input]: the
// packages, the options of their directives overlaid with those of the
// option files, the options of the step and the root of the output.
//
// A generator may also find its documents through the loaded packages: a
// package names its document with //<namespace>:option-file <name>=<path>,
// or holds the default file <namespace>_<name> in its directory, and the
// generator reads it with the loader [NewDocumentLoader] returns and
// generates into the directory [PackageDir] tells. Such a generator is a
// [Proc] whose in is the loaded packages; the generators contextkey and
// jsonschema of kamalib are.
//
// Generate yields [Output] values. A [Whole] is a file the generator
// finished, such as a file edited in place or a file in another language.
// A [Composed] is a Go file the framework assembles from [Part] values,
// each a [Decl] or a [Text]. Paths are slash-separated and relative to the
// root of the output. Generate keeps yielding after an error, so that one
// run reports every problem. It reports warnings and progress through
// Env.Reporter and reads existing files from Env.Out. Nothing assumes a
// file per package or per source file: a generator with no declarations,
// or one editing files in place, yields Whole files only.
//
// # What the framework does
//
// [Drive] runs a generator and stages its files; [Prepare] does all of it
// but the staging. A run goes through these stages:
//
//  1. Drain: Prepare takes every output and every error the generator
//     yields, and recovers a panic of the generator as an error.
//  2. Check: an output has a valid path naming a file, no path is yielded
//     twice, a Composed is named *.go, a Decl is not empty, and the Id is
//     valid and registered in Env.Registry.
//  3. Assemble: a Composed becomes the header naming the Id, its Params and
//     the time of the run, its build constraint, its package clause, the
//     import declaration of Imports, and its parts with an empty line
//     between each two. The header and the package clause are those of
//     genhelper.GeneratorParams and genhelper.PrintFileHeader, which a
//     generator writing files without the framework uses too. [Stamp]
//     prepends the generated mark to each Decl.
//  4. Format: the formatter of the output formats each Go file once, a
//     Whole named *.go included, and the framework stages exactly those
//     bytes.
//  5. Preflight: the output can take each file. No path is a file and a
//     directory of another file, names an existing directory, or goes
//     through an existing file or a symbolic link. A Composed replaces only
//     a file its generator generated: one starting with its header or
//     holding a declaration carrying its mark.
//  6. Stale check: each file of the [Scope] of the run that holds a
//     declaration carrying the mark of the generator, and that the run did
//     not yield, is stale.
//  7. Stage: when anything above failed or the context is done, Drive
//     stages nothing and returns every error joined. Otherwise it stages
//     every file, keeping a file that would change in the generated: line
//     of its header only, and then warns through Env.Reporter about each
//     stale file with an error wrapping [ErrStale].
//
// [Proc] wraps a generator in a workflow proc: it decodes the param of a
// step onto its default, builds the input of the generator and the scope
// of its stale check from the param, the in and the out of the step, and
// drives the generator into the out. [SourceProc] is the proc of a
// generator taking an [Input]. It loads the options of the packages its in
// loaded, reports the warnings of loading them, and checks the files of
// those packages for stale ones, as [Input.Scope] tells.
//
// # Registration
//
// A generator registers its Id with [MustRegister] and its proc with
// workflow.Register under [Id.ProcKind], both from an init function. The
// framework drives no generator whose Id the registry of the run does not
// know: the registry tells the marks of the registered generators from
// those of no generator, which the stale check, the replace check and the
// conflict checks of the generators rely on.
//
// A spec names the procs of its steps by kind. The stock kinds are
// overlayfs.open, opening the project as the output, packages.load,
// loading packages from the output into the *gopackages.Packages a
// SourceProc takes as its in, file.load, loading a document into the
// *fileloader.Document a document generator may take as its in, and
// overlayfs.copy_down, checking that the staged files compile and writing
// them into the project. A SourceProc rejects packages that hold errors,
// which packages.load keeps with its param allow_type_errors, since its
// generator reads their types. A generator reading the syntax trees only
// may take such packages.
//
// # Params
//
// A step decodes its param into the Partial type of the proc and merges it
// onto Default. [ParamOf] is the Partial of a param P with no partial
// variant: the step decodes onto P.Default, so a key the step leaves out
// keeps its default, and Default stays nil. A param with a partial variant,
// as kamalib's own generators have, implements [ParamPartial] with value
// receivers; its Merge lets the param of the step win.
//
// # Example: a generator generating from a document
//
// Package greet of the directory internal/example reads names from a
// document and declares a greeting function per name. It registers its Id
// and its proc:
//
//	var id = base.Id{Namespace: "acme", Name: "greet"}
//
//	func init() {
//		base.MustRegister(id)
//		workflow.Register(id.ProcKind(), func() workflow.Proc { return proc })
//	}
//
// Its param needs no partial variant:
//
//	// Default returns the values of the keys a step leaves out.
//	func (Param) Default() Param {
//		return Param{Input: "names.yaml", Dir: "greet", Package: "greet"}
//	}
//
// Its proc reads the document from the out of the step and makes the
// directory of the generated files the scope of the stale check:
//
//	var proc = base.Proc[base.ParamOf[Param], Param, base.None, Input]{
//		Generator: base.Generator[Input]{Id: id, Generate: generate},
//
// Generate spells the imports of a new file with importdecl.NewFile,
// yields a Composed holding a Text and a Decl per name, and a Whole:
//
//	c := base.Composed{
//		Path:    path.Join(in.Param.Dir, id.Naming().SharedFile("greet")),
//		Params:  map[string]any{"input": in.Param.Input},
//		Package: in.Param.Package,
//		Imports: imports,
//		Parts:   parts,
//	}
//	if !yield(c, nil) {
//		return
//	}
//
//	yield(base.Whole{
//		Path:    path.Join(in.Param.Dir, "names.txt"),
//		Content: []byte(strings.Join(in.Names, "\n") + "\n"),
//	}, nil)
//
// # Example: a generator generating from loaded packages
//
// Package foo declares a method returning a label for each type marked
// //acme:foo [label=<label>], or labelled in the option file acme_foo.yaml
// of its package. Its type options decode the directive, and
// [NewOptionLoader] loads them from the directives and the option files:
//
//	var proc = base.SourceProc[base.ParamOf[Param], Param, Param, struct{}, TypeOptions, struct{}]{
//		Generator: base.Generator[Input]{Id: id, Generate: generate},
//		Options: func(p Param) (Param, opt.Option[fileloader.PathInput], error) {
//			return p, opt.None[fileloader.PathInput](), nil
//		},
//		Loader: base.NewOptionLoader[struct{}, TypeOptions, struct{}](id, 1, nil),
//	}
//
// Generate yields a Composed per package holding marked types:
//
//	c := base.Composed{
//		Path:    path.Join(dir, base.PackageFile(pkg, id.Naming().SharedFile("foo"))),
//		Params:  in.Loaded.HeaderParams(pkg.PkgPath),
//		Package: pkg.Name,
//		Parts:   parts,
//	}
//
// # Running
//
// A spec runs both, the packages for foo loaded by a packages.load step:
//
//	version: 1
//	steps:
//	  root:
//	    proc: overlayfs.open
//	  load:
//	    proc: packages.load
//	    in: root
//	    param:
//	      patterns: [./a, ./b, ./c]
//	  greet:
//	    proc: acme.greet.generate
//	    out: root
//	    param:
//	      input: names.yaml
//	  foo:
//	    proc: acme.foo.generate
//	    in: load
//	    out: root
//	    param:
//	      method: Label
//	  final:
//	    proc: overlayfs.copy_down
//	    in: root
//	    after: [greet, foo]
//
// A command running such a spec is a main package importing the generators
// and calling Main of package kamasimple/cli. A type of b losing its
// directive leaves the file foo generated in b stale, and the next run
// warns about it. The test of internal/example runs all of it.
//
// # Limits
//
//   - The framework does not format beyond its one formatting pass, and it
//     does not check that a Decl is one declaration. [Stamp] puts the mark
//     on whatever a Decl starts with: a comment detached by an empty line
//     takes the mark and leaves the declaration unmarked, and of two
//     declarations only the first is marked.
//   - A blank or a dot import goes in a leading Text holding an import
//     declaration of its own. A path whose first element holds no dot
//     counts as the standard library in the import declaration of a
//     Composed.
//   - The framework does not delete a stale file, and it leaves a stale
//     declaration inside a file the run yields to the generator.
//   - The stale check cannot fire for a file referring to a type that no
//     longer exists: loading the packages fails on the file first, until
//     it is deleted.
//   - One step per generator and scope: two steps of one generator whose
//     scopes overlap report the files of each other as stale, and two
//     steps yielding one path overwrite each other.
//
// # Helpers
//
// Beside the framework, the package holds helpers a generator may use:
//
//   - [Naming] names generated files, and [HostShared] places declarations
//     several generated files share.
//   - [SourcePackages], [DeclaringPackage], [PackageFile], [PackageDir]
//     and [BuildConstraint] read loaded packages. SourceProc picks its packages
//     with SourcePackages.
//   - [Generated], [UnownedDecls], [DeclDocs] and [AstDeclDocs] read the
//     generated marks of declarations, and [ExplainConflict] explains a
//     conflict with a declaration carrying a mark of no registered
//     generator.
//   - [CheckPredeclared] and [MethodTypeParams] check and choose the names
//     generated code spells.
//   - [TypeGraph] and [MarkedTypeError] build the type graph of the
//     packages without the generated types.
//
// Package github.com/ngicks/kama/kamalib/generator/utils/base/filescope
// spells types for a generator building declarations as dst nodes and
// prints them as Decl parts. The framework itself does not import dst,
// directly or not. It imports package
// github.com/ngicks/kama/kamalib/pkg/genhelper, which leaves dst to its
// subpackage dstgen.
package base
