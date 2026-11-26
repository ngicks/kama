package model

import (
	"database/sql"
	"math/big"
	"net/netip"
	"time"
	"unique"
)

// Std holds the types of the standard library that the package std of
// the generator supports.
//
//kama:validate hook=False
type Std struct {
	//kama:validate required
	Name sql.NullString `json:"name"`
	//kama:validate t=cmp(ge=0)
	Count sql.NullInt64 `json:"count"`
	// A rule a null type does not take applies to the value it holds.
	//kama:validate cmp(lt=100)
	Small sql.NullInt32 `json:"small"`
	//kama:validate null
	Gone sql.NullBool `json:"gone"`
	//kama:validate t=cmp(ge=-1)
	Short sql.NullInt16 `json:"short"`
	//kama:validate cmp(ne=0)
	Octet sql.NullByte `json:"octet"`
	//kama:validate t=cmp(lt=1.5)
	Real sql.NullFloat64 `json:"real"`
	// cmp applies to the time.Time the null type holds.
	//kama:validate cmp(ge="2020-01-01T00:00:00Z")
	Seen sql.NullTime `json:"seen"`
	//kama:validate cmp(lt="2030-01-01T00:00:00Z")
	Until *time.Time `json:"until"`
	//kama:validate zero
	Never time.Time `json:"never"`
	// The nested validation reaches the value.
	Tag sql.Null[Tag] `json:"tag"`
	//kama:validate t=length(le=3)
	Code unique.Handle[string] `json:"code"`
	Kind unique.Handle[Tag]    `json:"kind"`
	//kama:validate cmp(ge="2020-01-01T00:00:00Z", lt="2030-01-01T09:00:00+09:00")
	At time.Time `json:"at"`
	//kama:validate required
	Since time.Time `json:"since"`
	//kama:validate cmp(ge="1s", le=300000000000)
	Timeout time.Duration `json:"timeout"`
	//kama:validate multipleof(1000000)
	Step time.Duration `json:"step"`
	//kama:validate cmp(gt=0, lt=1e30)
	Int *big.Int `json:"int"`
	//kama:validate multipleof(3)
	Triple *big.Int `json:"triple"`
	//kama:validate cmp(ge=0.5, le=1)
	Ratio *big.Rat `json:"ratio"`
	//kama:validate cmp(lt=0.1)
	Float *big.Float `json:"float"`
	//kama:validate required
	Addr netip.Addr `json:"addr"`
	//kama:validate required
	Prefix netip.Prefix `json:"prefix"`
	//kama:validate zero
	Unset netip.Addr `json:"unset"`
}
