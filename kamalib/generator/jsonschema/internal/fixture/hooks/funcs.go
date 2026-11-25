package hooks

import "errors"

// calls counts the calls of each function by name. failing names the
// functions that fail.
var (
	calls   = map[string]int{}
	failing = map[string]bool{}
)

func validateOrderPre(Order, bool) error  { return call("validateOrderPre") }
func validateOrderPost(Order, bool) error { return call("validateOrderPost") }
func validateLinePre(Line, bool) error    { return call("validateLinePre") }
func validateLinePost(Line, bool) error   { return call("validateLinePost") }
func validateSkuPre(Sku, bool) error      { return call("validateSkuPre") }
func validateSkuPost(Sku, bool) error     { return call("validateSkuPost") }

func call(name string) error {
	calls[name]++
	if failing[name] {
		return errors.New(name)
	}

	return nil
}
