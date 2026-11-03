// Package pkg1 imports packages under their own names.
package pkg1

import (
	"encoding/json"
	"math/rand/v2"
)

var (
	_ = json.Marshal
	_ = rand.Int
)
