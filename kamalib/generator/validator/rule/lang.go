package rule

import "github.com/ngicks/kama/kamalib/pkg/genhelper"

type (
	// Context is the context the code of a rule renders with.
	Context = genhelper.BaseContext
	// Gen renders a piece of code, such as the expression Rule.Emit
	// returns.
	Gen = genhelper.Gen[Context]
	// Step renders code, such as the declaration Emitter.Declare takes.
	Step = genhelper.Step[Context]
)

type (
	ref    = genhelper.Ref
	shadow = genhelper.Shadow
)

// lang is the vocabulary of steps for Context. It is zero-size and holds no
// state.
var lang genhelper.Lang[Context]
