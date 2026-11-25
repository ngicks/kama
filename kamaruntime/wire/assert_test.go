package wire

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// The module depends on the standard library only, tests included, so these replace an
// assertion library. Each one stops the test on failure.

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

func nilError(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v%s", err, message(msgAndArgs))
	}
}

func errorIs(t *testing.T, err, target error, msgAndArgs ...any) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Fatalf("error %v (%T) is not %v%s", err, err, target, message(msgAndArgs))
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
