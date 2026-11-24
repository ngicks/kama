package model

import "github.com/ngicks/option/opt"

// Combinators holds a field per combinator, nested ones included.
//
//kama:validate hook=off
type Combinators struct {
	//kama:validate and(cmp=">=1", multipleof=2)
	Even int `json:"even"`
	//kama:validate or(const="0", cmp=">=10")
	Big int `json:"big"`
	//kama:validate not(regexp="^x")
	Word string `json:"word"`
	//kama:validate oneof(regexp="^a", regexp="b$")
	Either string `json:"either"`
	//kama:validate if(const=`"fixed"`, requires=amount, const=`"percent" "flat"`)
	Kind   string `json:"kind"`
	Amount *int   `json:"amount"`
	//kama:validate or(and(cmp=">=1", not(or(const="5", const="7"))), const="-1")
	Nested int `json:"nested"`
	//kama:validate or(cmp=">=10", const="0")
	Held opt.Option[int] `json:"held"`
	//kama:validate if(cmp=">=100", requires=note)
	Limit *int               `json:"limit,omitzero"`
	Note  opt.Option[string] `json:"note,omitzero"`
}

// Sparse holds either no member or two members or more.
//
//kama:validate hook=off or(members="==0", members=">=2")
type Sparse struct {
	A opt.Option[int] `json:"a,omitzero"`
	B *int            `json:"b,omitzero"`
	C opt.Option[int] `json:"c,omitzero"`
}

// Counted holds one member at most. Its member b always counts, as
// encoders write null for none.
//
//kama:validate hook=off members="<=1"
type Counted struct {
	A opt.Option[int] `json:"a,omitzero"`
	B opt.Option[int] `json:"b"`
}
