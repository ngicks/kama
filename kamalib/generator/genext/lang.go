package genext

import "github.com/ngicks/kama/kamalib/pkg/genhelper"

type (
	// Context is the context the code a type support tells renders with.
	Context = genhelper.BaseContext
	// Gen renders a piece of code, such as the expression of a held value.
	Gen = genhelper.Gen[Context]
	// Step renders code, such as the declaration Output.Declare takes.
	Step = genhelper.Step[Context]
)

// lang is the vocabulary of steps for Context. It is zero-size and holds no
// state.
var lang genhelper.Lang[Context]
