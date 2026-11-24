// Package model declares targets in its files, in a _test.go file of the
// package and in its external test package.
package model

// Base is embedded by a target of every file.
//
//kama:partial
type Base struct {
	Id string `json:"id"`
}

// Item is a target outside the test files. Its field Owner calls helpers
// Sample calls too.
//
//kama:partial
type Item struct {
	Base

	Name  string `json:"name"`
	Owner Base   `json:"owner"`
}
