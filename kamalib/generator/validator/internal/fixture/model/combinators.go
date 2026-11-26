package model

import "github.com/ngicks/option/opt"

// Combinators holds a field per combinator, nested ones included.
//
//kama:validate hook=False
type Combinators struct {
	//kama:validate cmp(ge=1) and multipleof(2)
	Even int `json:"even"`
	//kama:validate const(0) or cmp(ge=10)
	Big int `json:"big"`
	//kama:validate not regexp("^x")
	Word string `json:"word"`
	//kama:validate oneof(regexp("^a"), regexp("b$"))
	Either string `json:"either"`
	//kama:validate requires("amount") if const("fixed") else const("percent", "flat")
	Kind   string `json:"kind"`
	Amount *int   `json:"amount"`
	//kama:validate cmp(ge=1) and not (const(5) or const(7)) or const(-1)
	Nested int `json:"nested"`
	//kama:validate cmp(ge=10) or const(0)
	Held opt.Option[int] `json:"held"`
	//kama:validate requires("note") if cmp(ge=100) else always
	Limit *int               `json:"limit,omitzero"`
	Note  opt.Option[string] `json:"note,omitzero"`
}

// Sparse holds either no member or two members or more.
//
//kama:validate hook=False, members(0) or members(ge=2)
type Sparse struct {
	A opt.Option[int] `json:"a,omitzero"`
	B *int            `json:"b,omitzero"`
	C opt.Option[int] `json:"c,omitzero"`
}

// Counted holds one member at most. Its member b always counts, as
// encoders write null for none.
//
//kama:validate hook=False, members(le=1)
type Counted struct {
	A opt.Option[int] `json:"a,omitzero"`
	B opt.Option[int] `json:"b"`
}
