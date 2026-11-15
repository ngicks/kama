package model

// Deck holds collections of targets under the append and overwrite
// defaults.
//
//kama:partial
type Deck struct {
	// Unnamed slices and maps of targets map as a whole and append.
	Cards      []Card           `json:"cards"`
	CardRefs   []*Card          `json:"card_refs"`
	ByName     map[string]Card  `json:"by_name"`
	RefsByName map[string]*Card `json:"refs_by_name"`
	//kama:partial:merge overwrite
	Kept []*Card `json:"kept"`

	List Cards `json:"list"`
	//kama:partial:merge recursive
	RefList CardRefs `json:"ref_list"`
	//kama:partial:merge recursive
	Map CardMap `json:"map"`
	//kama:partial:merge append
	RefMap CardRefMap `json:"ref_map"`

	ListPtr *Cards `json:"list_ptr"`
	//kama:partial:merge recursive
	MapPtr *CardMap `json:"map_ptr"`

	//kama:partial:merge recursive
	Hands Hands `json:"hands"`
	//kama:partial:merge recursive
	Piles Piles[int] `json:"piles"`
}

// Card is reachable through the collections of Deck.
type Card struct {
	Name string `json:"name"`
	Rank *int   `json:"rank"`
}

// Cards is a slice target, whose Merge appends.
type Cards []Card

// CardRefs is a slice target of pointers, whose Merge appends.
type CardRefs []*Card

// CardMap is a map target, whose Merge sets the entries of r.
type CardMap map[string]Card

// CardRefMap is a map target of pointers, whose Merge sets the entries of
// r.
type CardRefMap map[string]*Card

// Hand holds collection targets and is the element of Hands.
type Hand struct {
	Cards Cards      `json:"cards"`
	Refs  CardRefMap `json:"refs"`
}

// Hands is a slice target whose element holds collection targets.
type Hands []Hand

// Piles is a generic slice target.
type Piles[T any] []Pile[T]

// Pile is reachable through the element of Piles.
type Pile[T any] struct {
	Top T `json:"top"`
}
