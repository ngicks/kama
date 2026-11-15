package model

// MaskPing has field masks and closes a cycle with MaskPong.
//
//kama:partial
//kama:partial:fieldmask
type MaskPing struct {
	Name string
	Pong *MaskPong
}

// MaskPong has field masks and closes a cycle with MaskPing.
//
//kama:partial:fieldmask
type MaskPong struct {
	Count int
	Ping  *MaskPing
}

// MaskHolder embeds by pointer a partial value holding a branch.
//
//kama:partial
//kama:partial:fieldmask
type MaskHolder struct {
	*MaskEmbedded
}
