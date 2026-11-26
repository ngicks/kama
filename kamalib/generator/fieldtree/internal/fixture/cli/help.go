package cli

// Help is the help text of the command. The step replace of spec.yaml
// writes the tree of Config between the lines Fields: and Example:.
//
//kama:textmod help
const Help = `Usage: app [flags]

Prints the config as --format renders it, a text/template run on Config.

Fields:
  Config
  ├─ .Addr     string             # listen address                              (addr)
  ├─ .Port     int                # listen port                                 (port)
  ├─ .Server   Server             # Server configures the server.               (server)
  │   ├─ .TLS      bool  # enable TLS       (server.tls)
  │   └─ .Timeout  int   # timeout seconds  (server.timeout)
  ├─ .Labels   map[string]string  # labels                                      (labels)
  │   └─ index .Labels key  string  # value at key; range .Labels  (labels[key])
  ├─ .Hosts    []string           # hosts                                       (hosts)
  │   └─ index .Hosts i  string  # element i; range .Hosts  (hosts[i])
  └─ .Timeout  opt.Option[int]    # Timeout is the request timeout in seconds.  (timeout)
      └─ .Timeout.Value  int  # set only when .Timeout.IsSome  (timeout)
Example:
  app --format '{{.Server.TLS}}'
`
