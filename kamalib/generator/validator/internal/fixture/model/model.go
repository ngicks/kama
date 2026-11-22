// Package model declares the types whose generated Validate methods the
// runtime tests call.
package model

import (
	"errors"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// ErrNegative is the error of the hook of User.Age.
var ErrNegative = errors.New("negative")

//kama:validate
//kama:validate:hook off
type User struct {
	//kama:validate:required
	Name string `json:"name"`
	//kama:validate:required
	Email   *string `json:"email"`
	Address Address `json:"address"`
	Tags    []Tag   `json:"tags"`
	Pets    map[string]*Pet
	//kama:validate:required
	Nick opt.Option[string] `json:"nick"`
	Box  Box[Tag]           `json:"box"`
	//kama:validate:hook on
	Age int `json:"age"`
}

func (u User) validateAge(bool) error {
	if u.Age < 0 {
		return ErrNegative
	}

	return nil
}

//kama:validate
//kama:validate:hook off
type Address struct {
	//kama:validate:required
	City string `json:"city"`
}

//kama:validate
//kama:validate:hook off
type Tag struct {
	//kama:validate:required
	Name string `json:"name"`
}

//kama:validate
//kama:validate:hook off
type Pet struct {
	//kama:validate:required
	Name string `json:"name"`
}

//kama:validate
//kama:validate:hook off
type Box[T any] struct {
	Items []T        `json:"items"`
	One   und.Und[T] `json:"one"`
}

// Team holds users, which makes it a dependant.
type Team struct {
	Members []User `json:"members"`
	Lead    *User  `json:"lead"`
}

// Roster is a slice of users.
type Roster []User
