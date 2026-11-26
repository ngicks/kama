package model

import (
	"github.com/ngicks/option/either"
	"github.com/ngicks/option/elastic"
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/tuple"
	"github.com/ngicks/option/und"
)

// Roles validates the values its fields hold through role items.
//
//kama:validate hook=False
type Roles struct {
	//kama:validate length(ge=1), e=length(le=3)
	Tags []string `json:"tags"`
	//kama:validate k=regexp(r"^[a-z]+$"), v=cmp(ge=0)
	Counts map[string]int `json:"counts"`
	//kama:validate t=length(le=3)
	Note opt.Option[string] `json:"note"`
	//kama:validate t=cmp(gt=0)
	Limit und.Und[int] `json:"limit"`
	//kama:validate length(3) or null, t=length(le=3)
	Codes elastic.Elastic[string] `json:"codes"`
	//kama:validate l=length(ge=1), r=cmp(ge=0)
	Choice either.Either[string, int] `json:"choice"`
	//kama:validate ok=cmp(gt=0), err=nil
	Outcome either.Result[int] `json:"outcome"`
	//kama:validate v1=length(ge=1), v2=cmp(lt=10)
	Pair tuple.Tuple2[string, int] `json:"pair"`
	//kama:validate e=g(length(ge=1), v=cmp(ge=0))
	Groups []map[string]int `json:"groups"`
	//kama:validate e=g(nil or not cmp(0))
	Deep **int `json:"deep"`
	//kama:validate cmp(ge=1)
	Wrapped *opt.Option[int] `json:"wrapped"`
}

// Bag holds values of its type parameter, which role items validate with a
// rule every type argument takes.
//
//kama:validate hook=False
type Bag[T comparable] struct {
	//kama:validate e=not zero
	Items []T `json:"items"`
	//kama:validate t=not zero
	Main opt.Option[T] `json:"main"`
}
