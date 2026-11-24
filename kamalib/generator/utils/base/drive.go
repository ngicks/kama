package base

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
)

// Drive runs g on in and stages the files it yields into out, whose files
// env.Out holds.
//
// Drive takes every output and every error g yields. When g yields an
// error, or an output cannot be turned into a file or does not format,
// Drive stages nothing and returns all of the errors joined. Otherwise it
// warns through
// env.Reporter about every stale file of scope, as [StaleCheck.InScope]
// tells, and stages the files with [Stage].
func Drive[I any](
	ctx context.Context,
	g Generator[I],
	env Env,
	in I,
	scope Scope,
	out *overlayfs.Fs,
) error {
	files, err := Assemble(ctx, g, env, in)
	if err != nil {
		return err
	}

	// Stage formats as it writes, file by file. Formatting every file first
	// keeps a file that fails from leaving the files before it staged.
	var formatErrs []error

	for _, f := range files {
		if _, err := out.Format(ctx, f.Path, f.Content); err != nil {
			formatErrs = append(formatErrs, fmt.Errorf("%s: format %q: %w", g.Id, f.Path, err))
		}
	}

	if len(formatErrs) > 0 {
		return errors.Join(formatErrs...)
	}

	stale, err := StaleCheck{Registry: env.Registry, Generator: g.Id}.InScope(env.Out, scope, files)
	if err != nil {
		return err
	}

	for _, w := range stale {
		env.Reporter.Warn(w)
	}

	return Stage(ctx, out, files, env.Reporter)
}

// Assemble runs g on in and returns the files of its outputs: a Whole as
// it is, and a Composed assembled as [Composed] describes, with the
// generated mark of g prepended to each Decl. A path fs.ValidPath rejects,
// and a path two outputs share, are errors.
//
// Assemble takes every output and every error g yields, and returns the
// errors joined and no file when there is any.
func Assemble[I any](ctx context.Context, g Generator[I], env Env, in I) ([]File, error) {
	var (
		files []File
		errs  []error
	)

	seen := map[string]bool{}

	for out, err := range g.Generate(ctx, env, in) {
		if err != nil {
			errs = append(errs, err)
			continue
		}

		f, err := assemble(g.Id, env, out)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", g.Id, err))
			continue
		}

		if seen[f.Path] {
			errs = append(errs, fmt.Errorf("%s: %q is yielded twice", g.Id, f.Path))
			continue
		}

		seen[f.Path] = true
		files = append(files, f)
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return files, nil
}

// assemble returns the file of out.
func assemble(id Id, env Env, out Output) (File, error) {
	switch o := out.(type) {
	case Whole:
		if !fs.ValidPath(o.Path) {
			return File{}, fmt.Errorf("invalid path %q", o.Path)
		}

		return File(o), nil
	case Composed:
		if !fs.ValidPath(o.Path) {
			return File{}, fmt.Errorf("invalid path %q", o.Path)
		}

		content, err := compose(id, env, o)
		if err != nil {
			return File{}, fmt.Errorf("%s: %w", o.Path, err)
		}

		return File{Path: o.Path, Content: content}, nil
	default:
		return File{}, fmt.Errorf("unknown output %T", out)
	}
}

// compose prints the unformatted content of c.
func compose(id Id, env Env, c Composed) ([]byte, error) {
	header, err := Header(id.String(), c.Params, env.Now)
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer

	b.WriteString(strings.Join(header, "\n") + "\n\n")

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
