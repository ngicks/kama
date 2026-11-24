package validator

import (
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

// genCtx is the context the generated functions render with.
type genCtx = genhelper.BaseContext

type (
	genStep  = genhelper.Step[genCtx]
	genSteps = genhelper.Steps[genCtx]
	genExpr  = genhelper.Gen[genCtx]
)

type decl = genhelper.Decl

// lang is the vocabulary of steps for genCtx. It is zero-size and holds no
// state.
var lang genhelper.Lang[genCtx]

// docComment returns the doc comment of a generated declaration as one
// comment group: the text of format and args, an empty comment line and the
// marker of the generator.
func docComment(format string, args ...any) genExpr {
	return lang.DocComment(GeneratorId.Marker(), format, args...)
}
