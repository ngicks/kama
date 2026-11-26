// Package version holds the values the fixture spec.yaml of textmod sets.
package version

// Version is the version of the fixture.
//
//kama:textmod version, format="ver"
const Version = "v1.10.0"

const (
	// Name is the name of the fixture.
	//kama:textmod name
	Name  = "fixture"
	Build = 3 //kama:textmod build
)

// Help is the help text of the fixture.
//
//kama:textmod help
const Help = `usage: fixture
# begin
  -v  print the version
# end
`
