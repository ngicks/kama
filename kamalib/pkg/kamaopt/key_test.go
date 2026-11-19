package kamaopt

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestKeyCompare(t *testing.T) {
	base := Key{PkgPath: "b", File: "b.go", Type: "B", Field: "F"}

	for _, tc := range []struct {
		name  string
		other Key
		want  int
	}{
		{"equal", base, 0},
		{"pkg path first", Key{PkgPath: "a", File: "z.go", Type: "Z", Field: "Z"}, 1},
		{"file second", Key{PkgPath: "b", File: "c.go", Type: "A", Field: "A"}, -1},
		{"type third", Key{PkgPath: "b", File: "b.go", Type: "A", Field: "Z"}, 1},
		{"field last", Key{PkgPath: "b", File: "b.go", Type: "B", Field: "G"}, -1},
		{"empty field first", Key{PkgPath: "b", File: "b.go", Type: "B"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, base.Compare(tc.other), tc.want)
			assert.Equal(t, tc.other.Compare(base), -tc.want)
		})
	}
}
