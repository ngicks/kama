package model

// Tree closes cycles through its own pointers and through Forest.
type Tree struct {
	Value       int    `json:"value"`
	Left, Right *Tree  `json:"-"`
	Kids        Forest `json:"kids"`
}

// Forest is a slice target on the cycle of Tree, which holds Tree without
// a pointer in its partial variant.
type Forest []*Tree

// Ring holds Elem by value and Elem points back, so only Elem.Ring closes
// the cycle.
type Ring struct {
	Head Elem `json:"head"`
}

// Elem is reachable from Ring.
type Elem struct {
	Name string `json:"name"`
	Ring *Ring  `json:"ring"`
}
