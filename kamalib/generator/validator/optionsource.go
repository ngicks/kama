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
	// arg is the arg of //kama:validate that sets the option: a validator
	// name, hook=on or hook=off from a directive, hook from an option file,
	// or empty for the mark of a type.
	arg string
	// keys are the keys below the entry of src that the option is spelled
	// with in an option file. arg is the key when keys is nil.
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
	if keys == nil && o.arg != "" {
		keys = []string{o.arg}
	}

	src.KeyPath = append(slices.Clip(src.KeyPath), keys...)

	return src.String()
}

// spelling names the option o sets as its source spells it.
func (o optionSource) spelling() string {
	if o.src.Kind == genopt.SourceDirective {
		if o.arg == "" {
			return "//" + toolName + ":" + directive
		}

		return "//" + toolName + ":" + directive + " " + o.arg
	}

	name := "type entry"
	if o.arg != "" {
		name = o.arg + " option"
	}

	if o.src.Kind == genopt.SourcePackageFile {
		return "package option file " + name
	}

	return "option file " + name
}

// subDirectiveError reports //kama:validate:<sub>, and shows the arg of
// //kama:validate that stands for it.
func subDirectiveError(src genopt.Source, sub string, kv []dircmnt.KeyValue) error {
	want := sub

	if len(kv) > 0 {
		arg := spellArgs(kv)
		// regexp takes its whole argument as the pattern. The quotes of a
		// quoted pattern therefore go. The other validators split their
		// argument into tokens again. Their quoted literals keep their quotes
		// to stay apart from bare ones.
		if sub == (Regexp{}).Name() && len(kv) == 1 && !kv[0].HasValue {
			arg = kv[0].Key
		}

		want += "=" + quoteValue(arg)
	}

	return fmt.Errorf(
		"%w: %s: //%s:%s:%s; validators and hook are args of //%s:%s, as in //%s:%s %s",
		ErrDirective, src.Pos, toolName, directive, sub,
		toolName, directive, toolName, directive, want,
	)
}

// quoteValue spells s as the value of an arg: bare when it is a plain word,
// else as a Go string literal, raw when s holds double quotes.
func quoteValue(s string) string {
	plain := s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyz"+
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-") == ""

	switch {
	case plain:
		return s
	case strings.Contains(s, `"`) && !strings.Contains(s, "`"):
		return "`" + s + "`"
	}

	return strconv.Quote(s)
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

// spellArgs spells kv as space-separated key or key=value pairs, in the
// grammar dircmnt.ParseArgsKeyValue reads back. A comparison is spelled as
// written; quoting it would turn it into a key.
func spellArgs(kv []dircmnt.KeyValue) string {
	spell := func(kv dircmnt.KeyValue) string {
		switch {
		case kv.HasValue:
			return spellToken(kv.Key, kv.KeyQuoted) + "=" + spellToken(kv.Value, kv.ValueQuoted)
		case isComparison(kv):
			return kv.Key
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

// isComparison reports whether kv is a comparison, as in >=1.
func isComparison(kv dircmnt.KeyValue) bool {
	return !kv.HasValue && !kv.KeyQuoted && kv.Key != "" && strings.ContainsAny(kv.Key[:1], "<>=!")
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
