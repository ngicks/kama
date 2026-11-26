module github.com/ngicks/kama/kamalib

go 1.27.0

require (
	github.com/dave/dst v0.27.3
	github.com/google/go-cmp v0.7.0
	github.com/ngicks/go-common/exver v1.2.0
	github.com/ngicks/go-fsys-helper/vroot v0.0.2
	github.com/ngicks/go-fsys-helper/vroot-adapter/overlayfs v0.0.0-20261115192526-4e496cb4e8cc
	github.com/ngicks/kama/kamaruntime v0.0.0
	github.com/ngicks/opiter v0.0.0-20261125032700-03a99644b6ce
	github.com/ngicks/option v0.0.5
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/rivo/uniseg v0.4.7
	github.com/spf13/pflag v1.0.10
	go.starlark.net v0.0.0-20261021141710-4f4267fb140d
	go.yaml.in/yaml/v4 v4.0.0-rc.5
	golang.org/x/sync v0.24.0
	golang.org/x/tools v0.52.0
	gotest.tools/v3 v3.5.2
)

require (
	github.com/ncruces/go-sqlite3 v0.35.3 // indirect
	github.com/ncruces/go-sqlite3-wasm/v3 v3.2.35304 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/ngicks/go-fsys-helper/fsutil v0.0.0-20260816154526-c6c8f4c0c36f // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

// The runtime module lives next to this one and is not released.
replace github.com/ngicks/kama/kamaruntime => ../kamaruntime
