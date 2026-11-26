package ext

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strings"

	"github.com/ngicks/opiter"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Params names the parameters of a validator. Bind binds the args of a
// call to them and decodes the args into a struct of the options, and
// Call spells such a struct back as a call. A validator declares its
// Params and its options struct once and gets both directions.
type Params struct {
	// Positional names the parameters the positional args bind to, in
	// order.
	Positional []string
	// Variadic names the parameter the positional args after Positional
	// bind to. Empty stands for none.
	Variadic string
	// Keywords names the parameters only a keyword arg binds to.
	Keywords []string
	// Rules names the parameters whose args are rules. Every other
	// parameter takes literals.
	Rules []string
}

// Bind binds the args of c to the parameters of p, as expr.Object does,
// and returns the args bound to a parameter of Rules, in the order
// written. It decodes the literal args into dst, a pointer to the struct
// of the options, with encoding/json/v2, which takes the names of the
// parameters as the names of the members. A field of dst named after a
// parameter of Rules receives the rules bound to it: a field of type
// expr.Node the first, a field of type []expr.Node all of them. dst may be
// nil when p takes no literal.
//
// A keyword arg naming no parameter of p is left out and reported through
// r.Warn. A rule bound to a parameter outside Rules, a literal bound to one
// of Rules and an arg that does not fit are errors wrapping ErrArgs.
func (p Params) Bind(r Resolver, c expr.Call, dst any) ([]expr.Arg, error) {
	known := slices.Concat(p.Positional, p.Keywords)
	if p.Variadic != "" {
		known = append(known, p.Variadic)
	}

	var args []expr.Arg

	for _, a := range c.Args {
		if a.Name != "" && !slices.Contains(known, a.Name) {
			r.Warn(fmt.Errorf("%s: unknown arg %s; known: %v", c.Name, a, known))
			continue
		}

		args = append(args, a)
	}

	c.Args = args

	obj, rules, err := expr.Object(c, p.Positional, p.Variadic)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrArgs, err)
	}

	for _, a := range rules {
		if slices.Contains(p.Rules, a.Name) {
			continue
		}

		if bare, ok := a.Value.(expr.Call); ok && !bare.Parens && len(bare.Args) == 0 {
			return nil, fmt.Errorf(
				"%w: %s: %s of %s is no literal; quote a name, as in %s(%q)",
				ErrArgs, c.Name, a.Value, a.Name, c.Name, bare.Name,
			)
		}

		return nil, fmt.Errorf("%w: %s: %s of %s is no literal", ErrArgs, c.Name, a.Value, a.Name)
	}

	var members map[string]jsontext.Value
	if err := json.Unmarshal(obj, &members); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrArgs, c.Name, err)
	}

	for name := range members {
		if slices.Contains(p.Rules, name) {
			return nil, fmt.Errorf("%w: %s: %s takes rules, got a literal", ErrArgs, c.Name, name)
		}
	}

	if dst == nil {
		if len(members) > 0 {
			return nil, fmt.Errorf("%w: %s takes no literal, got %s", ErrArgs, c.Name, obj)
		}

		return rules, nil
	}

	if err := json.Unmarshal(obj, dst); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrArgs, c.Name, err)
	}

	v := reflect.ValueOf(dst).Elem()
	if v.Kind() != reflect.Struct {
		return rules, nil
	}

	for sf, fv := range optionFields(v) {
		name := paramName(sf)
		if !slices.Contains(p.Rules, name) {
			continue
		}

		nodes := opiter.WrapSeq(slices.Values(rules)).
			Filter(func(a expr.Arg) bool { return a.Name == name }).
			Map(func(a expr.Arg) expr.Node { return a.Value }).
			CollectSlice()

		switch {
		case fv.Type() == nodesType:
			fv.Set(reflect.ValueOf(nodes))
		case fv.Type() == nodeType && len(nodes) > 0:
			fv.Set(reflect.ValueOf(&nodes[0]).Elem())
		}
	}

	return rules, nil
}

// Call returns the call named name whose args spell src, the struct of
// the options that Bind decodes into, or a pointer to it: the reverse of
// Bind. A field takes the parameter named by its json tag, else by its
// name. A field of a parameter of Rules holds an expr.Node or a
// []expr.Node, and any other field a value encoding/json/v2 encodes as a
// literal. A field holding nil gives no arg, and so does a field of a
// parameter other than a positional one holding its zero value. A
// positional parameter is spelled as its value, as "" in field("").
//
// The args come in the order of the parameters: the positional ones by
// position, the variadic ones after them, then the keyword ones in the
// order of Keywords. A positional parameter after one holding nothing
// becomes a keyword arg. A call without args is the bare name, as
// required.
//
// A field naming no parameter, a variadic arg after a positional parameter
// holding nothing, a rule field of another type and a value no literal
// spells, as 1e400, are errors.
func (p Params) Call(name string, src any) (expr.Call, error) {
	v := reflect.Indirect(reflect.ValueOf(src))
	if v.Kind() != reflect.Struct {
		return expr.Call{}, fmt.Errorf("%s: options of type %T are no struct", name, src)
	}

	known := slices.Concat(p.Positional, []string{p.Variadic}, p.Keywords)
	spelled := map[string][]expr.Node{}

	for sf, fv := range optionFields(v) {
		param := paramName(sf)
		if param == "" || !slices.Contains(known, param) {
			return expr.Call{}, fmt.Errorf("%s: field %s names no parameter; known: %v",
				name, sf.Name, known)
		}

		nodes, err := p.spell(param, fv)
		if err != nil {
			return expr.Call{}, fmt.Errorf("%s: %s: %w", name, param, err)
		}

		if len(nodes) > 0 {
			spelled[param] = nodes
		}
	}

	var (
		args     []expr.Arg
		gap      string
		keywords []string
	)

	for _, n := range p.Positional {
		switch {
		case spelled[n] == nil:
			gap = cmp.Or(gap, n)
		case gap != "":
			keywords = append(keywords, n)
		default:
			args = append(args, expr.Arg{Value: spelled[n][0]})
		}
	}

	if rest := spelled[p.Variadic]; p.Variadic != "" && rest != nil {
		if gap != "" {
			return expr.Call{}, fmt.Errorf("%s: %s follows %s, which holds nothing",
				name, p.Variadic, gap)
		}

		args = opiter.WrapSeq(slices.Values(rest)).
			Map(func(n expr.Node) expr.Arg { return expr.Arg{Value: n} }).
			AppendSeq(args)
	}

	args = opiter.WrapSeq(slices.Values(slices.Concat(keywords, p.Keywords))).
		Filter(func(n string) bool { return spelled[n] != nil }).
		Map(func(n string) expr.Arg { return expr.Arg{Name: n, Value: spelled[n][0]} }).
		AppendSeq(args)

	return expr.Call{Name: name, Parens: len(args) > 0, Args: args}, nil
}

// spell returns the nodes of the value v of the parameter name, one per
// arg, or nil when v gives no arg.
func (p Params) spell(name string, v reflect.Value) ([]expr.Node, error) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		if v.IsNil() {
			return nil, nil
		}
	default:
		if v.IsZero() && !slices.Contains(p.Positional, name) {
			return nil, nil
		}
	}

	if slices.Contains(p.Rules, name) {
		switch v.Type() {
		case nodeType:
			return []expr.Node{v.Interface().(expr.Node)}, nil
		case nodesType:
			return v.Interface().([]expr.Node), nil
		}

		return nil, fmt.Errorf(
			"a parameter of rules takes %s or %s, got %s",
			nodeType,
			nodesType,
			v.Type(),
		)
	}

	text, err := json.Marshal(v.Interface())
	if err != nil {
		return nil, err
	}

	if name != p.Variadic {
		n, err := expr.FromJson(text)
		if err != nil {
			return nil, err
		}

		return []expr.Node{n}, nil
	}

	var elems []jsontext.Value
	if err := json.Unmarshal(text, &elems); err != nil {
		return nil, err
	}

	nodes := make([]expr.Node, 0, len(elems))

	for _, e := range elems {
		n, err := expr.FromJson(e)
		if err != nil {
			return nil, err
		}

		nodes = append(nodes, n)
	}

	return nodes, nil
}

var (
	nodeType  = reflect.TypeFor[expr.Node]()
	nodesType = reflect.TypeFor[[]expr.Node]()
)

// optionFields returns the exported fields of the struct v that a json tag
// does not leave out, with their values.
func optionFields(v reflect.Value) iter.Seq2[reflect.StructField, reflect.Value] {
	return func(yield func(reflect.StructField, reflect.Value) bool) {
		for i := range v.NumField() {
			sf := v.Type().Field(i)
			if sf.IsExported() && sf.Tag.Get("json") != "-" && !yield(sf, v.Field(i)) {
				return
			}
		}
	}
}

// paramName returns the name of the parameter the field sf takes: the
// name of its json tag, else the name of the field.
func paramName(sf reflect.StructField) string {
	name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
	return cmp.Or(name, sf.Name)
}
