package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// Flags covers every flag type and the field types flags unwrap.
type Flags struct {
	Name     string            `json:"name"`
	Verbose  bool              `json:"verbose"`
	Count    int               `json:"count"`
	Size     int64             `json:"size"`
	Workers  uint              `json:"workers"`
	Limit    uint64            `json:"limit"`
	Ratio    float64           `json:"ratio"`
	Timeout  time.Duration     `json:"timeout"`
	Tags     []string          `json:"tags"`
	Labels   map[string]string `json:"labels"`
	Level    LogLevel          `json:"level"`
	Since    time.Time         `json:"since"`
	Mode     Mode              `json:"mode"`
	Hosts    Hosts             `json:"hosts"`
	Retry    opt.Option[int]   `json:"retry"`
	Nick     *string           `json:"nick"`
	Port     und.Und[int]      `json:"port"`
	MinLevel *LogLevel         `json:"min_level"`

	Unflagged string  `json:"unflagged"`
	Nested    Address `json:"nested"`
}

// LogLevel is set from its text form.
type LogLevel int

const (
	LogLevelInfo LogLevel = iota
	LogLevelDebug
)

func (l *LogLevel) UnmarshalText(text []byte) error {
	switch strings.ToLower(string(text)) {
	case "info":
		*l = LogLevelInfo
	case "debug":
		*l = LogLevelDebug
	default:
		return fmt.Errorf("unknown level %q", text)
	}
	return nil
}

// Mode is a named string.
type Mode string

// Hosts is a named string slice. A slice type reached from a target is a
// target, whose fields take no flags, so it opts out.
type Hosts []string
