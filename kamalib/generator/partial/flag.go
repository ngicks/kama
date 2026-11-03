package partial

import (
	"bytes"
	"errors"
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"golang.org/x/tools/go/packages"

	"github.com/ngicks/kama/kamalib/pkg/genmark"
	"github.com/ngicks/kama/kamalib/pkg/importdecl"
	"github.com/ngicks/kama/kamalib/pkg/kamaopt"
	"github.com/ngicks/kama/kamalib/pkg/typegraph"
)

// FlagSetFile is the name of the file declaring the flag set interface
// the flag methods of a package take. The generator writes it again on
// every run into every package holding targets.
const FlagSetFile = "kama_partial_flagset.go"

// flagSetName is the interface declared in FlagSetFile.
const flagSetName = "PartialFlagSet"

// flagMethodNames are the methods a partial type with flag options gets.
var flagMethodNames = []string{"RegisterFlags", "FromFlagSet"}

// flagSetParams are the parameter names of the methods of flagSetName,
// which import names must not shadow.
var flagSetParams = []string{"name", "shorthand", "value", "usage"}

// flagOption is a decoded //kama:partial:flag directive or option file
// entry.
type flagOption struct {
	name, short, usage string
	raw                kamaopt.Raw
}

func (o flagOption) same(other flagOption) bool {
	return o.name == other.name && o.short == other.short && o.usage == other.usage
}

func (o flagOption) String() string {
	return fmt.Sprintf("name=%q short=%q usage=%q", o.name, o.short, o.usage)
}

// parseFlag decodes the args of //kama:partial:flag.
func parseFlag(args string) (flagOption, error) {
	kv, err := kamaopt.ParseArgs(args)
	if err != nil {
		return flagOption{}, err
	}
	return flagFromArgs(kv)
}

// flagFromOptions decodes an option file flag entry as the directive with
// the same args.
func flagFromOptions(f FlagOptions) (flagOption, error) {
	kv := map[string]string{"name": f.Name, "usage": f.Usage}
	if f.Short != "" {
		kv["short"] = f.Short
	}
	return flagFromArgs(kv)
}

// flagFromArgs validates the args of //kama:partial:flag, keyed by name.
func flagFromArgs(kv map[string]string) (flagOption, error) {
	for _, k := range slices.Sorted(maps.Keys(kv)) {
		switch k {
		case "name", "short", "usage":
		default:
			return flagOption{}, fmt.Errorf("unknown key %q; want name, short or usage", k)
		}
	}
	o := flagOption{name: kv["name"], short: kv["short"], usage: kv["usage"]}
	switch {
	case o.name == "":
		return flagOption{}, errors.New("name is required")
	case strings.HasPrefix(o.name, "-") || strings.ContainsFunc(o.name, invalidFlagRune):
		return flagOption{}, fmt.Errorf(
			"name %q; want no leading -, no space, no = and no control character", o.name,
		)
	}
	// pflag measures a shorthand in bytes and panics on more than one, so
	// a single non-ASCII rune is rejected too.
	if _, ok := kv["short"]; ok &&
		(len(o.short) != 1 || o.short == "-" || invalidFlagRune(rune(o.short[0])) ||
			o.short[0] >= 0x80) {
		return flagOption{}, fmt.Errorf(
			"short %q; want a single ASCII character other than -, space, = and control characters",
			o.short,
		)
	}
	return o, nil
}

func invalidFlagRune(r rune) bool {
	return r == '=' || unicode.IsSpace(r) || unicode.IsControl(r)
}

// flagKind is a flag type of the flag set, and the methods defining and
// reading a flag of it.
type flagKind struct {
	define, get string
	// value spells the flag value type in FlagSetFile, where $time. is
	// replaced by the identifier of the time import.
	value string
	zero  string
	// text parses the value with UnmarshalText.
	text bool
}

var (
	flagString   = flagKind{define: "StringP", get: "GetString", value: "string", zero: `""`}
	flagBool     = flagKind{define: "BoolP", get: "GetBool", value: "bool", zero: "false"}
	flagInt      = flagKind{define: "IntP", get: "GetInt", value: "int", zero: "0"}
	flagInt64    = flagKind{define: "Int64P", get: "GetInt64", value: "int64", zero: "0"}
	flagUint     = flagKind{define: "UintP", get: "GetUint", value: "uint", zero: "0"}
	flagUint64   = flagKind{define: "Uint64P", get: "GetUint64", value: "uint64", zero: "0"}
	flagFloat64  = flagKind{define: "Float64P", get: "GetFloat64", value: "float64", zero: "0"}
	flagDuration = flagKind{
		define: "DurationP", get: "GetDuration", value: "$time.Duration", zero: "0",
	}
	flagStringSlice = flagKind{
		define: "StringSliceP", get: "GetStringSlice", value: "[]string", zero: "nil",
	}
	flagStringToString = flagKind{
		define: "StringToStringP", get: "GetStringToString", value: "map[string]string",
		zero: "nil",
	}
	flagText = flagKind{
		define: "StringP", get: "GetString", value: "string", zero: `""`, text: true,
	}
)

var basicFlagKinds = map[types.BasicKind]flagKind{
	types.String:  flagString,
	types.Bool:    flagBool,
	types.Int:     flagInt,
	types.Int64:   flagInt64,
	types.Uint:    flagUint,
	types.Uint64:  flagUint64,
	types.Float64: flagFloat64,
}

// flagKinds are every flag kind a field can take.
var flagKinds = []flagKind{
	flagString, flagBool, flagInt, flagInt64, flagUint, flagUint64, flagFloat64,
	flagDuration, flagStringSlice, flagStringToString, flagText,
}

// flagSetSignatures returns the methods of flagSetName, keyed by name: the
// methods every flag kind calls, with the signatures of the methods of
// *pflag.FlagSet.
func flagSetSignatures() map[string]string {
	out := map[string]string{"Changed": "Changed(name string) bool"}
	for _, k := range flagKinds {
		out[k.define] = fmt.Sprintf(
			"%s(name, shorthand string, value %s, usage string) *%s", k.define, k.value, k.value,
		)
		out[k.get] = fmt.Sprintf("%s(name string) (%s, error)", k.get, k.value)
	}
	return out
}

// flagField is a field with a flag option.
type flagField struct {
	flagOption
	kind flagKind
	// conv spells the type a flag value converts to, or is empty when the
	// value has the type the field holds.
	conv string
}

// textUnmarshaler is encoding.TextUnmarshaler, built here since the
// packages a target refers to need not import encoding.
var textUnmarshaler = types.NewInterfaceType([]*types.Func{
	types.NewFunc(0, nil, "UnmarshalText", types.NewSignatureType(
		nil, nil, nil,
		types.NewTuple(types.NewVar(0, nil, "text", types.NewSlice(types.Typ[types.Byte]))),
		types.NewTuple(types.NewVar(0, nil, "", types.Universe.Lookup("error").Type())),
		false,
	)),
}, nil).Complete()

// flag returns the flag of the field info of n described by o, or nil
// when o is no directive.
func (p *mapper) flag(n *typegraph.Node, info fieldInfo, o flagOption) (*flagField, error) {
	if o.raw.Directive == "" {
		return nil, nil
	}
	bad := func(format string, args ...any) error {
		return fmt.Errorf(
			"%w: %s: %s on field %s.%s.%s: "+format,
			append([]any{
				ErrDirective, origin(o.raw), spelling(o.raw),
				n.Id.PackagePath, n.Id.TypeName, info.name,
			}, args...)...,
		)
	}

	var t types.Type
	switch {
	case info.conv == convSkip:
		return nil, bad("a blank field cannot be set")
	case info.class == classSub || info.class == classSubSlice || info.class == classSubMap ||
		info.class == classEmbedded:
		return nil, bad("the field holds a partial value; " +
			"flags apply to fields of the partial type itself, not to nested partial values")
	case info.class == classElastic:
		return nil, bad("unsupported type %s", info.source)
	case info.conv == convPointer:
		t = deref(info.source)
	case info.conv == convOption || info.conv == convCopy:
		t = types.Unalias(info.source).(*types.Named).TypeArgs().At(0)
	default:
		t = info.source
	}

	f := &flagField{flagOption: o}
	want, ok := f.setKind(t)
	if !ok {
		return nil, bad("unsupported type %s", t)
	}
	if !types.Identical(t, want) {
		expr, err := p.print(t)
		if err != nil {
			return nil, err
		}
		if f.conv, err = exprString(expr); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// setKind sets the kind of the flag of a field holding t and returns the
// type of the values of the kind, which t converts from.
//
// time.Duration is checked before the underlying types since its
// underlying int64 would make it an int64 flag. A TextUnmarshaler is
// checked before them too, as its text form is what users type.
func (f *flagField) setKind(t types.Type) (types.Type, bool) {
	if isNamed(t, "time", "Duration") {
		f.kind = flagDuration
		return t, true
	}
	if _, isPtr := t.Underlying().(*types.Pointer); !isPtr &&
		types.Implements(types.NewPointer(t), textUnmarshaler) {
		f.kind = flagText
		return t, true
	}
	str := types.Typ[types.String]
	switch u := t.Underlying().(type) {
	case *types.Basic:
		kind, ok := basicFlagKinds[u.Kind()]
		f.kind = kind
		return types.Typ[u.Kind()], ok
	case *types.Slice:
		f.kind = flagStringSlice
		return types.NewSlice(str), types.Identical(u.Elem(), str)
	case *types.Map:
		f.kind = flagStringToString
		return types.NewMap(str, str), types.Identical(u.Key(), str) &&
			types.Identical(u.Elem(), str)
	}
	return nil, false
}

// flagMethods writes RegisterFlags and FromFlagSet for the fields with a
// flag option. It writes nothing when there are none.
func (w *methodWriter) flagMethods(fields []fieldInfo) {
	var flags []fieldInfo
	for _, f := range fields {
		if f.flag != nil {
			flags = append(flags, f)
		}
	}
	if len(flags) == 0 {
		return
	}
	w.p.refs[flagSetName] = true
	p, f, v, err := w.l["p"], w.l["f"], w.l["v"], w.l["err"]

	w.doc("RegisterFlags", "defines on "+f+" a flag for each field of p with a flag\n"+
		"// option. Every flag defaults to the zero value; FromFlagSet reads the\n"+
		"// flags back after "+f+" is parsed.")
	w.line("func (%s *%s) RegisterFlags(%s %s) {", p, w.recv, f, flagSetName)
	for _, fl := range flags {
		k := fl.flag.kind
		w.line("%s.%s(%s, %s, %s, %s)", f, k.define,
			strconv.Quote(fl.flag.name), strconv.Quote(fl.flag.short), k.zero,
			strconv.Quote(fl.flag.usage))
	}
	w.line("}")

	w.doc("FromFlagSet", "sets each field of p with a flag option to the value of\n"+
		"// its flag when the flag was set on "+f+". Other fields are left as they\n"+
		"// are, so merging p onto another partial value keeps the fields of the\n"+
		"// flags not set.")
	w.line("func (%s *%s) FromFlagSet(%s %s) error {", p, w.recv, f, flagSetName)
	for _, fl := range flags {
		k := fl.flag.kind
		name := strconv.Quote(fl.flag.name)
		fail := "return " + w.call("kamaPartialFlagError", name, err)
		lhs := p + "." + fl.partial
		w.line("if %s.Changed(%s) {", f, name)
		w.line("%s, %s := %s.%s(%s)", v, err, f, k.get, name)
		w.line("if %s != nil {\n%s\n}", err, fail)
		switch {
		case k.text:
			w.line("%s = %s", err, w.call("kamaPartialFromText", "&"+lhs, v))
			w.line("if %s != nil {\n%s\n}", err, fail)
		case fl.flag.conv != "":
			w.line("%s = %s", lhs, w.call("kamaPartialFromValue", fl.flag.conv+"("+v+")"))
		default:
			w.line("%s = %s", lhs, w.call("kamaPartialFromValue", v))
		}
		w.line("}")
	}
	w.line("return nil")
	w.line("}")
}

// checkFlags reports duplicate flag names and shorthands among fields.
func checkFlags(n *typegraph.Node, fields []fieldInfo) error {
	names := map[string]fieldInfo{}
	shorts := map[string]fieldInfo{}
	for _, f := range fields {
		if f.flag == nil {
			continue
		}
		for _, c := range []struct {
			what, key string
			seen      map[string]fieldInfo
		}{
			{"name", f.flag.name, names},
			{"shorthand", f.flag.short, shorts},
		} {
			if c.key == "" {
				continue
			}
			if prev, ok := c.seen[c.key]; ok {
				return fmt.Errorf(
					"%w: %s: %s on field %s.%s.%s: flag %s %q is taken by field %s (%s)",
					ErrDirective,
					origin(f.flag.raw),
					spelling(f.flag.raw),
					n.Id.PackagePath,
					n.Id.TypeName,
					f.name,
					c.what,
					c.key,
					prev.name,
					origin(prev.flag.raw),
				)
			}
			c.seen[c.key] = f
		}
	}
	return nil
}

// renderFlagSet prints FlagSetFile of pkg declaring flagSetName.
func renderFlagSet(pkg *packages.Package) ([]byte, error) {
	sigs := flagSetSignatures()
	parser := importdecl.NewParser(pkg.Types)
	parser.Reserve(types.Universe.Names()...)
	parser.Reserve(pkg.Types.Scope().Names()...)
	parser.Reserve(flagSetParams...)

	var usesTime bool
	for _, s := range sigs {
		usesTime = usesTime || strings.Contains(s, "$time.")
	}
	if usesTime {
		err := parser.Require(
			importdecl.Import{Package: importdecl.Package{Path: "time", Name: "time"}},
		)
		if err != nil {
			return nil, err
		}
	}
	m, err := parser.ParseDst(nil)
	if err != nil {
		return nil, err
	}
	var pairs []string
	if usesTime {
		ident, ok := m.Ident("time")
		if !ok {
			return nil, fmt.Errorf("partial: unknown package %q", "time")
		}
		pairs = append(pairs, "$time.", ident+".")
	}
	replacer := strings.NewReplacer(pairs...)

	var src strings.Builder
	fmt.Fprintf(&src, "package %s\n\n", pkg.Name)
	fmt.Fprintf(&src, "// %s is the flag set the RegisterFlags and FromFlagSet methods of\n"+
		"// the package take. *FlagSet of github.com/spf13/pflag implements it.\n//\n%s\n",
		flagSetName, genmark.Marker(GeneratorName))
	fmt.Fprintf(&src, "type %s interface {\n", flagSetName)
	for _, name := range slices.Sorted(maps.Keys(sigs)) {
		src.WriteString(replacer.Replace(sigs[name]) + "\n")
	}
	src.WriteString("}\n")

	f, err := decorator.Parse(src.String())
	if err != nil {
		return nil, fmt.Errorf("partial: parse flag set of %s: %w", pkg.PkgPath, err)
	}
	f.Decs.Start = dst.Decorations{header, "\n"}
	m.AddMissingDst(f)

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, f); err != nil {
		return nil, fmt.Errorf("partial: print flag set of %s: %w", pkg.PkgPath, err)
	}
	return buf.Bytes(), nil
}
