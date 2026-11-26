package basic

import (
	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/genhelper"
)

type (
	ref    = genhelper.Ref
	shadow = genhelper.Shadow
)

// lang is the vocabulary of steps for ext.Context. It is zero-size and
// holds no state.
var lang genhelper.Lang[ext.Context]
