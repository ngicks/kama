package partial

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
)

// OptionFileVersion is the only version of the option file format.
const OptionFileVersion = 1

// LoadedOptions are the options of Generate. The Loader of NewLoader loads
// them.
type LoadedOptions = genopt.Loaded[PackageOptions, TypeOptions, FieldOptions]

// NewLoader returns the loader of the options of the generator. optionFile
// finds the central option files relative to the root; none are read when
// it is nil. A package without //kama:option-file partial=<path> reads
// kama_partial.yaml, .yml, .json or .toml in its directory, the first one
// present in that order.
func NewLoader(
	optionFile fileloader.Input,
) genopt.Loader[PackageOptions, TypeOptions, FieldOptions] {
	return genopt.Loader[PackageOptions, TypeOptions, FieldOptions]{
		ToolName:  toolName,
		Generator: directive,
		Version:   OptionFileVersion,
		File:      optionFile,
		PackageFile: fileloader.PathInput{
			Base: packageOptionFileBase,
			Formats: []fileloader.Format{
				fileloader.FormatYaml,
				fileloader.FormatJson,
				fileloader.FormatToml,
			},
		},
	}
}

// packageOptionFileBase is the name of the package option file, without
// extension, read from a package directory without //kama:option-file
// partial=<path>.
const packageOptionFileBase = "kama_partial"

// PackageOptions are the options of a package, from the package entry of an
// option file or from the free-floating directives of a file.
type PackageOptions struct {
	// AllTypes stands for //kama:partial package.
	AllTypes bool `json:"all_types"`
	// file stands for //kama:partial file, which option files lack.
	file bool
}

// UnmarshalKeyValue decodes a directive in a free-floating comment.
func (o *PackageOptions) UnmarshalKeyValue(
	src genopt.Source,
	sub string,
	kv []dircmnt.KeyValue,
) error {
	switch sub {
	case "":
	case "merge", "flag":
		return fmt.Errorf(
			"%w: %s: //kama:partial:%s applies to struct fields only", ErrDirective, src.Pos, sub,
		)
	case "fieldmask":
		return fmt.Errorf(
			"%w: %s: //kama:partial:fieldmask applies to types and struct fields only",
			ErrDirective, src.Pos,
		)
	default:
		return unknownDirective(src, sub)
	}

	words, ok := bareWords(kv)
	if !ok || len(words) == 0 {
		return fmt.Errorf(
			"%w: %s: unknown args %q on a free-floating comment; want file or package",
			ErrDirective, src.Pos, spellArgs(kv),
		)
	}

	for _, w := range words {
		switch w {
		case "file":
			o.file = true
		case "package":
			o.AllTypes = true
		default:
			return fmt.Errorf(
				"%w: %s: unknown args %q on a free-floating comment; want file or package",
				ErrDirective, src.Pos, spellArgs(kv),
			)
		}
	}

	return nil
}

// TypeOptions are the options of a type.
type TypeOptions struct {
	// Ignore stands for //kama:partial ignore.
	Ignore bool `json:"ignore"`
	// FieldMask stands for //kama:partial:fieldmask on the type.
	FieldMask bool `json:"fieldmask"`
	// marked stands for //kama:partial, or for an option file entry of the
	// type without ignore.
	marked bool
	// markSrc locates marked or Ignore, and maskSrc FieldMask.
	markSrc, maskSrc optionSource
}

// UnmarshalKeyValue decodes a directive on a type or a type group.
func (o *TypeOptions) UnmarshalKeyValue(
	src genopt.Source,
	sub string,
	kv []dircmnt.KeyValue,
) error {
	at := onTarget(src.Target)

	switch sub {
	case "":
		words, ok := bareWords(kv)

		switch {
		case ok && len(words) == 0:
			o.marked = true
		case ok && slices.Equal(words, []string{"ignore"}):
			o.Ignore = true
		default:
			return fmt.Errorf(
				"%w: %s: unknown args %q on %s; want none or ignore",
				ErrDirective, src.Pos, spellArgs(kv), at,
			)
		}

		o.markSrc = optionSource{src: src}
	case "fieldmask":
		if len(kv) > 0 {
			return fmt.Errorf(
				"%w: %s: unknown args %q of //kama:partial:fieldmask on %s; want none",
				ErrDirective, src.Pos, spellArgs(kv), at,
			)
		}

		o.FieldMask = true
		o.maskSrc = optionSource{src: src, sub: "fieldmask"}
	case "merge", "flag":
		return fmt.Errorf(
			"%w: %s: //kama:partial:%s applies to struct fields only", ErrDirective, src.Pos, sub,
		)
	default:
		return unknownDirective(src, sub)
	}

	return nil
}

// SetSource records src, the option file entry of the type. Listing a type
// marks it unless Ignore is set.
func (o *TypeOptions) SetSource(src genopt.Source) {
	o.marked = !o.Ignore
	o.markSrc = optionSource{src: src}

	if o.Ignore {
		o.markSrc.keys = []string{"ignore"}
	}

	if o.FieldMask {
		o.maskSrc = optionSource{src: src, sub: "fieldmask"}
	}
}

// FieldOptions are the options of a struct field.
type FieldOptions struct {
	// Merge stands for //kama:partial:merge. Empty sets no strategy.
	Merge string `json:"merge"`
	// Flag stands for //kama:partial:flag, or for none when nil.
	Flag *FlagOptions `json:"flag"`
	// FieldMask stands for //kama:partial:fieldmask on the field, or for
	// none when nil.
	FieldMask *FieldMaskOptions `json:"fieldmask"`
	// mergeSrc, flagSrc and maskSrc locate Merge, Flag and FieldMask.
	mergeSrc, flagSrc, maskSrc optionSource
}

// UnmarshalKeyValue decodes a directive on a field.
func (o *FieldOptions) UnmarshalKeyValue(
	src genopt.Source,
	sub string,
	kv []dircmnt.KeyValue,
) error {
	field := src.Target.PkgPath + "." + src.Target.Type + "." + src.Target.Field

	switch sub {
	case "":
		return fmt.Errorf(
			"%w: %s: //kama:partial on field %s; mark the type instead",
			ErrDirective,
			src.Pos,
			field,
		)
	case "merge":
		words, ok := bareWords(kv)
		if !ok || len(words) != 1 {
			return fmt.Errorf(
				"%w: %s: args %q of //kama:partial:merge on field %s; want one strategy",
				ErrDirective, src.Pos, spellArgs(kv), field,
			)
		}

		o.Merge = words[0]
		o.mergeSrc = optionSource{src: src, sub: "merge"}
	case "flag":
		f, err := flagFromKeyValues(kv)
		if err != nil {
			return fmt.Errorf(
				"%w: %s: //kama:partial:flag on field %s: %w", ErrDirective, src.Pos, field, err,
			)
		}

		o.Flag = &FlagOptions{Name: f.name, Short: f.short, Usage: f.usage}
		o.flagSrc = optionSource{src: src, sub: "flag"}
	case "fieldmask":
		args, err := keyValueMap(kv)
		if err == nil {
			_, err = maskNameFromArgs(args)
		}

		if err != nil {
			return fmt.Errorf(
				"%w: %s: //kama:partial:fieldmask on field %s: %w",
				ErrDirective,
				src.Pos,
				field,
				err,
			)
		}

		o.FieldMask = &FieldMaskOptions{Name: args["name"]}
		o.maskSrc = optionSource{src: src, sub: "fieldmask"}
	default:
		return unknownDirective(src, sub)
	}

	return nil
}

// UnmarshalJSONFrom decodes a field entry of an option file. A YAML key
// without value, as in flag: or fieldmask:, decodes to null, which would
// leave Flag or FieldMask nil. It stands for the directive without args, so
// it sets the zero options, whose missing name is then reported.
func (o *FieldOptions) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}

	// fields is FieldOptions without its methods.
	type fields FieldOptions

	var v fields
	if err := json.Unmarshal(val, &v, dec.Options()); err != nil {
		return err
	}

	var raw map[string]jsontext.Value
	if err := json.Unmarshal(val, &raw); err != nil {
		return err
	}

	if string(raw["flag"]) == "null" {
		v.Flag = &FlagOptions{}
	}

	if string(raw["fieldmask"]) == "null" {
		v.FieldMask = &FieldMaskOptions{}
	}

	*o = FieldOptions(v)

	return nil
}

// SetSource records src, the option file entry of the field.
func (o *FieldOptions) SetSource(src genopt.Source) {
	if o.Merge != "" {
		o.mergeSrc = optionSource{src: src, sub: "merge"}
	}

	if o.Flag != nil {
		o.flagSrc = optionSource{src: src, sub: "flag"}
	}

	if o.FieldMask != nil {
		o.maskSrc = optionSource{src: src, sub: "fieldmask"}
	}
}

// FieldMaskOptions are the args of //kama:partial:fieldmask on a field.
type FieldMaskOptions struct {
	Name string `json:"name"`
}

// FlagOptions are the args of //kama:partial:flag. An empty Short sets no
// shorthand.
type FlagOptions struct {
	Name  string `json:"name"`
	Short string `json:"short"`
	Usage string `json:"usage"`
}

// UnmarshalJSONFrom decodes a flag entry of an option file. The directive
// rejects an empty short=, and FlagOptions cannot tell an empty Short from
// none, so an empty short is rejected here.
func (o *FlagOptions) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var v struct {
		Name  string  `json:"name"`
		Short *string `json:"short"`
		Usage string  `json:"usage"`
	}
	if err := json.UnmarshalDecode(dec, &v); err != nil {
		return err
	}

	if v.Short != nil && *v.Short == "" {
		return errors.New("empty shorthand; omit short or give one character")
	}

	*o = FlagOptions{Name: v.Name, Usage: v.Usage}
	if v.Short != nil {
		o.Short = *v.Short
	}

	return nil
}

// optionSource locates one option for error messages.
type optionSource struct {
	src genopt.Source
	// sub is the name of the directive after //kama:partial:, or empty for
	// //kama:partial itself.
	sub string
	// keys are the keys below the entry of src that the option is spelled
	// with in an option file. sub is the key when keys is nil.
	keys []string
}

// origin locates o: the position of a directive, or the option file and
// the key path of its entry.
func (o optionSource) origin() string {
	if o.src.Kind == genopt.SourceDirective {
		return o.src.Pos.String()
	}

	src := o.src

	keys := o.keys
	if keys == nil && o.sub != "" {
		keys = []string{o.sub}
	}

	src.KeyPath = append(slices.Clip(src.KeyPath), keys...)

	return src.String()
}

// spelling names the option o sets as its source spells it.
func (o optionSource) spelling() string {
	if o.src.Kind == genopt.SourceDirective {
		if o.sub == "" {
			return "//" + toolName + ":" + directive
		}

		return "//" + toolName + ":" + directive + ":" + o.sub
	}

	name := "type entry"
	if o.sub != "" {
		name = o.sub + " option"
	}

	if o.src.Kind == genopt.SourcePackageFile {
		return "package option file " + name
	}

	return "option file " + name
}

func unknownDirective(src genopt.Source, sub string) error {
	return fmt.Errorf(
		"%w: %s: unknown directive //kama:%s:%s", ErrDirective, src.Pos, directive, sub,
	)
}

// onTarget names the type or type group a directive is on.
func onTarget(t genopt.Target) string {
	if t.Kind == genopt.TargetTypeGroup {
		return "a type group"
	}

	return "type " + t.PkgPath + "." + t.Type
}

// bareWords returns the keys of kv, or false when one of them has a value.
func bareWords(kv []dircmnt.KeyValue) ([]string, bool) {
	hasValue := opiter.WrapSeq(slices.Values(kv)).
		Any(func(kv dircmnt.KeyValue) bool { return kv.HasValue })

	if hasValue {
		return nil, false
	}

	return opiter.WrapSeq(slices.Values(kv)).
		Map(func(kv dircmnt.KeyValue) string { return kv.Key }).
		CollectSlice(), true
}

// spellArgs spells kv as space-separated key or key=value pairs.
func spellArgs(kv []dircmnt.KeyValue) string {
	spell := func(kv dircmnt.KeyValue) string {
		if kv.HasValue {
			return kv.Key + "=" + kv.Value
		}

		return kv.Key
	}

	return strings.Join(
		opiter.WrapSeq(slices.Values(kv)).
			Map(spell).
			CollectSlice(),
		" ",
	)
}

// keyValueMap returns kv keyed by name. A pair without "=" maps its name to
// "". A name given twice is an error.
func keyValueMap(kv []dircmnt.KeyValue) (map[string]string, error) {
	out := make(map[string]string, len(kv))
	for _, p := range kv {
		if _, ok := out[p.Key]; ok {
			return nil, fmt.Errorf("duplicate name %q", p.Key)
		}

		out[p.Key] = p.Value
	}

	return out, nil
}

// loadError marks err, an error of a Loader, with the errors of this
// package matching the errors of genopt it wraps.
func loadError(err error) error {
	if errors.Is(err, genopt.ErrOptionFile) || errors.Is(err, genopt.ErrPackageFile) {
		err = fmt.Errorf("%w: %w", ErrOptionFile, err)
	}

	if errors.Is(err, genopt.ErrDirective) {
		err = fmt.Errorf("%w: %w", ErrDirective, err)
	}

	return err
}
