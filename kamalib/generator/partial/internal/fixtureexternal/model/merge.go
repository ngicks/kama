package model

// Merging covers the merge directives and nil and empty collections.
type Merging struct {
	Tags     []string       `json:"tags"`
	Attrs    map[string]int `json:"attrs"`
	Plain    []string       `json:"plain"`
	Dict     map[string]int `json:"dict"`
	Replaced Address        `json:"replaced"`
	Nested   Address        `json:"nested"`
	Items    Items          `json:"items"`
	Index    Index          `json:"index"`
	Ptr      *[]int         `json:"ptr"`
	Flagged  int            `json:"flagged"`
}

// Embeds embeds a slice target, which becomes a field named after it.
type Embeds struct {
	Labels
	*Base
}

// Labels is a slice target of a basic element type.
type Labels []string
