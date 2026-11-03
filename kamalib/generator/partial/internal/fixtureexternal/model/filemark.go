package model

// Config is listed in partial.yaml.
type Config struct {
	Limits Limits `toml:"limits"`
	Level  Level  `toml:"level"`
	Meta   Meta   `yaml:"meta"`
}

// Limits is listed in partial.yaml too.
type Limits struct {
	Max int `yaml:"max,omitempty"`
}

// Meta is ignored by partial.yaml, so Config keeps it whole.
type Meta struct {
	Note string
}

// partial.yaml lists none of the following types.

type Level int

type Handler func()

type Visitor interface {
	Visit()
}

type Names = []string
