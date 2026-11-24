package base

import (
	"fmt"
	"strings"
	"time"

	"github.com/ngicks/kama/kamalib/pkg/genheader"
)

// header returns the comment lines a generated file starts with: the
// generated-code line naming generator, then params sorted by key with each
// value as JSON, then the time now in UTC to the second. The first line
// follows the convention of go help generate.
func header(generator string, params map[string]any, now time.Time) ([]string, error) {
	comment, err := genheader.GeneratorParams{
		Name:      generator,
		Params:    params,
		Generated: now,
	}.Comment()
	if err != nil {
		return nil, fmt.Errorf("base: file header: %w", err)
	}

	return strings.Split(comment, "\n"), nil
}
