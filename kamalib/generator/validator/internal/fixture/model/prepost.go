package model

import (
	"errors"
	"fmt"
)

// Hooked records the calls of its pre and post functions into Log. Fail
// makes them fail.
//
//kama:validate pre="validateHookedPre", post="validateHookedPost", hook=False
type Hooked struct {
	//kama:validate cmp(gt=0)
	N    int       `json:"n"`
	Next *Hooked   `json:"next"`
	Fail Fail      `json:"-"`
	Log  *[]string `json:"-"`
}

// Fail tells which of pre and post fail.
type Fail struct {
	Pre, Post bool
}

func validateHookedPre(v Hooked, stopOnFirstError bool) error {
	return logCall(v.Log, v.Fail.Pre, fmt.Sprintf("pre %d %t", v.N, stopOnFirstError))
}

func validateHookedPost(v Hooked, stopOnFirstError bool) error {
	return logCall(v.Log, v.Fail.Post, fmt.Sprintf("post %d %t", v.N, stopOnFirstError))
}

// Tally counts the calls of its pre and post functions under the keys pre
// and post when it holds them. The keys failPre and failPost make them
// fail.
//
//kama:validate length(le=4), pre="validateTallyPre", post="validateTallyPost"
type Tally map[string]int

func validateTallyPre(v Tally, _ bool) error { return count(v, "pre", "failPre") }

func validateTallyPost(v Tally, _ bool) error { return count(v, "post", "failPost") }

func logCall(log *[]string, fail bool, call string) error {
	*log = append(*log, call)
	if fail {
		return errors.New(call)
	}

	return nil
}

func count(v Tally, key, failKey string) error {
	if _, ok := v[key]; ok {
		v[key]++
	}

	if _, ok := v[failKey]; ok {
		return errors.New(key)
	}

	return nil
}
