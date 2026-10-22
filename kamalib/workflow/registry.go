package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrInvalidFunc   = errors.New("workflow: invalid step function")
	ErrDuplicateKind = errors.New("workflow: duplicate kind")
)

var (
	contextType = reflect.TypeFor[context.Context]()
	errorType   = reflect.TypeFor[error]()
)

// Registry maps kinds to step functions.
//
// A loader has the shape
//
//	func(ctx context.Context, in1 I1, ..., inN IN, out *O, param P) error
//
// and a sink the shape
//
//	func(ctx context.Context, in1 I1, ..., inN IN, param P) error
//
// The number of inputs is fixed by the function and must match the step's
// input list. A function without parameters takes struct{} as P.
type Registry struct {
	loaders map[string]reflect.Value
	sinks   map[string]reflect.Value
}

func NewRegistry() *Registry {
	return &Registry{loaders: map[string]reflect.Value{}, sinks: map[string]reflect.Value{}}
}

// Loader registers fn as the loader for kind.
func (r *Registry) Loader(kind string, fn any) error {
	// ctx, out, param
	v, err := checkFunc(fn, 3)
	if err != nil {
		return fmt.Errorf("loader %q: %w", kind, err)
	}

	t := v.Type()
	if t.In(t.NumIn()-2).Kind() != reflect.Pointer {
		return fmt.Errorf(
			"loader %q: %w: output must be a pointer, got %v",
			kind,
			ErrInvalidFunc,
			t.In(t.NumIn()-2),
		)
	}

	if _, dup := r.loaders[kind]; dup {
		return fmt.Errorf("%w: loader %q", ErrDuplicateKind, kind)
	}

	r.loaders[kind] = v
	return nil
}

// Sink registers fn as the sink for kind.
func (r *Registry) Sink(kind string, fn any) error {
	// ctx, param
	v, err := checkFunc(fn, 2)
	if err != nil {
		return fmt.Errorf("sink %q: %w", kind, err)
	}

	if _, dup := r.sinks[kind]; dup {
		return fmt.Errorf("%w: sink %q", ErrDuplicateKind, kind)
	}

	r.sinks[kind] = v
	return nil
}

func checkFunc(fn any, minIn int) (reflect.Value, error) {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return reflect.Value{}, fmt.Errorf("%w: %T is not a function", ErrInvalidFunc, fn)
	}

	t := v.Type()
	if t.IsVariadic() {
		return reflect.Value{}, fmt.Errorf("%w: variadic %v", ErrInvalidFunc, t)
	}
	if t.NumIn() < minIn {
		return reflect.Value{}, fmt.Errorf(
			"%w: %v takes fewer than %d arguments",
			ErrInvalidFunc,
			t,
			minIn,
		)
	}
	if t.In(0) != contextType {
		return reflect.Value{}, fmt.Errorf(
			"%w: first argument of %v is not context.Context",
			ErrInvalidFunc,
			t,
		)
	}
	if t.NumOut() != 1 || t.Out(0) != errorType {
		return reflect.Value{}, fmt.Errorf("%w: %v must return only error", ErrInvalidFunc, t)
	}

	return v, nil
}
