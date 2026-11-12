package genhelper

// Gen returns one piece of text, usually an expression.
type Gen[C Context[C]] func(c C) (string, error)

// ForRange is a step appending a range loop:
//
//	for key, value := range x {
//		body
//	}
//
// x renders in the scope around the loop. key and value are roles declared
// in the scope of the loop, where body renders. An empty role is the blank
// identifier, and the loop declares nothing when both are empty.
func ForRange[C Context[C]](key, value string, x Gen[C], body Step[C]) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		over, err := x(c)
		if err != nil {
			return c, nil, err
		}
		outer := c.Scope()
		c = Push(c)

		p := c.Printer(b)
		switch {
		case key == "" && value == "":
			p.Printf("for range %s {\n", over)
		case value == "":
			p.Printf("for %s := range %s {\n", c.Scope().Declare(key), over)
		case key == "":
			p.Printf("for _, %s := range %s {\n", c.Scope().Declare(value), over)
		default:
			p.Printf(
				"for %s, %s := range %s {\n",
				c.Scope().Declare(key), c.Scope().Declare(value), over,
			)
		}

		c, err = printBody(c, p, body)
		if err != nil {
			return c, nil, err
		}
		p.Printf("}\n")
		return c.WithScope(outer), p.Bytes(), nil
	})
}

// If is a step appending an if statement:
//
//	if cond {
//		then
//	} else {
//		els
//	}
//
// cond renders in the scope around the statement. then and els render in
// scopes of their own. A nil els leaves the else branch out.
func If[C Context[C]](cond Gen[C], then, els Step[C]) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		expr, err := cond(c)
		if err != nil {
			return c, nil, err
		}
		outer := c.Scope()

		p := c.Printer(b)
		p.Printf("if %s {\n", expr)
		c, err = printBody(Push(c), p, then)
		if err != nil {
			return c, nil, err
		}
		if els != nil {
			p.Printf("} else {\n")
			c, err = printBody(Push(c.WithScope(outer)), p, els)
			if err != nil {
				return c, nil, err
			}
		}
		p.Printf("}\n")
		return c.WithScope(outer), p.Bytes(), nil
	})
}

// Case is one clause of [Switch].
type Case[C Context[C]] struct {
	// List renders the expressions of the clause, separated by commas. A
	// nil List is the default clause.
	List Gen[C]
	// Body renders in a scope of its own. It may be nil.
	Body Step[C]
}

// Switch is a step appending a switch statement:
//
//	switch tag {
//	case list:
//		body
//	default:
//		body
//	}
//
// tag and the lists render in the scope around the statement. A nil tag
// gives a switch without one.
func Switch[C Context[C]](tag Gen[C], cases ...Case[C]) Step[C] {
	return FuncStep[C](func(c C, b []byte) (C, []byte, error) {
		outer := c.Scope()

		p := c.Printer(b)
		if tag == nil {
			p.Printf("switch {\n")
		} else {
			expr, err := tag(c)
			if err != nil {
				return c, nil, err
			}
			p.Printf("switch %s {\n", expr)
		}
		for _, cs := range cases {
			if cs.List == nil {
				p.Printf("default:\n")
			} else {
				list, err := cs.List(c)
				if err != nil {
					return c, nil, err
				}
				p.Printf("case %s:\n", list)
			}
			var err error
			c, err = printBody(Push(c), p, cs.Body)
			if err != nil {
				return c, nil, err
			}
			c = c.WithScope(outer)
		}
		p.Printf("}\n")
		return c, p.Bytes(), nil
	})
}

// printBody renders body from empty text and prints it into p. c is
// positioned at the scope of the body.
func printBody[C Context[C]](c C, p Printer, body Step[C]) (C, error) {
	if body == nil {
		return c, nil
	}
	c, text, err := body.Render(c, nil)
	if err != nil {
		return c, err
	}
	p.Printf("%s", text)
	return c, nil
}
