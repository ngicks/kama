package model

//kama:partial file

// Config is marked by the file mark.
type Config struct {
	Limits Limits `toml:"limits"`
	Level  Level  `toml:"level"`
	Meta   Meta   `yaml:"meta"`
}

// Limits is marked by the file mark too.
type Limits struct {
	Max int `yaml:"max,omitempty"`
}

// Meta opts out of the file mark, so Config keeps it whole.
//
//kama:partial ignore
type Meta struct {
	Note string
}

// The file mark skips the following types silently.

type Level int

type Handler func()

type Visitor interface {
	Visit()
}

type Names = []string
