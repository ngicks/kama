package validator

import (
	"fmt"
	"reflect"
	"testing"
)

// The module depends on the standard library only, tests included, so these replace an
// assertion library. Each one stops the test on failure.

func assertTrue(t *testing.T, cond bool, msgAndArgs ...any) {
	t.Helper()

	if !cond {
		t.Fatalf("assertion failed%s", message(msgAndArgs))
	}
}

func equal[T comparable](t *testing.T, got, want T, msgAndArgs ...any) {
	t.Helper()

	if got != want {
		t.Fatalf("got %#v, want %#v%s", got, want, message(msgAndArgs))
	}
}

func deepEqual[T any](t *testing.T, got, want T) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func nilError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func message(msgAndArgs []any) string {
	if len(msgAndArgs) == 0 {
		return ""
	}

	if format, ok := msgAndArgs[0].(string); ok {
		return ": " + fmt.Sprintf(format, msgAndArgs[1:]...)
	}

	return ": " + fmt.Sprint(msgAndArgs...)
}
