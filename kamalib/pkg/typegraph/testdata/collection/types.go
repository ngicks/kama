package collection

type Elem struct {
	Value string
}

type Key string

type (
	S []Elem
	M map[Key]Elem
)
