package shared

// Public exposes an unexported target to other packages.
type Public = hidden

// hidden is reachable from pkgmark.Order through Public, but its partial
// variant cannot be spelled there.
type hidden struct {
	Secret string `json:"secret"`
}
