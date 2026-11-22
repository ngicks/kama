package validator

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt"
)

// optionSource locates one option for error messages.
type optionSource struct {
	src genopt.Source
	// sub is the name of the directive after //kama:validate:, or empty for
	// //kama:validate itself.
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

// onTarget names the type or type group a directive is on.
func onTarget(t genopt.Target) string {
	if t.Kind == genopt.TargetTypeGroup {
		return "a type group"
	}

	return "type " + t.PkgPath + "." + t.Type
}

// fieldName names the field a directive is on.
func fieldName(t genopt.Target) string { return t.PkgPath + "." + t.Type + "." + t.Field }

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

// spellArgs spells kv as space-separated key or key=value pairs, in the
// grammar dircmnt.ParseArgsKeyValue reads back.
func spellArgs(kv []dircmnt.KeyValue) string {
	spell := func(kv dircmnt.KeyValue) string {
		if kv.HasValue {
			return spellToken(kv.Key, kv.KeyQuoted) + "=" + spellToken(kv.Value, kv.ValueQuoted)
		}

		return spellToken(kv.Key, kv.KeyQuoted)
	}

	return strings.Join(
		opiter.WrapSeq(slices.Values(kv)).
			Map(spell).
			CollectSlice(),
		" ",
	)
}

// spellToken quotes s when it was quoted or a bare token cannot hold it.
func spellToken(s string, quoted bool) string {
	if quoted || s == "" || strings.ContainsAny(s, " \t\"`") {
		return strconv.Quote(s)
	}

	return s
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
