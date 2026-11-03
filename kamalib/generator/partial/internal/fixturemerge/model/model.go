// Package model holds types generated with the append and overwrite
// defaults.
package model

// Doc is marked by its doc comment.
//
//kama:partial
type Doc struct {
	Tags  []string          `json:"tags"`
	Attrs map[string]string `json:"attrs"`
	Name  string            `json:"name"`
	Inner Inner             `json:"inner"`
	//kama:partial:merge recursive
	Merged Inner `json:"merged"`
	//kama:partial:merge overwrite
	Kept  []string `json:"kept"`
	Lines Lines    `json:"lines"`
}

// Inner is reachable from Doc.
type Inner struct {
	A int `json:"a"`
	B int `json:"b"`
}

// Lines is a slice target, whose Merge appends.
type Lines []Inner
