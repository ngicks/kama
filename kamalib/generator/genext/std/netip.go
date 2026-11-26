package std

import (
	"go/types"

	"github.com/ngicks/kama/kamalib/generator/genext"
	"github.com/ngicks/kama/kamalib/pkg/typequery"
)

const netipPath = "net/netip"

// Netip is the genext.TypeSupport of netip.Addr and netip.Prefix, whose zero
// value is the one that is not valid. It implements ext.ZeroTester and
// ext.PresenceTester.
type Netip struct{}

// Name returns "net/netip".
func (Netip) Name() string { return netipPath }

// Zero tells an Addr or a Prefix being zero by IsValid unset.
func (Netip) Zero(t types.Type, v genext.Gen) (genext.Gen, string, bool) {
	name, ok := netipName(t)
	if !ok {
		return nil, "", false
	}

	return lang.Expr("!%s.IsValid()", v), "a valid " + name, true
}

// Presence tells an Addr or a Prefix holding a value by IsValid.
func (Netip) Presence(t types.Type, v genext.Gen) (genext.Gen, string, bool) {
	name, ok := netipName(t)
	if !ok {
		return nil, "", false
	}

	return lang.Expr("%s.IsValid()", v), "an invalid " + name, true
}

// netipName returns the name of t, netip.Addr or netip.Prefix, or false
// for another type.
func netipName(t types.Type) (string, bool) {
	for _, name := range []string{"Addr", "Prefix"} {
		if typequery.IsNamed(t, netipPath, name) {
			return name, true
		}
	}

	return "", false
}
