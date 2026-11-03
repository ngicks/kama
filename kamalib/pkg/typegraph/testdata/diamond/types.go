package diamond

type (
	A struct {
		B B
		C C
	}

	B struct {
		D D
	}

	C struct {
		D D
	}

	D struct {
		Value int
	}
)
