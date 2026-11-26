package expr_test

import (
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

func Example() {
	args, err := expr.Parse(`required, hook=off, length(min=1, max=3) or regexp(r"^\d+$")`)
	if err != nil {
		panic(err)
	}

	for _, a := range args {
		fmt.Printf("%q: %T %s\n", a.Name, a.Value, a.Value)
	}

	fmt.Println(expr.Print(args))

	// Output:
	// "": expr.Call required
	// "hook": expr.Call off
	// "": expr.Or length(min=1, max=3) or regexp(r"^\d+$")
	// required, hook=off, length(min=1, max=3) or regexp(r"^\d+$")
}

func ExampleObject() {
	args, err := expr.Parse(`field("name", required, kind("string")), length(1, max=3)`)
	if err != nil {
		panic(err)
	}

	field := args[0].Value.(expr.Call)

	obj, rules, err := expr.Object(field, []string{"name"}, "operands")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(obj))

	for _, r := range rules {
		fmt.Println(r)
	}

	var length struct {
		Min int64 `json:"min"`
		Max int64 `json:"max"`
	}

	obj, _, err = expr.Object(args[1].Value.(expr.Call), []string{"min", "max"}, "")
	if err != nil {
		panic(err)
	}

	if err := jsonv2.Unmarshal(obj, &length); err != nil {
		panic(err)
	}

	fmt.Println(length.Min, length.Max)

	// Output:
	// {"name":"name"}
	// operands=required
	// operands=kind("string")
	// 1 3
}

func ExampleParse_error() {
	_, err := expr.Parse(`required, length(min=1) == 2`)
	fmt.Println(err)

	// Output:
	// expr: 1:25: operator == is not supported
}
