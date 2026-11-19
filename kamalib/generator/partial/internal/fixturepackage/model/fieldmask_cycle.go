package model

// MaskPing has field masks and closes a cycle with MaskPong.
type MaskPing struct {
	Name string
	Pong *MaskPong
}

// MaskPong has field masks and closes a cycle with MaskPing.
type MaskPong struct {
	Count int
	Ping  *MaskPing
}

// MaskHolder embeds by pointer a partial value holding a branch.
type MaskHolder struct {
	*MaskEmbedded
}
