// Package model holds types generated with the append and overwrite
// defaults.
package model

import (
	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// Doc is marked by its doc comment.
//
//kama:partial
type Doc struct {
	// Base is an embedded target, which is replaced under overwrite.
	Base
	// Hollow is an embedded pointer to a target whose partial form has no
	// defined field.
	*Hollow

	Tags  []string          `json:"tags"`
	Attrs map[string]string `json:"attrs"`
	Name  string            `json:"name"`
	Inner Inner             `json:"inner"`
	//kama:partial:merge recursive
	Merged Inner `json:"merged"`
	//kama:partial:merge overwrite
	Kept  []string `json:"kept"`
	Lines Lines    `json:"lines"`
	//kama:partial:merge append
	Index    Index                         `json:"index"`
	OptTags  opt.Option[[]string]          `json:"opt_tags"`
	OptAttrs opt.Option[map[string]string] `json:"opt_attrs"`

	// Blank fields cannot be referred to, so the methods skip them.
	_ int
	_ string
}

// Base is an embedded target.
type Base struct {
	ID  string `json:"id"`
	Rev int    `json:"rev"`
}

// Hollow holds an option type only, which FromValue copies as is.
type Hollow struct {
	Note und.Und[string] `json:"note"`
}

// Inner is reachable from Doc.
type Inner struct {
	A int `json:"a"`
	B int `json:"b"`
}

// Lines is a slice target, whose Merge appends.
type Lines []Inner

// Index is a map target, whose Merge sets the entries of r.
type Index map[string]Inner
