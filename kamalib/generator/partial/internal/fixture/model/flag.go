package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/ngicks/option/opt"
	"github.com/ngicks/option/und"
)

// Flags covers every flag type and the field types flags unwrap.
//
//kama:partial
type Flags struct {
	//kama:partial:flag name=name short=n usage="the name"
	Name string `json:"name"`
	//kama:partial:flag name=verbose short=v
	Verbose bool `json:"verbose"`
	//kama:partial:flag name=count
	Count int `json:"count"`
	//kama:partial:flag name=size
	Size int64 `json:"size"`
	//kama:partial:flag name=workers
	Workers uint `json:"workers"`
	//kama:partial:flag name=limit
	Limit uint64 `json:"limit"`
	//kama:partial:flag name=ratio
	Ratio float64 `json:"ratio"`
	//kama:partial:flag name=timeout short=t usage="time to wait"
	Timeout time.Duration `json:"timeout"`
	//kama:partial:flag name=tag
	Tags []string `json:"tags"`
	//kama:partial:flag name=label
	Labels map[string]string `json:"labels"`
	//kama:partial:flag name=level short=l
	Level LogLevel `json:"level"`
	//kama:partial:flag name=since
	Since time.Time `json:"since"`
	//kama:partial:flag name=mode
	Mode Mode `json:"mode"`
	//kama:partial:flag name=host
	Hosts Hosts `json:"hosts"`
	//kama:partial:flag name=retry
	Retry opt.Option[int] `json:"retry"`
	//kama:partial:flag name=nick
	Nick *string `json:"nick"`
	//kama:partial:flag name=port
	Port und.Und[int] `json:"port"`
	//kama:partial:flag name=min-level
	MinLevel *LogLevel `json:"min_level"`

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
//
//kama:partial ignore
type Hosts []string
