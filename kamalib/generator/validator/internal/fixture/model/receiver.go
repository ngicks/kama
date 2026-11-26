package model

import "sync"

// Locked holds a sync.Mutex, which go vet reports when a value holding it is
// copied. Its Validate and the walk of the values holding it validate it in
// place.
//
//kama:validate hook=False
type Locked struct {
	mu sync.Mutex
	//kama:validate length(ge=1)
	Name string `json:"name"`
}

// SetName sets the name under the lock.
func (l *Locked) SetName(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.Name = name
}

// Lockers holds Locked values in each place the nested walk reaches.
//
//kama:validate hook=False
type Lockers struct {
	One   Locked             `json:"one"`
	Ptr   *Locked            `json:"ptr"`
	Slice []Locked           `json:"slice"`
	Array [2]Locked          `json:"array"`
	Map   map[string]*Locked `json:"map"`
}
