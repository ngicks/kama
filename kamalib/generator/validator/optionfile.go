package validator

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/token"
	"strconv"

	"github.com/ngicks/kama/kamalib/generator/utils/base"
	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
	"github.com/ngicks/kama/kamalib/pkg/fileloader"
	"github.com/ngicks/kama/kamalib/pkg/unknownkey"
)

// OptionFileVersion is the only version of the option file format.
const OptionFileVersion = 1

// LoadedOptions are the options of Generate. The Loader of NewLoader loads
// them.
type LoadedOptions = genopt.Loaded[PackageOptions, TypeOptions, FieldOptions]

// NewLoader returns the loader of the options of the generator. optionFile
// finds the central option files relative to the root; none are read when
// it is nil. A package without //kama:option-file validate=<path> reads
// kama_validate.toml, .yaml, .yml or .json in its directory.
func NewLoader(
	optionFile fileloader.Input,
) genopt.Loader[PackageOptions, TypeOptions, FieldOptions] {
	return base.NewOptionLoader[PackageOptions, TypeOptions, FieldOptions](
		GeneratorId(), OptionFileVersion, optionFile,
	)
}

// PackageOptions are the options of a package, from the package entry of an
// option file or from the free-floating directives of a file.
type PackageOptions struct {
	// AllTypes stands for //kama:validate package.
	AllTypes bool `json:"all_types"`
	// Unknown captures the keys of an option file entry that name no option.
	Unknown map[string]any `json:",embed"`
	// file stands for //kama:validate file, which option files lack.
	file bool
}

// UnknownKeys returns the keys of the option file entry that name no
// option.
func (o *PackageOptions) UnknownKeys() [][]string { return unknownkey.Paths(o.Unknown) }

// UnmarshalExpr decodes a directive in a free-floating comment.
func (o *PackageOptions) UnmarshalExpr(src genopt.Source, sub string, args []expr.Arg) error {
	if sub != "" {
		return subDirectiveError(src, sub, args)
	}

	if len(args) == 0 {
		return fmt.Errorf(
			"%w: %s: no args on a free-floating comment; want file or package",
			ErrDirective, src.Pos,
		)
	}

	for _, a := range args {
		switch {
		case isWord(a, ext.FileWord):
			o.file = true
		case isWord(a, ext.PackageWord):
			o.AllTypes = true
		default:
			return fmt.Errorf(
				"%w: %s: unknown arg %s on a free-floating comment; want file or package",
				ErrDirective, src.Pos, a,
			)
		}
	}

	return nil
}

// isWord reports whether a is the bare name word.
func isWord(a expr.Arg, word string) bool {
	c, ok := a.Value.(expr.Call)
	return ok && a.Name == "" && c.Name == word && !c.Parens && len(c.Args) == 0
}

// TypeOptions are the options of a type.
type TypeOptions struct {
	// Ignore stands for //kama:validate ignore.
	Ignore bool `json:"ignore"`
	// Decoded stands for //kama:validate decoded: decoding a value of the
	// type validates it, so ValidateShallow takes a decoded value of it as
	// valid.
	Decoded bool `json:"decoded"`
	// Hook stands for //kama:validate hook=True or hook=False. Nil leaves
	// the default.
	Hook *bool `json:"hook"`
	// Pre stands for //kama:validate pre="<func>": the package-level
	// function Validate calls first. Empty stands for none.
	Pre string `json:"pre"`
	// Post stands for //kama:validate post="<func>": the package-level
	// function Validate calls last, whatever happened before. Empty stands
	// for none.
	Post string `json:"post"`
	// Validators stand for the validators //kama:validate names in the doc
	// comment of the type, in the order written. They validate the value of
	// the type itself.
	Validators []ValidatorOptions `json:"validators"`
	// Unknown captures the keys of an option file entry that name no option.
	Unknown map[string]any `json:",embed"`
	// marked stands for //kama:validate without ignore, or for an option
	// file entry of the type without ignore.
	marked bool
	// markSrc locates marked or Ignore, hookSrc Hook, preSrc Pre and postSrc
	// Post.
	markSrc, hookSrc, preSrc, postSrc optionSource
}

// UnmarshalExpr decodes a directive on a type or a type group. Every
// directive without ignore marks the type, as an option file entry does.
// Each item other than ignore, decoded, hook, pre and post is a rule.
func (o *TypeOptions) UnmarshalExpr(src genopt.Source, sub string, args []expr.Arg) error {
	if sub != "" {
		return subDirectiveError(src, sub, args)
	}

	for _, a := range args {
		c, isCall := a.Value.(expr.Call)
		word := isCall && a.Name == "" && (c.Name == ext.IgnoreWord || c.Name == ext.DecodedWord)

		switch {
		case a.Name == "" &&
			(isWord(a, ext.HookWord) || isWord(a, ext.PreWord) || isWord(a, ext.PostWord)):
			return fmt.Errorf(
				"%w: %s: item %s on %s is a keyword item; write %s=<value>",
				ErrDirective, src.Pos, a, src.Target.Describe(), c.Name,
			)
		case word && (c.Parens || len(c.Args) > 0):
			return fmt.Errorf(
				"%w: %s: unknown arg %s on %s; want %s without args",
				ErrDirective, src.Pos, a, src.Target.Describe(), c.Name,
			)
		case word && c.Name == ext.IgnoreWord:
			o.Ignore = true
		case word:
			o.Decoded = true
		case a.Name == ext.HookWord:
			if err := setHook(&o.Hook, &o.hookSrc, src, a); err != nil {
				return err
			}
		case a.Name == ext.PreWord:
			if err := setFunc(&o.Pre, &o.preSrc, src, a); err != nil {
				return err
			}
		case a.Name == ext.PostWord:
			if err := setFunc(&o.Post, &o.postSrc, src, a); err != nil {
				return err
			}
		default:
			o.Validators = append(o.Validators, newValidatorOptions(src, a))
		}
	}

	o.marked = !o.Ignore
	o.markSrc = optionSource{src: src}

	return nil
}

// SetSource records src, the option file entry of the type. Listing a type
// marks it unless Ignore is set.
func (o *TypeOptions) SetSource(src genopt.Source) {
	o.marked = !o.Ignore
	o.markSrc = optionSource{src: src}

	if o.Ignore {
		o.markSrc.keys = []string{ext.IgnoreWord}
	}

	if o.Hook != nil {
		o.hookSrc = optionSource{src: src, arg: ext.HookWord}
	}

	if o.Pre != "" {
		o.preSrc = optionSource{src: src, arg: ext.PreWord}
	}

	if o.Post != "" {
		o.postSrc = optionSource{src: src, arg: ext.PostWord}
	}

	setValidatorSources(o.Validators, src)
}

// OverlayOn returns lower with the functions of o set on it when o, an
// option file entry, sets pre or post and nothing else, and lower sets
// neither of those o sets. A type then keeps the validators a generator
// wrote into its doc comment, and an option file adds its own checks
// beside them.
func (o TypeOptions) OverlayOn(lower TypeOptions) (TypeOptions, bool) {
	onlyFuncs := !o.Ignore && !o.Decoded && o.Hook == nil && len(o.Validators) == 0 &&
		(o.Pre != "" || o.Post != "")
	if !onlyFuncs || o.Pre != "" && lower.Pre != "" || o.Post != "" && lower.Post != "" {
		return TypeOptions{}, false
	}

	if o.Pre != "" {
		lower.Pre, lower.preSrc = o.Pre, o.preSrc
	}

	if o.Post != "" {
		lower.Post, lower.postSrc = o.Post, o.postSrc
	}

	return lower, true
}

// UnknownKeys returns the keys of the option file entry that name no
// option.
func (o *TypeOptions) UnknownKeys() [][]string { return unknownkey.Paths(o.Unknown) }

// FieldOptions are the options of a struct field.
type FieldOptions struct {
	// Hook stands for //kama:validate hook=True or hook=False. Nil leaves
	// the default of the type.
	Hook *bool `json:"hook"`
	// Validators stand for the validators //kama:validate names on the
	// field, in the order written.
	Validators []ValidatorOptions `json:"validators"`
	// Unknown captures the keys of an option file entry that name no option.
	Unknown map[string]any `json:",embed"`
	hookSrc optionSource
}

// UnmarshalExpr decodes a directive on a field. Each item is a rule,
// except hook.
func (o *FieldOptions) UnmarshalExpr(src genopt.Source, sub string, args []expr.Arg) error {
	if sub != "" {
		return subDirectiveError(src, sub, args)
	}

	if len(args) == 0 {
		return fmt.Errorf(
			"%w: %s: //kama:validate on field %s names no validator;"+
				" mark the type in its doc comment instead",
			ErrDirective, src.Pos, src.Target.FieldName(),
		)
	}

	for _, a := range args {
		if isWord(a, ext.HookWord) {
			return fmt.Errorf(
				"%w: %s: item hook on field %s is a keyword item; write hook=True or hook=False",
				ErrDirective, src.Pos, src.Target.FieldName(),
			)
		}

		switch a.Name {
		case ext.PreWord, ext.PostWord:
			return fmt.Errorf(
				"%w: %s: %s on field %s; write it in the doc comment of the type",
				ErrDirective, src.Pos, a.Name, src.Target.FieldName(),
			)
		case ext.HookWord:
			if err := setHook(&o.Hook, &o.hookSrc, src, a); err != nil {
				return err
			}
		default:
			o.Validators = append(o.Validators, newValidatorOptions(src, a))
		}
	}

	return nil
}

// SetSource records src, the option file entry of the field.
func (o *FieldOptions) SetSource(src genopt.Source) {
	if o.Hook != nil {
		o.hookSrc = optionSource{src: src, arg: ext.HookWord}
	}

	setValidatorSources(o.Validators, src)
}

// UnknownKeys returns the keys of the option file entry that name no
// option.
func (o *FieldOptions) UnknownKeys() [][]string { return unknownkey.Paths(o.Unknown) }

// setValidatorSources records src, the option file entry listing vs, in
// each of vs.
func setValidatorSources(vs []ValidatorOptions, src genopt.Source) {
	for i := range vs {
		vs[i].src = optionSource{
			src: src, arg: itemSpelling(vs[i].Item), keys: []string{"validators", strconv.Itoa(i)},
		}
	}
}

// ValidatorOptions are one rule item of a field or a type: a rule, as
// length(ge=1) or a or b, or a keyword item. In an option file it is the
// text of the item, as a directive spells it.
type ValidatorOptions struct {
	Item expr.Arg
	src  optionSource
}

// newValidatorOptions returns the rule item a of the directive at src.
func newValidatorOptions(src genopt.Source, a expr.Arg) ValidatorOptions {
	return ValidatorOptions{Item: a, src: optionSource{src: src, arg: itemSpelling(a)}}
}

// itemSpelling names the item a in messages: the name of a call, else a as
// expr.Print spells it.
func itemSpelling(a expr.Arg) string {
	if c, ok := a.Value.(expr.Call); ok && a.Name == "" {
		return c.Name
	}

	return a.String()
}

// UnmarshalJSONFrom decodes a rule item of an option file, a string
// holding one item. An object, the entry of an older format, is an error.
func (o *ValidatorOptions) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() != '"' {
		return errors.New(`want the text of one rule item, as in "length(ge=1)"`)
	}

	var text string
	if err := json.UnmarshalDecode(dec, &text); err != nil {
		return err
	}

	args, err := expr.Parse(text)
	if err != nil {
		return err
	}

	if len(args) != 1 {
		return fmt.Errorf("%q holds %d items; want one rule item per entry", text, len(args))
	}

	*o = ValidatorOptions{Item: args[0]}

	return nil
}

// MarshalJSONTo encodes o as the text of its item.
func (o ValidatorOptions) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(o.Item.String()))
}

// setFunc sets *dst, the function the item a of the directive at src
// names, and *dstSrc, its source. a names one function, given once on the
// type.
func setFunc(dst *string, dstSrc *optionSource, src genopt.Source, a expr.Arg) error {
	if *dst != "" {
		return fmt.Errorf(
			"%w: %s: %s given twice on %s", ErrDirective, src.Pos, a.Name, src.Target.Describe(),
		)
	}

	name, ok := a.Value.(expr.String)
	if !ok || !token.IsIdentifier(name.Value) {
		return fmt.Errorf(
			"%w: %s: arg %s of //kama:validate on %s; want %s=\"<function name>\"",
			ErrDirective, src.Pos, a, src.Target.Describe(), a.Name,
		)
	}

	*dst = name.Value
	*dstSrc = optionSource{src: src, arg: a.String()}

	return nil
}

// setHook sets *dst, the switch of the hook the item a of the directive at
// src gives, and *dstSrc, its source. a is hook=True or hook=False, given
// once.
func setHook(dst **bool, dstSrc *optionSource, src genopt.Source, a expr.Arg) error {
	if *dst != nil {
		return fmt.Errorf(
			"%w: %s: hook given twice on %s", ErrDirective, src.Pos, src.Target.Describe(),
		)
	}

	on, ok := a.Value.(expr.Bool)
	if !ok {
		return fmt.Errorf(
			"%w: %s: arg %s of //kama:validate on %s; want hook=True or hook=False",
			ErrDirective, src.Pos, a, src.Target.Describe(),
		)
	}

	*dst = &on.Value
	*dstSrc = optionSource{src: src, arg: a.String()}

	return nil
}
