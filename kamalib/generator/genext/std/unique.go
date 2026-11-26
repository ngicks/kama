package std

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

const uniquePath = "unique"

// Unique is the genext.TypeSupport of unique.Handle[T]. It implements
// genext.Holder.
type Unique struct{}

// Name returns "unique".
func (Unique) Name() string { return uniquePath }

// Held tells the value a unique.Handle[T] holds, which Value returns. The
// role of the value is "t". The guard leaves out the zero Handle, whose
// Value panics.
func (Unique) Held(t types.Type, v genext.Gen) (string, types.Type, genext.Gen, genext.Gen, bool) {
	if !typequery.IsNamed(t, uniquePath, "Handle") {
		return "", nil, nil, nil, false
	}

	guard := func(c genext.Context) (string, error) {
		typ, err := c.Package().Type(t)
		if err != nil {
			return "", err
		}

		return lang.Expr("%s != (%s{})", v, typ)(c)
	}

	return "t", typequery.TypeArg(t), guard, lang.Expr("%s.Value()", v), true
}
