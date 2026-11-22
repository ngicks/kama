package validation

import "strings"

// Join aggregates errs into one error. It drops nil errors and flattens values made by Join. It
// returns nil for no non-nil error. It returns the only non-nil error unchanged. The result of
// two or more non-nil errors implements Unwrap() []error.
func Join(errs ...error) error {
	var (
		flat []error
		last error
		n    int
	)

	for _, err := range errs {
		if err != nil {
			last = err
			n++
		}
	}

	if n <= 1 {
		return last
	}

	for _, err := range errs {
		switch x := err.(type) {
		case nil:
		case *joinError:
			flat = append(flat, x.errs...)
		default:
			flat = append(flat, err)
		}
	}

	return &joinError{errs: flat}
}

// joinError never shares its slice: Join and wrap always build a fresh one, so appending to a
// result cannot reach into the slice of another.
type joinError struct {
	errs []error
}

func (e *joinError) Error() string {
	msgs := make([]string, len(e.errs))
	for i, err := range e.errs {
		msgs[i] = err.Error()
	}

	return strings.Join(msgs, "\n")
}

func (e *joinError) Unwrap() []error {
	return e.errs
}
