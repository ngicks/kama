package ext

import (
	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

type (
	// Context is the context the code of a rule renders with.
	Context = genext.Context
	// Gen renders a piece of code, such as the expression Rule.Emit
	// returns.
	Gen = genext.Gen
	// Step renders code, such as the declaration Emitter.Declare takes.
	Step = genhelper.Step[Context]
)
