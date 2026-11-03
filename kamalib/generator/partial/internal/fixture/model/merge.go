package model

// Merging covers the merge directives and nil and empty collections.
//
//kama:partial
type Merging struct {
	//kama:partial:merge append
	Tags []string `json:"tags"`
	//kama:partial:merge append
	Attrs map[string]int `json:"attrs"`
	Plain []string       `json:"plain"`
	Dict  map[string]int `json:"dict"`
	//kama:partial:merge overwrite
	Replaced Address `json:"replaced"`
	Nested   Address `json:"nested"`
	//kama:partial:merge append
	Items Items `json:"items"`
	Index Index `json:"index"`
	//kama:partial:merge append
	Ptr *[]int `json:"ptr"`
	//kama:partial:flag name=flagged
	Flagged int `json:"flagged"`
}

// Embeds embeds a slice target, which becomes a field named after it.
//
//kama:partial
type Embeds struct {
	Labels
	*Base
}

// Labels is a slice target of a basic element type.
type Labels []string
