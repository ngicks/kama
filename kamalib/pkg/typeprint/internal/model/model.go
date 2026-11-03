// Package model declares a variable for each type expression the typeprint
// tests print. A type whose name starts with U is printed by its
// underlying type, for type expressions no variable can have.
package model

import (
	"io"
	"sync/atomic"
	"time"
	"unsafe"

	o "github.com/ngicks/kama/kamalib/pkg/typeprint/internal/other"
)

type Local struct{}

type G[K comparable, V any] struct{}

type A = time.Duration

type AG[T comparable] = G[T, int]

var (
	VBool   bool
	VByte   byte
	VRune   rune
	VUnsafe unsafe.Pointer
	VError  error
	VAny    any
	// The spelling interface{} gives an empty *types.Interface, while any
	// gives the predeclared alias.
	VEmptyIface       interface{} //nolint:modernize
	VLocal            Local
	VQualified        time.Duration
	VRenamed          o.Named
	VGenericLocal     G[string, *Local]
	VGenericQualified atomic.Pointer[time.Time]
	VGenericRenamed   o.Pair[string, []o.Named]
	VAlias            A
	VAliasGeneric     AG[string]
	VPointer          *int
	VSlice            []string
	VArray            [4]byte
	VMap              map[string][]int
	VChan             chan int
	VSendChan         chan<- int
	VRecvChan         <-chan int
	VChanOfRecvChan   chan (<-chan int)
	VChanOfSendChan   chan chan<- int
	VSendChanOfChan   chan<- chan int
	VRecvChanOfRecv   <-chan <-chan int
	VFunc             func()
	VFuncUnnamed      func(int, string) error
	VFuncNamed        func(a int, b string) (n int, err error)
	VFuncVariadic     func(format string, args ...any)
	VFuncResults      func() (int, error)
	VFuncOfFunc       func(func(int) bool) func() error
	VStructEmpty      struct{}
	VStruct           struct {
		Name   string `json:"name"`
		Quoted int    "json:\"`quoted`\""
		o.Named
		*Local
		Duration A
		Inline   struct{ X int }
	}
	VIface interface {
		io.Reader
		Close() error
		Name() string
	}
)

type UConstraint interface {
	~int | ~string | o.Named
	comparable
}

type UTilde interface{ ~[]byte }

type UGeneric[K comparable, V any] struct {
	Map  map[K][]V
	Ptr  *G[K, V]
	Func func(K) V
}
