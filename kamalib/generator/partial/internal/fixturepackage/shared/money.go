// Package shared holds types reachable from targets of other packages.
package shared

// Money is unmarked but reachable from pkgmark.Order.
type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Rate is neither marked nor reachable.
type Rate struct {
	Value float64
}
