package ext

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

// Params names the parameters of a validator, which Bind binds the args of
// a call to.
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
// parameters as the names of the members. dst may be nil when p takes no
// literal.
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

	return rules, nil
}
