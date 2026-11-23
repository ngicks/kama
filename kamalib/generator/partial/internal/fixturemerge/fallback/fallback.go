// Package fallback holds a target with an unknown member fallback of
// encoding/json/v2, which keeps overwrite under the append strategy.
package fallback

// Fallback captures the unknown members of its JSON object.
//
//kama:partial
type Fallback struct {
	Tags    []string       `json:"tags"`
	Unknown map[string]any `json:",embed"`
}
