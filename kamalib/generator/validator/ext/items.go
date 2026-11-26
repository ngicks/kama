package ext

import "github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"

// The words of //kama:validate that name no validator and no role. A
// validator, a role and an item of a directive cannot share them.
const (
	HookWord    = "hook"
	PreWord     = "pre"
	PostWord    = "post"
	IgnoreWord  = "ignore"
	DecodedWord = "decoded"
	FileWord    = "file"
	PackageWord = "package"
	// GroupWord names the call grouping the items applying to the values
	// of a role, as in e=g(length(ge=1), v=cmp(ge=0)).
	GroupWord  = "g"
	NestedWord = "nested"
	UnwrapWord = "unwrap"
	IterWord   = "iter"
	Iter2Word  = "iter2"
)

// ItemWords lists the words of //kama:validate that name no validator and
// no role.
var ItemWords = []string{
	HookWord, PreWord, PostWord, IgnoreWord, DecodedWord, FileWord, PackageWord,
	GroupWord, NestedWord, UnwrapWord, IterWord, Iter2Word,
}

// Hook returns the item hook=True, or hook=False when on is false.
func Hook(on bool) expr.Arg { return expr.Arg{Name: HookWord, Value: expr.Bool{Value: on}} }

// Decoded returns the item decoded, which marks a type whose decoders
// validate it.
func Decoded() expr.Arg { return expr.Arg{Value: expr.Call{Name: DecodedWord}} }
