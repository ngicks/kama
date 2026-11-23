// Package fallback holds targets with unknown member fallbacks of
// encoding/json/v2.
package fallback

// Fallback captures the unknown members of its JSON object.
type Fallback struct {
	Name    string         `json:"name"`
	Unknown map[string]any `json:",embed"`
}

// Embedding embeds a target with a fallback.
type Embedding struct {
	Fallback
	Id int `json:"id"`
}

// PointerEmbedding embeds a target with a fallback by pointer.
type PointerEmbedding struct {
	*Fallback
	Id int `json:"id"`
}

// Masked has field masks. Its fallback has no path.
type Masked struct {
	Name    string         `json:"name"`
	Unknown map[string]any `json:",embed"`
}
