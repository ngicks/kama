package model

// Config is listed in kama_partial.yaml.
type Config struct {
	Limits Limits `toml:"limits"`
	Level  Level  `toml:"level"`
	Meta   Meta   `yaml:"meta"`
}

// Limits is listed in kama_partial.yaml too.
type Limits struct {
	Max int `yaml:"max,omitempty"`
}

// Meta is ignored by kama_partial.yaml, so Config keeps it whole.
type Meta struct {
	Note string
}

// kama_partial.yaml lists none of the following types.

type Level int

type Handler func()

type Visitor interface {
	Visit()
}

type Names = []string
