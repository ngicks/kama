package validator

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestHooks(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// typeName is the type whose Validate is checked.
		typeName string
		want     []string
		notWant  []string
	}{
		{
			name: "on by default for a marked type",
			src: "//kama:validate\ntype A struct {\n\tName string `json:\"name\"`\n\tage  int\n}\n\n" +
				"func (A) validateName(bool) error { return nil }\n\n" +
				"func (A) validateAge(bool) error { return nil }\n",
			typeName: "A",
			want: []string{
				check(`validation.Field(v.validateName(stopOnFirstError), "name")`),
				check(`validation.Field(v.validateAge(stopOnFirstError), "age")`),
			},
		},
		{
			name: "off by type",
			src: "//kama:validate\n//kama:validate:hook off\ntype A struct {\n\tN int\n" +
				"\t//kama:validate:hook on\n\tM int\n}\n\n" +
				"func (A) validateM(bool) error { return nil }\n",
			typeName: "A",
			want:     []string{"v.validateM(stopOnFirstError)"},
			notWant:  []string{"validateN"},
		},
		{
			name: "off by field",
			src: "//kama:validate\ntype A struct {\n\t//kama:validate:hook off\n\tN int\n\tM int\n}\n\n" +
				"func (A) validateM(bool) error { return nil }\n",
			typeName: "A",
			want:     []string{"v.validateM(stopOnFirstError)"},
			notWant:  []string{"validateN"},
		},
		{
			name: "off for a dependant",
			src: "type P struct{ C C }\n\n" +
				"//kama:validate\n//kama:validate:hook off\ntype C struct{ X int }\n",
			typeName: "P",
			notWant:  []string{"validateC"},
		},
		{
			name: "on for a dependant",
			src: "//kama:validate:hook on\ntype P struct{ C C }\n\n" +
				"func (P) validateC(bool) error { return nil }\n\n" +
				"//kama:validate\n//kama:validate:hook off\ntype C struct{ X int }\n",
			typeName: "P",
			want: []string{
				check(`validation.Field(v.C.Validate(stopOnFirstError), "C")`),
				check(`validation.Field(v.validateC(stopOnFirstError), "C")`),
			},
		},
		{
			// Validators, nested validation and the hook run in that order.
			name: "order",
			src: "//kama:validate\ntype A struct {\n\t//kama:validate:required\n\tC *C\n}\n\n" +
				"func (A) validateC(bool) error { return nil }\n\n" +
				"//kama:validate\n//kama:validate:hook off\ntype C struct{ X int }\n",
			typeName: "A",
			want: []string{
				`kamaValidateRequired(v.C != nil, "nil")`,
				"(*v.C).Validate(stopOnFirstError)",
				"v.validateC(stopOnFirstError)",
			},
		},
		{
			name: "generic",
			src: "//kama:validate\ntype B[T any] struct{ V T }\n\n" +
				"func (B[T]) validateV(bool) error { return nil }\n",
			typeName: "B",
			want:     []string{"v.validateV(stopOnFirstError)"},
		},
		{
			// Fields name and Name share validateName, which only one of them
			// may call.
			name: "shared hook off for one field",
			src: "//kama:validate\ntype A struct {\n\t//kama:validate:hook off\n\tname string\n" +
				"\tName string `json:\"Name2\"`\n}\n\n" +
				"func (A) validateName(bool) error { return nil }\n",
			typeName: "A",
			want: []string{
				check(`validation.Field(v.validateName(stopOnFirstError), "Name2")`),
			},
		},
		{
			// An embedded struct without json name adds no segment, so its hook
			// error is located at the outer value.
			name: "embedded field",
			src: "//kama:validate\ntype A struct {\n\tInner\n\t*Ptr\n}\n\n" +
				"func (A) validateInner(bool) error { return nil }\n\n" +
				"func (A) validatePtr(bool) error { return nil }\n\n" +
				"type Inner struct{ X int }\n\ntype Ptr struct{ Y int }\n",
			typeName: "A",
			want: []string{
				check("v.validateInner(stopOnFirstError)"),
				check("v.validatePtr(stopOnFirstError)"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn := validateOf(t, map[string]string{"a.go": "package x\n\n" + tc.src},
				"a.kama_validate.go", tc.typeName)
			assertContains(t, fn, tc.want...)
			assertNotContains(t, fn, tc.notWant...)
		})
	}

	// The steps of a field keep their order.
	fn := validateOf(t, map[string]string{"a.go": "package x\n\n" + cases[5].src},
		"a.kama_validate.go", "A")
	assert.Assert(t, strings.Index(fn, "kamaValidateRequired") < strings.Index(fn, ".Validate(") &&
		strings.Index(fn, ".Validate(") < strings.Index(fn, "validateC"), fn)
}

func TestHookErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		msgs []string
	}{
		{
			name: "missing",
			src:  "//kama:validate\ntype A struct {\n\tName string\n}\n",
			msgs: []string{
				"a.go:5:2", "field Name of", "A has no hook validateName",
				"declare func (A) validateName(stopOnFirstError bool) error",
				"//kama:validate:hook off on the field or the type",
			},
		},
		{
			name: "wrong signature",
			src: "//kama:validate\ntype A struct{ N int }\n\n" +
				"func (A) validateN() error { return nil }\n",
			msgs: []string{"has its hook validateN with signature func() error"},
		},
		{
			name: "pointer receiver",
			src: "//kama:validate\ntype A struct{ N int }\n\n" +
				"func (*A) validateN(bool) error { return nil }\n",
			msgs: []string{"has its hook validateN on the pointer receiver"},
		},
		{
			name: "generic receiver spelled",
			src:  "//kama:validate\ntype A[T any, U comparable] struct{ N int }\n",
			msgs: []string{"declare func (A[T, U]) validateN(stopOnFirstError bool) error"},
		},
		{
			// Every missing hook is reported.
			name: "several",
			src:  "//kama:validate\ntype A struct{ N, M int }\n",
			msgs: []string{"no hook validateN", "no hook validateM"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate(t, map[string]string{"a.go": "package x\n\n" + tc.src}, Options{})
			assert.ErrorIs(t, err, ErrHook)

			for _, msg := range tc.msgs {
				assert.ErrorContains(t, err, msg)
			}
		})
	}
}
