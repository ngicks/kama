package jsonschema

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/ngicks/option/und"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	gopkg "github.com/ngicks/kama/kamalib/pkg/gopackages"
	"github.com/ngicks/kama/kamalib/pkg/jsonschema"
	"github.com/ngicks/kama/kamalib/pkg/overlayfs"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
	"github.com/ngicks/kama/kamalib/proc/gopackages"
	"github.com/ngicks/kama/kamalib/workflow"
)

func init() { workflow.Register(GeneratorId().ProcKind(), NewProc) }

// ProcParam configures the proc. A spec decodes into ProcParamPartial, which
// the proc merges onto DefaultProcParam.
//
//kama:partial
type ProcParam struct {
	// TypeName names the Go type of the root schema of every package as it
	// is written, so it must be an exported Go identifier. Empty takes the
	// title of each root schema.
	TypeName string `json:"type_name"`
	// Remotes map the URIs of other documents a reference may name to the
	// files of the out of the proc holding them. The generator never fetches a
	// document from the network.
	Remotes []Remote `json:"remotes"`
	// AssertFormats makes the format keyword assert the formats the
	// validator generator has a helper for. False keeps format an
	// annotation.
	AssertFormats bool `json:"assert_formats"`
	// Validate makes the generated decoders call the Validate methods the
	// validator generator writes. A spec running the proc with it chains
	// packages.load and validate.generate after the proc. False leaves the calls
	// out, and decoding then checks no validation keyword.
	Validate bool `json:"validate"`
	// Unknown captures the keys of a param that name no field.
	Unknown map[string]any `json:",embed"`
}

// Remote maps a URI to a file of the out of the proc. With File, the URI
// names that file, or the value of the file a JSON pointer names when File
// is followed by # and the pointer, as in a.json#/0/schema. The pointer is
// percent-decoded first, as the fragment of a $ref is. With Dir, the URI is
// a prefix: a URI starting with it names the file of Dir at the rest of the
// URI, as http://example.com/schemas/ with schemas names
// schemas/pet.json for http://example.com/schemas/pet.json. The paths are
// slash-separated and relative to the root of the out.
type Remote struct {
	Uri  string `json:"uri"`
	Dir  string `json:"dir"`
	File string `json:"file"`
	// Unknown captures the keys of a remote that name no field.
	Unknown map[string]any `json:",embed"`
}

// DefaultProcParam returns the values a ProcParam takes for the keys a spec
// leaves out.
func DefaultProcParam() ProcParamPartial {
	return ProcParamPartial{Validate: und.Defined(true)}
}

// UnknownKeys implements workflow.UnknownKeyer.
func (p *ProcParamPartial) UnknownKeys() [][]string {
	paths := unknownkey.Paths(p.Unknown)

	for i, r := range p.Remotes.Value() {
		paths = append(paths, unknownkey.Paths(r.Unknown, "remotes", strconv.Itoa(i))...)
	}

	return paths
}

// proc runs Generator on the schema file of each package its in loaded and
// stages the files in the directory of the package in its out, as
// base.Proc does. The schema files are read from its out, so a file staged
// by an earlier step is read too. Its stale check covers the Go files of
// the directories of the packages.
//
//	Param(ProcParamPartial) error
//	In(*gopackages.Packages)
//	Out(*overlayfs.Fs)
var proc = base.Proc[ProcParamPartial, ProcParam, *gopackages.Packages, procIn]{
	Generator: base.Generator[procIn]{Id: GeneratorId(), Generate: generatePackages},
	Default:   DefaultProcParam,
	Check:     checkParam,
	Input:     procInput,
}

// NewProc returns the proc of the generator. It is the constructor of the kind
// jsonschema.generate in workflow.DefaultRegistry.
func NewProc() workflow.Proc { return proc }

// documents finds the schema file of a package. The file is decoded from
// its raw bytes, so it holds JSON whatever its name. A path a directive
// names may end in # and a JSON pointer naming the schema inside the file.
var documents = func() genopt.DocumentLoader {
	l := base.NewDocumentLoader(GeneratorId(), fileloader.FormatJson)
	l.Fragments = true

	return l
}()

// procIn is the input of a run of the proc.
type procIn struct {
	// pkgs are the packages the run reads, as base.SourcePackages picks
	// them.
	pkgs []*packages.Package
	// root is the absolute directory of the root of the out.
	root  string
	param ProcParam
}

func checkParam(p ProcParam) error {
	if err := checkTypeName(p.TypeName); err != nil {
		return err
	}

	return checkRemotes(p.Remotes)
}

func procInput(
	_ context.Context,
	p ProcParam,
	pkgs *gopackages.Packages,
	out *overlayfs.Fs,
) (procIn, base.Scope, error) {
	in := procIn{pkgs: base.SourcePackages(pkgs.Pkgs), root: out.Root(), param: p}

	var scope base.Scope

	// A package without a schema file still counts, so that the files
	// generated from a schema file it dropped are stale.
	for _, pkg := range in.pkgs {
		if dir, err := base.PackageDir(in.root, pkg); err == nil {
			scope.Dirs = append(scope.Dirs, dir)
		}
	}

	return in, scope, nil
}

// generatePackages runs Generate on the schema file of each package of in.
func generatePackages(
	ctx context.Context,
	env base.Env,
	in procIn,
) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		for _, pkg := range in.pkgs {
			for out, err := range generatePackage(ctx, env, in, pkg) {
				if !yield(out, err) {
					return
				}
			}
		}
	}
}

// generatePackage runs Generate on the schema file of pkg, generating into
// the directory of pkg in the package of pkg. A package has one schema
// file at most, since its declarations go to one file named after the
// package.
func generatePackage(
	ctx context.Context,
	env base.Env,
	in procIn,
	pkg *packages.Package,
) iter.Seq2[base.Output, error] {
	return func(yield func(base.Output, error) bool) {
		docs, err := documents.Documents(env.Out, in.root, pkg)

		switch {
		case err != nil:
			yield(nil, fmt.Errorf("jsonschema: %w", err))
			return
		case len(docs) == 0:
			return
		case len(docs) > 1:
			yield(nil, fmt.Errorf(
				"%w: package %s has %d schema files; want one", ErrOptions, pkg.PkgPath, len(docs),
			))

			return
		}

		dir, err := base.PackageDir(in.root, pkg)
		if err != nil {
			yield(nil, fmt.Errorf("jsonschema: %w", err))
			return
		}

		schema := docs[0].Name
		content := jsontext.Value(docs[0].Content)

		if docs[0].Fragment != "" {
			schema += "#" + docs[0].Fragment

			_, ptr, err := splitSchema(schema)
			if err == nil {
				content, err = pointedValue(content, schema, ptr)
			}

			if err != nil {
				yield(nil, fmt.Errorf("%w: %w", ErrOptions, err))
				return
			}
		}

		// The document is decoded from the raw bytes rather than through
		// fileloader, whose decoded values lose the order of members and the
		// text of numbers.
		var doc jsonschema.Schema
		if err := jsonv2.Unmarshal(content, &doc); err != nil {
			yield(nil, fmt.Errorf("jsonschema: %s: %w", schema, err))
			return
		}

		for out, err := range Generate(ctx, env, Input{Doc: &doc, Options: Options{
			Package:       pkg.Name,
			TypeName:      in.param.TypeName,
			Schema:        schema,
			Dir:           dir,
			Load:          remoteLoader(env.Out, in.param.Remotes),
			AssertFormats: in.param.AssertFormats,
			Validate:      in.param.Validate,
			ExternalTest:  gopkg.IsExternalTest(pkg),
		}}) {
			if !yield(out, err) {
				return
			}
		}
	}
}

// checkRemotes reports a remote without a URI, or naming not exactly one of
// a file and a directory, or naming an invalid path.
func checkRemotes(remotes []Remote) error {
	for i, r := range remotes {
		target := r.Dir

		if r.File != "" {
			file, _, err := splitSchema(r.File)
			if err != nil {
				return fmt.Errorf(
					"%w: remote %q: invalid file %q: %w",
					ErrOptions,
					r.Uri,
					r.File,
					err,
				)
			}

			target = file
		}

		switch {
		case r.Uri == "":
			return fmt.Errorf("%w: remote %d has no uri", ErrOptions, i)
		case (r.Dir == "") == (r.File == ""):
			return fmt.Errorf("%w: remote %q needs either dir or file", ErrOptions, r.Uri)
		case !fs.ValidPath(target):
			return fmt.Errorf("%w: remote %q: invalid path %q", ErrOptions, r.Uri, target)
		}
	}

	return nil
}

// remoteLoader returns a function loading the document of a URI from the
// file of fsys the remotes map it to. A remote with File takes its URI
// alone, and one with Dir every URI it prefixes. The longest URI of the
// remotes covering a URI wins.
func remoteLoader(fsys fs.FS, remotes []Remote) func(string) (*jsonschema.Schema, error) {
	return func(uri string) (*jsonschema.Schema, error) {
		var (
			file  string
			match int
			// ref is the File of the remote, which may hold a pointer. A #
			// in the file of a Dir belongs to its name.
			ref bool
		)

		for _, r := range remotes {
			rest, ok := strings.CutPrefix(uri, r.Uri)
			if !ok || len(r.Uri) <= match || r.File != "" && rest != "" {
				continue
			}

			file, match, ref = r.File, len(r.Uri), r.File != ""
			if r.Dir != "" {
				file = path.Join(r.Dir, rest)
			}
		}

		if file == "" {
			return nil, errors.New("no remote covers the URI")
		}

		b, err := remoteValue(fsys, file, ref)
		if err != nil {
			return nil, err
		}

		var doc jsonschema.Schema
		if err := jsonv2.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}

		return &doc, nil
	}
}

// remoteValue reads the value of file of fsys, which is the File of a
// remote when ref is true and may then hold a pointer.
func remoteValue(fsys fs.FS, file string, ref bool) ([]byte, error) {
	if ref {
		return schemaValue(fsys, file)
	}

	if !fs.ValidPath(file) {
		return nil, fmt.Errorf("invalid path %q", file)
	}

	return fs.ReadFile(fsys, file)
}

// splitSchema splits schema, the File of a remote or a schema file named
// with a fragment, at its first # into the path of the file and the JSON
// pointer of its fragment. The fragment is
// percent-decoded, as the fragment of a $ref is. A File without # points
// to the whole file.
func splitSchema(schema string) (string, jsontext.Pointer, error) {
	file, frag, _ := strings.Cut(schema, "#")

	frag, err := url.PathUnescape(frag)
	if err != nil {
		return "", "", err
	}

	ptr := jsontext.Pointer(frag)
	if !ptr.IsValid() {
		return "", "", fmt.Errorf("invalid JSON pointer %q", frag)
	}

	return file, ptr, nil
}

// schemaValue reads the file of the File of a remote from fsys and returns the
// raw value its pointer points to. The value is a slice of the file, so the
// order of members and the text of numbers survive.
func schemaValue(fsys fs.FS, schema string) (jsontext.Value, error) {
	file, ptr, err := splitSchema(schema)
	if err != nil {
		return nil, err
	}

	src, err := fs.ReadFile(fsys, file)
	if err != nil {
		return nil, err
	}

	return pointedValue(src, schema, ptr)
}

// pointedValue returns the raw value of src, the content of the file of
// schema, that ptr, the pointer of schema, points to.
func pointedValue(src []byte, schema string, ptr jsontext.Pointer) (jsontext.Value, error) {
	v := jsontext.Value(src)

	for tok := range ptr.Tokens() {
		var err error

		v, err = pointee(v, tok)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", schema, err)
		}
	}

	// A whole file is left for the decoder to report, as it may not even
	// be JSON.
	if k := v.Kind(); ptr != "" && k != '{' && k != 't' && k != 'f' {
		return nil, fmt.Errorf("%s: points to a value of kind %s, not to a schema", schema, k)
	}

	return v, nil
}

// pointee returns the member of v that the token of a JSON pointer names:
// an object member by name, or an array element by index.
func pointee(v jsontext.Value, tok string) (jsontext.Value, error) {
	switch v.Kind() {
	case '{':
		var members map[string]jsontext.Value
		if err := jsonv2.Unmarshal(v, &members); err != nil {
			return nil, err
		}

		m, ok := members[tok]
		if !ok {
			return nil, fmt.Errorf("no member %q", tok)
		}

		return m, nil
	case '[':
		var elems []jsontext.Value
		if err := jsonv2.Unmarshal(v, &elems); err != nil {
			return nil, err
		}

		// RFC 6901 spells an index in decimal without leading zeros.
		i, err := strconv.Atoi(tok)
		if err != nil || i < 0 || strconv.Itoa(i) != tok {
			return nil, fmt.Errorf("%q is no array index", tok)
		}

		if i >= len(elems) {
			return nil, fmt.Errorf("index %d out of range of %d elements", i, len(elems))
		}

		return elems[i], nil
	default:
		return nil, fmt.Errorf("token %q steps into a value of kind %s", tok, v.Kind())
	}
}
