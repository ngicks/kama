package validator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ngicks/kama/kamalib/generator/utils/genopt"
	"github.com/ngicks/kama/kamalib/generator/validator/rule"
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
func (o optionSource) origin() string { return o.src.Origin(o.arg, o.keys) }

// spelling names the option o sets as its source spells it.
func (o optionSource) spelling() string {
	return o.src.Spelling(toolName, directive, " ", o.arg)
}

// subDirectiveError reports //kama:validate:<sub>, and shows the arg of
// //kama:validate that stands for it.
func subDirectiveError(src genopt.Source, sub string, kv []dircmnt.KeyValue) error {
	want := sub

	if len(kv) > 0 {
		arg := dircmnt.SpellArgs(kv)
		// regexp takes its whole argument as the pattern. The quotes of a
		// quoted pattern therefore go. The other validators split their
		// argument into tokens again. Their quoted literals keep their quotes
		// to stay apart from bare ones.
		if sub == (rule.Regexp{}).Name() && len(kv) == 1 && !kv[0].HasValue {
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

// loadError marks err, an error of a Loader, with the errors of this
// package matching the errors of genopt it wraps.
func loadError(err error) error { return genopt.WrapLoadError(err, ErrOptionFile, ErrDirective) }
