package base

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// Drive runs g on in and stages the files it yields into out.
//
// Drive prepares the run with [Prepare]. When that fails, it stages
// nothing and returns the error. Otherwise it stages every file of the
// plan into out, leaving a file that would change in the generated: line
// of its header only as it is, reports to env.Reporter how many files it
// wrote, and then warns through env.Reporter about every stale file.
func Drive[I any](
	ctx context.Context,
	g Generator[I],
	env Env,
	in I,
	scope Scope,
	out *overlayfs.Fs,
) error {
	plan, err := Prepare(ctx, g, env, in, scope, out)
	if err != nil {
		return err
	}

	if err := stage(ctx, out, plan.Files, env.Reporter); err != nil {
		return err
	}

	for _, w := range plan.Stale {
		env.Reporter.Warn(w)
	}

	return nil
}

// Plan is what a run of a generator stages, as [Prepare] finds it.
type Plan struct {
	// Files are the files the run stages in the order the generator yielded
	// them, with the content out stages: a Go file formatted, any other file
	// as it is.
	Files []File
	// Stale are the warnings of the stale check, each wrapping ErrStale.
	Stale []error
}

// Prepare runs g on in and returns what [Drive] stages into out, staging
// nothing itself.
//
// Prepare returns an error for an Id that is not valid or that
// env.Registry does not register. It takes every output and every error g yields, and recovers a
// panic of g as an error. It assembles each output as [Composed]
// describes and checks it: its path is valid and is not yielded twice, a
// Composed is a Go file, a Decl is not empty. It formats each file once,
// with the formatter of out. It checks that out can take each file: no
// path is yielded both as a file and as a directory of another file, no
// path names an existing directory, no directory of a path is an existing
// non-directory or a symbolic link, and a Composed replaces only a file g
// generated. It then runs the stale check of scope on the files of out.
//
// When anything of that fails, or ctx is done, Prepare returns every error
// joined.
func Prepare[I any](
	ctx context.Context,
	g Generator[I],
	env Env,
	in I,
	scope Scope,
	out *overlayfs.Fs,
) (Plan, error) {
	if err := g.Id.Validate(); err != nil {
		return Plan{}, err
	}

	// An Id the registry does not know would disable the stale check and
	// the replace check silently, since both find the marks of registered
	// generators only.
	if env.Registry == nil || !env.Registry.Registered(g.Id.String()) {
		return Plan{}, fmt.Errorf(
			"%s: the generator is not registered in env.Registry; register it with base.MustRegister",
			g.Id,
		)
	}

	if g.Generate == nil {
		return Plan{}, fmt.Errorf("%s: Generate is nil", g.Id)
	}

	outs, errs := drain(ctx, g, env, in)
	if len(errs) > 0 {
		return Plan{}, errors.Join(errs...)
	}

	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}

	files := make([]File, 0, len(outs))

	for _, o := range outs {
		content, err := out.Format(ctx, o.Path, o.Content)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: format %q: %w", g.Id, o.Path, err))
			continue
		}

		files = append(files, File{Path: o.Path, Content: content})
	}

	if len(errs) > 0 {
		return Plan{}, errors.Join(errs...)
	}

	if errs := preflight(out.FS(), g.Id, env.Registry, outs); len(errs) > 0 {
		return Plan{}, errors.Join(errs...)
	}

	stale, err := StaleCheck{Registry: env.Registry, Generator: g.Id}.InScope(
		out.FS(), scope, files,
	)
	if err != nil {
		return Plan{}, err
	}

	return Plan{Files: files, Stale: stale}, nil
}

// output is a file a run yields, before it is formatted.
type output struct {
	File
	// composed tells a Composed, which may replace only a file of its
	// generator, from a Whole.
	composed bool
}

// drain runs g on in and returns the files of its outputs and every error
// it yields or its outputs give.
func drain[I any](
	ctx context.Context,
	g Generator[I],
	env Env,
	in I,
) (outs []output, errs []error) {
	defer func() {
		// A generator is code of any author; one that panics must not take
		// the process down with the errors the run collected.
		if r := recover(); r != nil {
			errs = append(errs, fmt.Errorf("%s: panic: %v\n%s", g.Id, r, debug.Stack()))
		}
	}()

	seen := map[string]bool{}

	for out, err := range g.Generate(ctx, env, in) {
		if err != nil {
			errs = append(errs, err)
			continue
		}

		o, err := assemble(g.Id, env, out)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", g.Id, err))
			continue
		}

		if seen[o.Path] {
			errs = append(errs, fmt.Errorf("%s: %q is yielded twice", g.Id, o.Path))
			continue
		}

		seen[o.Path] = true
		outs = append(outs, o)
	}

	return outs, errs
}

// assemble returns the file of out.
func assemble(id Id, env Env, out Output) (output, error) {
	switch o := out.(type) {
	case *Whole:
		if o == nil {
			return output{}, errors.New("nil *Whole")
		}

		return assemble(id, env, *o)
	case *Composed:
		if o == nil {
			return output{}, errors.New("nil *Composed")
		}

		return assemble(id, env, *o)
	case Whole:
		if !validPath(o.Path) {
			return output{}, fmt.Errorf("invalid path %q", o.Path)
		}

		return output{File: File(o)}, nil
	case Composed:
		if !validPath(o.Path) {
			return output{}, fmt.Errorf("invalid path %q", o.Path)
		}

		if path.Ext(o.Path) != ".go" {
			return output{}, fmt.Errorf("%s: a Composed is a Go file, named *.go", o.Path)
		}

		content, err := compose(id, env, o)
		if err != nil {
			return output{}, fmt.Errorf("%s: %w", o.Path, err)
		}

		return output{Path: o.Path, Content: content, composed: true}, nil
	default:
		return output{}, fmt.Errorf("unknown output %T", out)
	}
}

// validPath reports whether p names a file under the root of the output:
// fs.ValidPath accepts the root itself, which is a directory.
func validPath(p string) bool { return fs.ValidPath(p) && p != "." }

// compose prints the unformatted content of c.
func compose(id Id, env Env, c Composed) ([]byte, error) {
	hdr, err := header(id.String(), c.Params, env.Now)
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer

	b.WriteString(strings.Join(hdr, "\n") + "\n\n")

	if c.BuildConstraint != "" {
		b.WriteString(c.BuildConstraint + "\n\n")
	}

	b.WriteString("package " + c.Package + "\n\n")

	if c.Imports != nil {
		writeImports(&b, c.Imports)
	}

	for i, part := range c.Parts {
		if i > 0 {
			b.WriteString("\n")
		}

		var content []byte

		switch p := part.(type) {
		case Decl:
			if len(bytes.TrimSpace(p)) == 0 {
				return nil, fmt.Errorf("part %d: empty Decl", i)
			}

			content = Stamp(id, p)
		case Text:
			content = p
		default:
			return nil, fmt.Errorf("unknown part %T", part)
		}

		b.Write(content)

		if !bytes.HasSuffix(content, []byte("\n")) {
			b.WriteString("\n")
		}
	}

	return b.Bytes(), nil
}

// Stamp returns decl without its leading white space and with the
// generated mark of id on a line before it.
func Stamp(id Id, decl Decl) []byte {
	return slices.Concat([]byte(id.Marker()+"\n"), bytes.TrimLeft(decl, " \t\r\n"))
}

// writeImports writes the import declaration of every import m holds: the
// standard library, an empty line, then the other packages, as goimports
// groups them. It writes nothing for no import.
func writeImports(b *bytes.Buffer, m *importdecl.Map) {
	imps := slices.Collect(m.Imports())
	if len(imps) == 0 {
		return
	}

	std := func(imp importdecl.Import) bool {
		first, _, _ := strings.Cut(imp.Package.Path, "/")
		return !strings.Contains(first, ".")
	}

	spec := func(imp importdecl.Import) string {
		if imp.Name != "" {
			return fmt.Sprintf("%s %q", imp.Name, imp.Package.Path)
		}

		return fmt.Sprintf("%q", imp.Package.Path)
	}

	if len(imps) == 1 {
		b.WriteString("import " + spec(imps[0]) + "\n\n")
		return
	}

	groups := [][]string{
		opiter.WrapSeq(slices.Values(imps)).
			Filter(std).
			Map(spec).
			CollectSlice(),
		opiter.WrapSeq(slices.Values(imps)).
			Filter(func(imp importdecl.Import) bool { return !std(imp) }).
			Map(spec).
			CollectSlice(),
	}

	b.WriteString("import (\n")

	for i, group := range slices.DeleteFunc(groups, func(g []string) bool { return len(g) == 0 }) {
		if i > 0 {
			b.WriteString("\n")
		}

		for _, s := range group {
			b.WriteString("\t" + s + "\n")
		}
	}

	b.WriteString(")\n\n")
}
