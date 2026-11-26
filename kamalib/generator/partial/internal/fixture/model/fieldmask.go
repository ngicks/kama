package model

// Masked has field masks, with leaves, branches, embedded partial values
// and collections.
//
//kama:partial
//kama:partial:fieldmask
type Masked struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	On    bool   `json:"on"`
	Ptr   *string
	//kama:partial:fieldmask name="alias"
	Renamed string

	Inner    MaskInner
	InnerPtr *MaskInner
	Plain    MaskPlain

	MaskEmbedded
	*MaskAudit

	Items []MaskInner
	ByKey map[string]MaskInner

	Box MaskBox[int]
}

// MaskInner is a branch of Masked holding a branch of its own.
//
//kama:partial:fieldmask
type MaskInner struct {
	A    string
	B    *int
	Deep MaskDeep
}

// MaskDeep is a branch of MaskInner.
//
//kama:partial:fieldmask
type MaskDeep struct {
	X int
	Y []string
}

// MaskPlain is a target without field masks, a leaf of Masked.
type MaskPlain struct {
	P string
}

// MaskEmbedded is embedded by Masked, which takes the paths of its fields.
//
//kama:partial:fieldmask
type MaskEmbedded struct {
	E string
	//kama:partial:fieldmask name="e_deep"
	EDeep MaskDeep
}

// MaskAudit is embedded by pointer.
//
//kama:partial:fieldmask
type MaskAudit struct {
	Note string
}

// MaskBox is a generic target with field masks.
//
//kama:partial:fieldmask
type MaskBox[T any] struct {
	V   T
	Ptr *T
}

// MaskPair is a generic target with field masks holding a generic branch.
//
//kama:partial
//kama:partial:fieldmask
type MaskPair[K comparable, V any] struct {
	Key K
	Box MaskBox[V]
	Map map[K]V
}

// MaskTree has field masks and closes a cycle through a pointer.
//
//kama:partial
//kama:partial:fieldmask
type MaskTree struct {
	Name  string
	Child *MaskTree
}
