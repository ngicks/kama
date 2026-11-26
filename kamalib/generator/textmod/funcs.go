package textmod

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/ngicks/go-common/exver"
	"golang.org/x/tools/go/packages"
)

// Run is the data of one run that a [FuncProvider] binds its functions to.
type Run struct {
	// Packages are the packages the run reads, as base.SourcePackages picks
	// them.
	Packages []*packages.Package
	// Vars are the caller values of the run, as the param vars gives them.
	Vars map[string]any
}

// FuncProvider returns template functions bound to the data of one run.
// A run calls it once, before any template is parsed. The proc also calls
// it with an empty Run to parse the templates of a param in a dry run.
// The names of the functions must not depend on the Run, and a function
// reads the Run when it is called.
type FuncProvider func(r Run) template.FuncMap

var (
	providersMu sync.Mutex
	providers   = map[string]FuncProvider{}
)

// RegisterFuncs adds the functions p returns to the templates of every
// run. Another package calls it from an init function. name identifies
// the provider. RegisterFuncs panics when name is registered already.
// A run fails when two providers, or a provider and the built-in
// functions, define one function name.
func RegisterFuncs(name string, p FuncProvider) {
	providersMu.Lock()
	defer providersMu.Unlock()

	if _, ok := providers[name]; ok {
		panic(fmt.Sprintf("textmod: funcs %q registered twice", name))
	}

	providers[name] = p
}

// Funcs returns the template functions of a run on r: the built-in
// functions of [BuiltinFuncs] and those of every registered provider.
func Funcs(r Run) (template.FuncMap, error) {
	out := BuiltinFuncs()
	owner := map[string]string{}

	for name := range out {
		owner[name] = "the built-in functions"
	}

	providersMu.Lock()
	defer providersMu.Unlock()

	var errs []error

	for _, pname := range slices.Sorted(maps.Keys(providers)) {
		for fname, fn := range providers[pname](r) {
			if prev, ok := owner[fname]; ok {
				errs = append(errs, fmt.Errorf(
					"textmod: function %q of funcs %q is defined by %s too", fname, pname, prev,
				))

				continue
			}

			owner[fname] = fmt.Sprintf("funcs %q", pname)
			out[fname] = fn
		}
	}

	return out, errors.Join(errs...)
}

// BuiltinFuncs returns the built-in template functions. Every function is
// pure: none reads the clock, the environment or a file. A function taking
// the value a pipeline passes takes it as its last argument, as in
// {{ .Value | withPre "devel" }}. text/template adds its own functions,
// such as printf, len, index, eq and lt.
//
// Versions take an exver.Version or a string parsing as one:
//
//	parseVer s              exver.Version of s
//	verCompare a b          -1, 0 or 1 as a is before, equal to or after b
//	withPre pre v           v with the pre-release pre; "" clears it
//	withBuild build v       v with the build metadata build; "" clears it
//	withV v                 v with the v prefix
//	withoutV v              v without the v prefix
//	withCore n... v         v with the core of the 1 to 4 numbers n
//	verMajor v              the major number of v; verMinor, verPatch and
//	                        verExtra read the others
//	verPre v                the pre-release of v; verBuild reads the build
//
// These map to the methods of exver.Core and exver.Version:
//
//	setMajor n v            v with the major number n (Core.WithMajor);
//	                        setMinor, setPatch and setExtra set the others
//	                        and add the component v lacks
//	dropMinor v             v with the major number only (Core.DropMinor);
//	                        dropPatch keeps major and minor, dropExtra keeps
//	                        major, minor and patch
//	dropPre v               v without its pre-release (DropPreRelease)
//	dropBuild v             v without its build metadata (DropBuild)
//
// exver has no bump helper. A template bumps a version with them, as in
// {{ .Value | setMinor (add (verMinor .Value) 1) | setPatch 0 | dropPre }}.
//
// Date-times take a time.Time or a string parsing as RFC 3339. A
// duration is a time.Duration or a string time.ParseDuration takes:
//
//	parseTime layout s      the time s spells in layout
//	formatTime layout t     t spelled in layout
//	rfc3339 t               t spelled in time.RFC3339Nano
//	utc t                   t in UTC
//	addDuration d t         t plus d
//	addDate y m d t         t plus y years, m months and d days
//	truncate d t            t rounded down to a multiple of d
//
// Numbers take an integer, a float, a *big.Int or a string spelling a
// number. An operation on integers only returns an int64, or a *big.Int
// when the result does not fit; one with a float returns a float64:
//
//	add a b, sub a b, mul a b, div a b, mod a b
//	min a b..., max a b...
//	toInt x                 x as an integer; a float is truncated
//	toFloat x               x as a float64
//
// Strings:
//
//	upper s, lower s, trimSpace s
//	trim cutset s, trimLeft cutset s, trimRight cutset s
//	trimPrefix prefix s, trimSuffix suffix s
//	replace old new s       every old in s replaced by new
//	hasPrefix prefix s, hasSuffix suffix s, contains sub s
//	split sep s, join sep elems
//	repeat n s
//	quote s                 s as a Go interpreted string literal
//	indent prefix s         s with prefix before each line that is not empty
//	dedent s                s without the leading white space its lines that
//	                        are not empty share
//	replaceBetween start end new s
//	                        s with the lines between the first line holding
//	                        start and the next line holding end replaced by
//	                        new; both marker lines stay
//
// Choices:
//
//	default def v           def when v is empty, v otherwise
//	coalesce v...           the first v that is not empty
//	ternary a b cond        a when cond is true, b otherwise
//
// A value is empty when it is nil, false, zero, or a string, slice or map
// of length zero.
func BuiltinFuncs() template.FuncMap {
	return template.FuncMap{
		// Versions.
		"parseVer":   exver.Parse,
		"verCompare": verCompare,
		"withPre":    withPre,
		"withBuild":  withBuild,
		"withV":      func(v any) (exver.Version, error) { return mapVer(v, withVPrefix(true)) },
		"withoutV":   func(v any) (exver.Version, error) { return mapVer(v, withVPrefix(false)) },
		"withCore":   withCore,
		"verMajor":   func(v any) (int, error) { return verNum(v, 0) },
		"verMinor":   func(v any) (int, error) { return verNum(v, 1) },
		"verPatch":   func(v any) (int, error) { return verNum(v, 2) },
		"verExtra":   func(v any) (int, error) { return verNum(v, 3) },
		"verPre":     func(v any) (string, error) { return verStr(v, exver.Version.PreRelease) },
		"verBuild":   func(v any) (string, error) { return verStr(v, exver.Version.Build) },
		"setMajor":   setCore(exver.Core.WithMajor),
		"setMinor":   setCore(exver.Core.WithMinor),
		"setPatch":   setCore(exver.Core.WithPatch),
		"setExtra":   setCore(exver.Core.WithExtra),
		"dropMinor":  mapCore(exver.Core.DropMinor),
		"dropPatch":  mapCore(exver.Core.DropPatch),
		"dropExtra":  mapCore(exver.Core.DropExtra),
		"dropPre":    mapVersion(exver.Version.DropPreRelease),
		"dropBuild":  mapVersion(exver.Version.DropBuild),
		// Bump functions go here once exver provides bump helpers.

		// Date-times.
		"parseTime":   time.Parse,
		"formatTime":  formatTime,
		"rfc3339":     func(t any) (string, error) { return formatTime(time.RFC3339Nano, t) },
		"utc":         func(t any) (time.Time, error) { return mapTime(t, time.Time.UTC) },
		"addDuration": addDuration,
		"addDate":     addDate,
		"truncate":    truncate,

		// Numbers.
		"add":     func(a, b any) (any, error) { return arith(a, b, opAdd) },
		"sub":     func(a, b any) (any, error) { return arith(a, b, opSub) },
		"mul":     func(a, b any) (any, error) { return arith(a, b, opMul) },
		"div":     func(a, b any) (any, error) { return arith(a, b, opDiv) },
		"mod":     func(a, b any) (any, error) { return arith(a, b, opMod) },
		"min":     func(a any, rest ...any) (any, error) { return pick(a, rest, -1) },
		"max":     func(a any, rest ...any) (any, error) { return pick(a, rest, 1) },
		"toInt":   toInt,
		"toFloat": toFloat,

		// Strings.
		"upper":          strings.ToUpper,
		"lower":          strings.ToLower,
		"trimSpace":      strings.TrimSpace,
		"trim":           func(cutset, s string) string { return strings.Trim(s, cutset) },
		"trimLeft":       func(cutset, s string) string { return strings.TrimLeft(s, cutset) },
		"trimRight":      func(cutset, s string) string { return strings.TrimRight(s, cutset) },
		"trimPrefix":     func(prefix, s string) string { return strings.TrimPrefix(s, prefix) },
		"trimSuffix":     func(suffix, s string) string { return strings.TrimSuffix(s, suffix) },
		"replace":        func(old, new, s string) string { return strings.ReplaceAll(s, old, new) },
		"hasPrefix":      func(prefix, s string) bool { return strings.HasPrefix(s, prefix) },
		"hasSuffix":      func(suffix, s string) bool { return strings.HasSuffix(s, suffix) },
		"contains":       func(sub, s string) bool { return strings.Contains(s, sub) },
		"split":          func(sep, s string) []string { return strings.Split(s, sep) },
		"join":           func(sep string, elems []string) string { return strings.Join(elems, sep) },
		"repeat":         repeat,
		"quote":          strconv.Quote,
		"indent":         indent,
		"dedent":         dedent,
		"replaceBetween": replaceBetween,

		// Choices.
		"default":  func(def, v any) any { return coalesce(v, def) },
		"coalesce": coalesce,
		"ternary":  func(a, b any, cond bool) any { return map[bool]any{true: a, false: b}[cond] },
	}
}

func asVer(v any) (exver.Version, error) {
	switch v := v.(type) {
	case exver.Version:
		return v, nil
	case string:
		return exver.Parse(v)
	default:
		return exver.Version{}, fmt.Errorf("%T is not a version", v)
	}
}

func mapVer(v any, fn func(exver.Version) exver.Version) (exver.Version, error) {
	ver, err := asVer(v)
	if err != nil {
		return exver.Version{}, err
	}

	return fn(ver), nil
}

func withVPrefix(on bool) func(exver.Version) exver.Version {
	return func(v exver.Version) exver.Version { return v.WithV(on) }
}

func verCompare(a, b any) (int, error) {
	va, err := asVer(a)
	if err != nil {
		return 0, err
	}

	vb, err := asVer(b)
	if err != nil {
		return 0, err
	}

	return va.Compare(vb), nil
}

func withPre(pre string, v any) (exver.Version, error) {
	ver, err := asVer(v)
	if err != nil {
		return exver.Version{}, err
	}

	if pre == "" {
		return ver.DropPreRelease(), nil
	}

	return ver.WithPreRelease(pre)
}

func withBuild(build string, v any) (exver.Version, error) {
	ver, err := asVer(v)
	if err != nil {
		return exver.Version{}, err
	}

	if build == "" {
		return ver.DropBuild(), nil
	}

	return ver.WithBuild(build)
}

func mapVersion(fn func(exver.Version) exver.Version) func(v any) (exver.Version, error) {
	return func(v any) (exver.Version, error) { return mapVer(v, fn) }
}

func mapCore(fn func(exver.Core) exver.Core) func(v any) (exver.Version, error) {
	return func(v any) (exver.Version, error) {
		return mapVer(
			v,
			func(ver exver.Version) exver.Version { return ver.WithCore(fn(ver.Core())) },
		)
	}
}

func setCore(
	fn func(exver.Core, uint16) (exver.Core, error),
) func(n, v any) (exver.Version, error) {
	return func(n, v any) (exver.Version, error) { return setCoreOf(n, v, fn) }
}

func setCoreOf(n, v any, fn func(exver.Core, uint16) (exver.Core, error)) (exver.Version, error) {
	ver, err := asVer(v)
	if err != nil {
		return exver.Version{}, err
	}

	i, err := int64Of(n)
	if err != nil {
		return exver.Version{}, err
	}

	if i < 0 || i > math.MaxUint16 {
		return exver.Version{}, fmt.Errorf("version number %d is out of range", i)
	}

	core, err := fn(ver.Core(), uint16(i))
	if err != nil {
		return exver.Version{}, err
	}

	return ver.WithCore(core), nil
}

// withCore takes the numbers of the core, then the version a pipeline
// passes last.
func withCore(args ...any) (exver.Version, error) {
	if len(args) < 2 || len(args) > 5 {
		return exver.Version{}, errors.New("withCore takes 1 to 4 numbers and a version")
	}

	ver, err := asVer(args[len(args)-1])
	if err != nil {
		return exver.Version{}, err
	}

	nums := make([]uint16, 0, len(args)-1)

	for _, a := range args[:len(args)-1] {
		n, err := int64Of(a)
		if err != nil {
			return exver.Version{}, err
		}

		if n < 0 || n > math.MaxUint16 {
			return exver.Version{}, fmt.Errorf("version number %d is out of range", n)
		}

		nums = append(nums, uint16(n))
	}

	core, err := exver.NewCore(nums)
	if err != nil {
		return exver.Version{}, err
	}

	return ver.WithCore(core), nil
}

func verNum(v any, i int) (int, error) {
	ver, err := asVer(v)
	if err != nil {
		return 0, err
	}

	return int(ver.Core().Component()[i]), nil
}

func verStr(v any, fn func(exver.Version) string) (string, error) {
	ver, err := asVer(v)
	if err != nil {
		return "", err
	}

	return fn(ver), nil
}

func asTime(t any) (time.Time, error) {
	switch t := t.(type) {
	case time.Time:
		return t, nil
	case string:
		return time.Parse(time.RFC3339Nano, t)
	default:
		return time.Time{}, fmt.Errorf("%T is not a date-time", t)
	}
}

func asDuration(d any) (time.Duration, error) {
	switch d := d.(type) {
	case time.Duration:
		return d, nil
	case string:
		return time.ParseDuration(d)
	default:
		return 0, fmt.Errorf("%T is not a duration", d)
	}
}

func mapTime(t any, fn func(time.Time) time.Time) (time.Time, error) {
	tm, err := asTime(t)
	if err != nil {
		return time.Time{}, err
	}

	return fn(tm), nil
}

func formatTime(layout string, t any) (string, error) {
	tm, err := asTime(t)
	if err != nil {
		return "", err
	}

	return tm.Format(layout), nil
}

func addDuration(d, t any) (time.Time, error) {
	dur, err := asDuration(d)
	if err != nil {
		return time.Time{}, err
	}

	return mapTime(t, func(tm time.Time) time.Time { return tm.Add(dur) })
}

func addDate(years, months, days int, t any) (time.Time, error) {
	return mapTime(t, func(tm time.Time) time.Time { return tm.AddDate(years, months, days) })
}

func truncate(d, t any) (time.Time, error) {
	dur, err := asDuration(d)
	if err != nil {
		return time.Time{}, err
	}

	return mapTime(t, func(tm time.Time) time.Time { return tm.Truncate(dur) })
}

// number is an integer, held in i, or a float, held in f.
type number struct {
	i *big.Int
	f float64
}

func numberOf(x any) (number, error) {
	switch x := x.(type) {
	case int:
		return number{i: big.NewInt(int64(x))}, nil
	case int8:
		return number{i: big.NewInt(int64(x))}, nil
	case int16:
		return number{i: big.NewInt(int64(x))}, nil
	case int32:
		return number{i: big.NewInt(int64(x))}, nil
	case int64:
		return number{i: big.NewInt(x)}, nil
	case uint:
		return number{i: new(big.Int).SetUint64(uint64(x))}, nil
	case uint8:
		return number{i: big.NewInt(int64(x))}, nil
	case uint16:
		return number{i: big.NewInt(int64(x))}, nil
	case uint32:
		return number{i: big.NewInt(int64(x))}, nil
	case uint64:
		return number{i: new(big.Int).SetUint64(x)}, nil
	case *big.Int:
		if x == nil {
			return number{}, errors.New("nil *big.Int is not a number")
		}

		return number{i: new(big.Int).Set(x)}, nil
	case float32:
		return number{f: float64(x)}, nil
	case float64:
		return number{f: x}, nil
	case string:
		if i, ok := new(big.Int).SetString(strings.ReplaceAll(x, "_", ""), 0); ok {
			return number{i: i}, nil
		}

		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return number{}, fmt.Errorf("%q is not a number", x)
		}

		return number{f: f}, nil
	default:
		return number{}, fmt.Errorf("%T is not a number", x)
	}
}

func (n number) float() float64 {
	if n.i == nil {
		return n.f
	}

	f, _ := new(big.Float).SetInt(n.i).Float64()

	return f
}

// value returns n as an int64 when it fits, as a *big.Int otherwise, or as
// a float64.
func (n number) value() any {
	if n.i == nil {
		return n.f
	}

	if n.i.IsInt64() {
		return n.i.Int64()
	}

	return n.i
}

type op int

const (
	opAdd op = iota
	opSub
	opMul
	opDiv
	opMod
)

func arith(a, b any, o op) (any, error) {
	na, err := numberOf(a)
	if err != nil {
		return nil, err
	}

	nb, err := numberOf(b)
	if err != nil {
		return nil, err
	}

	if na.i != nil && nb.i != nil {
		if (o == opDiv || o == opMod) && nb.i.Sign() == 0 {
			return nil, errors.New("division by zero")
		}

		r := new(big.Int)

		switch o {
		case opAdd:
			r.Add(na.i, nb.i)
		case opSub:
			r.Sub(na.i, nb.i)
		case opMul:
			r.Mul(na.i, nb.i)
		case opDiv:
			r.Quo(na.i, nb.i)
		case opMod:
			r.Rem(na.i, nb.i)
		}

		return number{i: r}.value(), nil
	}

	fa, fb := na.float(), nb.float()

	switch o {
	case opAdd:
		return fa + fb, nil
	case opSub:
		return fa - fb, nil
	case opMul:
		return fa * fb, nil
	case opDiv:
		return fa / fb, nil
	default:
		return math.Mod(fa, fb), nil
	}
}

// pick returns the least of a and rest for sign -1, the greatest for 1.
func pick(a any, rest []any, sign int) (any, error) {
	best, err := numberOf(a)
	if err != nil {
		return nil, err
	}

	for _, x := range rest {
		n, err := numberOf(x)
		if err != nil {
			return nil, err
		}

		if compare(n, best) == sign {
			best = n
		}
	}

	return best.value(), nil
}

func compare(a, b number) int {
	if a.i != nil && b.i != nil {
		return a.i.Cmp(b.i)
	}

	fa, fb := a.float(), b.float()

	switch {
	case fa < fb:
		return -1
	case fa > fb:
		return 1
	default:
		return 0
	}
}

func toInt(x any) (any, error) {
	n, err := numberOf(x)
	if err != nil {
		return nil, err
	}

	if n.i != nil {
		return n.value(), nil
	}

	if math.IsNaN(n.f) || math.IsInf(n.f, 0) {
		return nil, fmt.Errorf("%v is not an integer", n.f)
	}

	i, _ := big.NewFloat(n.f).Int(nil)

	return number{i: i}.value(), nil
}

func toFloat(x any) (float64, error) {
	n, err := numberOf(x)
	if err != nil {
		return 0, err
	}

	return n.float(), nil
}

func int64Of(x any) (int64, error) {
	v, err := toInt(x)
	if err != nil {
		return 0, err
	}

	i, ok := v.(int64)
	if !ok {
		return 0, fmt.Errorf("%v does not fit int64", v)
	}

	return i, nil
}

func repeat(n int, s string) (string, error) {
	if n < 0 {
		return "", fmt.Errorf("negative repeat count %d", n)
	}

	return strings.Repeat(s, n), nil
}

func indent(prefix, s string) string {
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = prefix + l
		}
	}

	return strings.Join(lines, "")
}

func dedent(s string) string {
	lines := strings.SplitAfter(s, "\n")

	var common string

	first := true

	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}

		lead := l[:len(l)-len(strings.TrimLeft(l, " \t"))]

		switch {
		case first:
			common, first = lead, false
		default:
			for !strings.HasPrefix(lead, common) {
				common = common[:len(common)-1]
			}
		}
	}

	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, common)
	}

	return strings.Join(lines, "")
}

// replaceBetween keeps the marker lines, so that a later run finds them
// again.
func replaceBetween(start, end, repl, s string) (string, error) {
	lines := strings.SplitAfter(s, "\n")

	from := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, start) })
	if from < 0 {
		return "", fmt.Errorf("replaceBetween: no line holds %q", start)
	}

	to := slices.IndexFunc(lines[from+1:], func(l string) bool { return strings.Contains(l, end) })
	if to < 0 {
		return "", fmt.Errorf("replaceBetween: no line after %q holds %q", start, end)
	}

	to += from + 1

	if repl != "" && !strings.HasSuffix(repl, "\n") {
		repl += "\n"
	}

	return strings.Join(lines[:from+1], "") + repl + strings.Join(lines[to:], ""), nil
}

func coalesce(vs ...any) any {
	for _, v := range vs {
		if !empty(v) {
			return v
		}
	}

	return nil
}

func empty(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case bool:
		return !v
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case []string:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	case *big.Int:
		return v == nil || v.Sign() == 0
	}

	n, err := numberOf(v)
	if err != nil {
		return false
	}

	return compare(n, number{i: new(big.Int)}) == 0
}
