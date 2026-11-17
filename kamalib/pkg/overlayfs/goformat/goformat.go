// Package goformat formats Go source, in process or through a command.
package goformat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/tools/imports"
)

var ErrEmptyArgv = errors.New("goformat: empty argv")

// Imports formats like goimports: it formats src and adds or removes import
// lines as needed.
type Imports struct{}

// Format formats src. path is the file's absolute path; Imports uses it to
// resolve the module the file belongs to.
func (Imports) Format(_ context.Context, path string, src []byte) ([]byte, error) {
	return imports.Process(path, src, nil)
}

// Command formats through an external command. The command reads the source
// on stdin and writes the formatted source to stdout, as gofmt and goimports
// do without file arguments.
type Command struct {
	Argv []string
	// Dir is the working directory of the command. Empty means the working
	// directory of the calling process.
	Dir string
}

// Format runs the command with src on stdin. path is not passed to the
// command.
func (c Command) Format(ctx context.Context, _ string, src []byte) ([]byte, error) {
	if len(c.Argv) == 0 || c.Argv[0] == "" {
		return nil, ErrEmptyArgv
	}

	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Dir = c.Dir
	cmd.Stdin = bytes.NewReader(src)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("goformat: %q: %w", c.Argv[0], err)
		}

		return nil, fmt.Errorf("goformat: %q: %w: %s", c.Argv[0], err, msg)
	}

	return stdout.Bytes(), nil
}
