package tests

import (
	"errors"
	"testing"
	"time"

	"github.com/ngicks/option/und"
	"github.com/spf13/pflag"
	"gotest.tools/v3/assert"

	"github.com/ngicks/kama/kamalib/generator/partial/internal/fixture/model"
)

var _ model.PartialFlagSet = (*pflag.FlagSet)(nil)

func parseFlags(t *testing.T, args ...string) (model.FlagsPartial, error) {
	t.Helper()
	fs := pflag.NewFlagSet("x", pflag.ContinueOnError)
	var p model.FlagsPartial
	p.RegisterFlags(fs)
	assert.NilError(t, fs.Parse(args))
	err := p.FromFlagSet(fs)
	return p, err
}

func TestFromFlagSet(t *testing.T) {
	p, err := parseFlags(t,
		"--name", "a", "-v", "--count=2", "--size", "3", "--workers", "4", "--limit", "5",
		"--ratio", "0.5", "-t", "1s", "--tag", "a", "--tag", "b", "--label", "k=v",
		"-l", "debug", "--since", "2020-01-02T03:04:05Z", "--mode", "m", "--host", "h",
		"--retry", "6", "--nick", "n", "--port", "8080", "--min-level", "info",
	)
	assert.NilError(t, err)

	want := model.FlagsPartial{
		Name:     und.Defined("a"),
		Verbose:  und.Defined(true),
		Count:    und.Defined(2),
		Size:     und.Defined(int64(3)),
		Workers:  und.Defined(uint(4)),
		Limit:    und.Defined(uint64(5)),
		Ratio:    und.Defined(0.5),
		Timeout:  und.Defined(time.Second),
		Tags:     und.Defined([]string{"a", "b"}),
		Labels:   und.Defined(map[string]string{"k": "v"}),
		Level:    und.Defined(model.LogLevelDebug),
		Since:    und.Defined(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)),
		Mode:     und.Defined(model.Mode("m")),
		Hosts:    und.Defined(model.Hosts{"h"}),
		Retry:    und.Defined(6),
		Nick:     und.Defined("n"),
		Port:     und.Defined(8080),
		MinLevel: und.Defined(model.LogLevelInfo),
	}
	equal(t, p, want)
}

func TestFromFlagSetUnchanged(t *testing.T) {
	p, err := parseFlags(t, "-n", "a")
	assert.NilError(t, err)
	equal(t, p, model.FlagsPartial{Name: und.Defined("a")})

	base := model.FlagsPartial{
		Name:      und.Defined("base"),
		Count:     und.Defined(9),
		Unflagged: und.Defined("u"),
	}
	equal(t, base.Merge(p), model.FlagsPartial{
		Name:      und.Defined("a"),
		Count:     und.Defined(9),
		Unflagged: und.Defined("u"),
	})
}

func TestFromFlagSetTextError(t *testing.T) {
	_, err := parseFlags(t, "--level", "nope")
	assert.ErrorContains(t, err, "flag --level")
	assert.ErrorContains(t, err, `unknown level "nope"`)
}

func TestFromFlagSetZeroValue(t *testing.T) {
	p, err := parseFlags(t, "--count=0", "--verbose=false", "--name=")
	assert.NilError(t, err)
	equal(t, p, model.FlagsPartial{
		Name:    und.Defined(""),
		Verbose: und.Defined(false),
		Count:   und.Defined(0),
	})
}

func TestFromFlagSetKeepsFields(t *testing.T) {
	fs := pflag.NewFlagSet("x", pflag.ContinueOnError)
	p := model.FlagsPartial{
		Name:      und.Defined("base"),
		Count:     und.Defined(9),
		Verbose:   und.Null[bool](),
		Unflagged: und.Defined("u"),
	}
	p.RegisterFlags(fs)
	assert.NilError(t, fs.Parse([]string{"--count", "5"}))
	assert.NilError(t, p.FromFlagSet(fs))
	equal(t, p, model.FlagsPartial{
		Name:      und.Defined("base"),
		Count:     und.Defined(5),
		Verbose:   und.Null[bool](),
		Unflagged: und.Defined("u"),
	})
}

var errGet = errors.New("get failed")

// failingGetInt is a flag set whose GetInt fails.
type failingGetInt struct {
	*pflag.FlagSet
}

func (failingGetInt) GetInt(string) (int, error) { return 0, errGet }

func TestFromFlagSetGetError(t *testing.T) {
	fs := failingGetInt{pflag.NewFlagSet("x", pflag.ContinueOnError)}
	var p model.FlagsPartial
	p.RegisterFlags(fs)
	assert.NilError(t, fs.Parse([]string{"--name", "a", "--count", "1"}))

	err := p.FromFlagSet(fs)
	assert.ErrorIs(t, err, errGet)
	assert.ErrorContains(t, err, "flag --count")
}

func TestFromFlagSetGetErrorUnchanged(t *testing.T) {
	fs := failingGetInt{pflag.NewFlagSet("x", pflag.ContinueOnError)}
	var p model.FlagsPartial
	p.RegisterFlags(fs)
	assert.NilError(t, fs.Parse([]string{"--name", "a"}))

	// GetInt is not called for a flag not set.
	assert.NilError(t, p.FromFlagSet(fs))
	equal(t, p, model.FlagsPartial{Name: und.Defined("a")})
}
