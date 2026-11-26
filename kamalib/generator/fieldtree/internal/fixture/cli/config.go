// Package cli is a small command whose help text lists the fields of its
// config, as a --format template reads them.
package cli

import "github.com/ngicks/option/opt"

// Config is the configuration of the command.
//
//kama:fieldtree config
type Config struct {
	Addr string `json:"addr"` // listen address
	Port int    `json:"port"` // listen port
	// Server configures the server.
	Server Server            `json:"server"`
	Labels map[string]string `json:"labels"` // labels
	Hosts  []string          `json:"hosts"`  // hosts
	// Timeout is the request timeout in seconds.
	Timeout opt.Option[int] `json:"timeout"`
}

// Server is the server sub-config.
type Server struct {
	TLS     bool `json:"tls"`     // enable TLS
	Timeout int  `json:"timeout"` // timeout seconds
}
