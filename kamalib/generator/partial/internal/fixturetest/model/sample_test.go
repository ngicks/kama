package model

// Sample is a target of a _test.go file of the package. Its field Owner
// calls helpers Item calls too, and its field Parent calls helpers only
// this file calls.
//
//kama:partial
type Sample struct {
	Item

	Count  int  `json:"count"`
	Owner  Base `json:"owner"`
	Parent Item `json:"parent"`
}
