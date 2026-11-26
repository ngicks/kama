package basic

import (
	"encoding/json/jsontext"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/validator/ext"
	"github.com/ngicks/kama/kamalib/pkg/dircmnt/expr"
)

type spelled interface{ Call() (expr.Call, error) }

// spellCase is the args of a validator, the parameters that bind them and
// the text Call spells them as.
type spellCase struct {
	args   spelled
	params ext.Params
	text   string
}

var ignorePositions = cmpopts.IgnoreTypes(expr.Position{})

func mustCall(t *testing.T, a spelled) expr.Node {
	t.Helper()

	c, err := a.Call()
	assert.NilError(t, err)

	return c
}

// TestArgsCall spells the args of every validator of the default registry,
// parses the text back, resolves it and binds it to the same args.
func TestArgsCall(t *testing.T) {
	str := func(s string) *string { return &s }
	kindString := mustCall(t, KindArgs{Kinds: []string{"string"}})
	never := mustCall(t, NeverArgs{})
	prefixArgs := PrefixArgs{Operands: []expr.Node{
		kindString,
		mustCall(t, RestArgs{Operands: []expr.Node{never}}),
	}}

	cases := map[string]spellCase{
		"always": {AlwaysArgs{}, noParams, "always"},
		"closed": {ClosedArgs{}, noParams, "closed"},
		"nil":    {NilArgs{}, noParams, "nil"},
		"never":  {NeverArgs{}, noParams, "never"},
		"null":   {NullArgs{}, noParams, "null"},
		"unique": {UniqueArgs{}, noParams, "unique"},
		"zero":   {ZeroArgs{}, noParams, "zero"},
		"required": {
			RequiredArgs{Names: []string{"a", "b c"}},
			namesParams,
			`required("a", "b c")`,
		},
		"requires": {RequiresArgs{Names: []string{"city"}}, namesParams, `requires("city")`},
		"cmp": {
			CmpArgs{Ge: jsontext.Value("1"), Lt: jsontext.Value("1e3")}, compareParams,
			"cmp(ge=1, lt=1e3)",
		},
		"length": {LengthArgs{Eq: jsontext.Value("3")}, compareParams, "length(3)"},
		"runes": {
			RunesArgs{Ge: jsontext.Value("1"), Le: jsontext.Value("10")}, compareParams,
			"runes(ge=1, le=10)",
		},
		"members": {
			MembersArgs{Gt: jsontext.Value("0"), Ne: jsontext.Value("[2,4]")}, compareParams,
			"members(gt=0, ne=[2, 4])",
		},
		"contains": {
			ContainsArgs{
				Operands: []expr.Node{kindString},
				Ge:       jsontext.Value("2"),
			},
			containsParams,
			`contains(kind("string"), ge=2)`,
		},
		"const": {
			ConstArgs{Values: []jsontext.Value{
				jsontext.Value("1"), jsontext.Value("null"), jsontext.Value(`"a"`),
				jsontext.Value("[1,true]"), jsontext.Value(`{"a":1}`),
			}},
			constParams,
			`const(1, None, "a", [1, True], {"a": 1})`,
		},
		"multipleof": {
			MultipleOfArgs{N: jsontext.Value("0.01")},
			multipleOfParams,
			"multipleof(0.01)",
		},
		"regexp": {RegexpArgs{Pattern: `^\d+$`}, regexpParams, `regexp(r"^\d+$")`},
		"format": {FormatArgs{Name: "date-time"}, formatParams, `format("date-time")`},
		"kind": {
			KindArgs{Kinds: []string{"integer", "null"}},
			kindParams,
			`kind("integer", "null")`,
		},
		"ref":    {RefArgs{Type: "Tree"}, typeParams, `ref("Tree")`},
		"decode": {DecodeArgs{Type: "Pet"}, typeParams, `decode("Pet")`},
		"items": {
			ItemsArgs{Operands: []expr.Node{kindString}},
			operandArgs,
			`items(kind("string"))`,
		},
		"keys": {
			KeysArgs{Operands: []expr.Node{kindString}},
			operandArgs,
			`keys(kind("string"))`,
		},
		"json": {
			JsonArgs{Operands: []expr.Node{kindString}},
			operandArgs,
			`json(kind("string"))`,
		},
		"oneof": {
			OneOfArgs{Operands: []expr.Node{kindString, never}}, operandArgs,
			`oneof(kind("string"), never)`,
		},
		"prefix": {prefixArgs, operandArgs, `prefix(kind("string"), rest(never))`},
		"props": {
			PropsArgs{Entries: []expr.Node{
				mustCall(t, FieldArgs{Name: str(""), Operands: []expr.Node{never}}),
				mustCall(t, FieldArgs{Name: str("a")}),
				mustCall(t, MatchArgs{Name: str("^x-"), Operands: []expr.Node{kindString}}),
				mustCall(t, RestArgs{}),
			}},
			propsParams,
			`props(field("", never), field("a"), match("^x-", kind("string")), rest)`,
		},
		"unevalprops": {
			UnevalPropsArgs{Scope: kindString, Operands: []expr.Node{never}}, unevalParams,
			`unevalprops(kind("string"), never)`,
		},
		"unevalitems": {
			UnevalItemsArgs{Scope: mustCall(t, prefixArgs)}, unevalParams,
			`unevalitems(prefix(kind("string"), rest(never)))`,
		},
	}
	reg := ext.DefaultRegistry()

	for _, name := range reg.Names() {
		t.Run(name, func(t *testing.T) {
			tc, ok := cases[name]
			if !ok {
				t.Fatalf("no case spells the args of %s", name)
			}

			c, err := tc.args.Call()
			assert.NilError(t, err)
			assert.Equal(t, c.Name, name)

			text := expr.Print([]expr.Arg{{Value: c}})
			assert.Equal(t, text, tc.text)

			parsed, err := expr.Parse(text)
			assert.NilError(t, err)
			assert.DeepEqual(t, parsed[0].Value, expr.Node(c), ignorePositions)

			_, err = ext.Resolve(reg, parsed[0].Value)
			assert.NilError(t, err)

			got := reflect.New(reflect.TypeOf(tc.args))
			_, err = tc.params.Bind(reg, parsed[0].Value.(expr.Call), got.Interface())
			assert.NilError(t, err)
			assert.DeepEqual(t, got.Elem().Interface(), tc.args, ignorePositions)
		})
	}
}

func TestOperators(t *testing.T) {
	a, b := mustCall(t, NullArgs{}), mustCall(t, ZeroArgs{})

	for _, tc := range []struct {
		node expr.Node
		text string
	}{
		{All(), "always"},
		{All(a), "null"},
		{All(a, All(a, b)), "null and (null and zero)"},
		{Any(), "never"},
		{Any(a, b), "null or zero"},
		{Negate(Any(a, b)), "not (null or zero)"},
		{Cond(a, b, nil), "zero if null else always"},
		{Cond(a, nil, b), "always if null else zero"},
	} {
		text := tc.node.String()
		assert.Equal(t, text, tc.text)

		parsed, err := expr.Parse(text)
		assert.NilError(t, err)
		assert.DeepEqual(t, parsed[0].Value, tc.node, ignorePositions)
	}
}
