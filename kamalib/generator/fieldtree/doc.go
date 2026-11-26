// Package fieldtree draws the fields of a struct type as a tree of text, for
// a help text that tells a reader how a Go text/template reaches each
// field of a value, as in --format '{{.Server.TLS}}'. It writes no file:
// the proc fieldtree.text hands the trees to a later step, such as
// textmod.replace, which puts a tree into a help text.
//
// # Format
//
// The first line is the label of the target, the name of the type by
// default. Each line below it shows one value and has four columns: the
// template expression reaching the value, its type, a # and a description,
// and the key path of the value in the serialized form in parentheses.
// The lines of a value hang below it, drawn with ├─, └─ and │. The
// expression and type columns of the lines hanging below one value are
// aligned. The # and the key path each sit in one column for the whole
// tree. The description is indented by two spaces for each level a line
// sits below the fields of the target. Every line ends with a newline and
// carries no trailing space.
//
// For this source
//
//	//kama:fieldtree config
//	type Config struct {
//		// Addr is the listen address.
//		Addr  string            `json:"addr"`
//		Users []User            `json:"users"` // users allowed in
//		Env   map[string]string `json:"env"`   // environment
//		TLS   opt.Option[TLS]   `json:"tls"`   // TLS settings
//		Root  *Dir              `json:"root"`  // served directory
//	}
//
//	type User struct {
//		Name string `json:"name"` // login name
//	}
//
//	type TLS struct {
//		Cert string `json:"cert"` // certificate file
//	}
//
//	type Dir struct {
//		Path string `json:"path"` // path on disk
//		Sub  []Dir  `json:"sub"`  // subdirectories
//	}
//
// the tree of config is
//
//	Config
//	├─ .Addr   string                  # Addr is the listen address.     (addr)
//	├─ .Users  []User                  # users allowed in                (users)
//	│   └─ index .Users i  User        #   element i; range .Users       (users[i])
//	│       └─ .Name  string           #     login name                  (users[i].name)
//	├─ .Env    map[string]string       # environment                     (env)
//	│   └─ index .Env key  string      #   value at key; range .Env      (env[key])
//	├─ .TLS    opt.Option[TLS]         # TLS settings                    (tls)
//	│   └─ .TLS.Value  TLS             #   set only when .TLS.IsSome     (tls)
//	│       └─ .Cert  string           #     certificate file            (tls.cert)
//	└─ .Root   *Dir                    # served directory                (root)
//	    ├─ .Path  string               #   path on disk                  (root.path)
//	    └─ .Sub   []Dir                #   subdirectories                (root.sub)
//	        └─ index .Root.Sub i  Dir  #     element i; range .Root.Sub  (root.sub[i])
//	            └─ Dir repeats; it is expanded above
//
// # Fields
//
// A field line shows the field as a template selects it on the value
// above, as .Addr, and the type of the field. Types of the package of the
// target carry no package name. The description is the first sentence of
// the doc comment of the field, else its line comment, else empty.
//
// The key path comes from a struct tag, json by default: the name before
// the first comma, or the Go field name when the tag gives none. Nested
// names are joined by a dot. A field tagged - is left out, and so is an
// unexported field. An embedded struct, or a pointer to one, whose tag
// gives no name lends its fields to the struct holding it, as
// encoding/json inlines them. Name conflicts between such fields are not
// resolved. An embedded struct with a name in the tag is a field like any
// other.
//
// # Walking into values
//
// The walk continues below a value by its type:
//
//   - A struct lists its fields one level down. A named struct of another
//     package is walked when the syntax of that package is loaded, as it
//     is for every package a load with dependencies reaches, and is a leaf
//     otherwise.
//   - A slice, an array or a map gets one line below it telling that the
//     index operator reaches an element: index .Users i, or index .Env key
//     for a map, and that range walks the elements. The walk continues into the
//     element type below that line. Nested containers repeat the rule and
//     name their indexes i, i2, i3 and so on, as index (index .Grid i) i2.
//     The key path adds [i] or [key].
//   - A type a type support of genext claims, such as opt.Option[T],
//     und.Und[T], elastic.Elastic[T], either.Either[L, R], either.Result[T],
//     tuple.Tuple2[V1, V2], sql.Null[T] or unique.Handle[T], gets one line
//     per value it holds, as genext.Roles lists them. The line tells how a
//     template reaches the value, as .TLS.Value, and when the value is
//     there, as "set only when .TLS.IsSome". The walk continues into the
//     held type below that line. A support answers through ext.Reacher. A
//     claimed type whose support does not implement it gets a line naming
//     the role in parentheses and the held type.
//   - A pointer is shown with its type and walked as the type it points to.
//   - A type parameter that is not instantiated stops the walk with a line
//     saying so. A generic struct is walked through its type arguments
//     where a field instantiates it.
//   - A named type already on the path from the root is not expanded
//     again. A line saying that it repeats stands below it.
//
// Every other type is a leaf: a type with a MarshalJSON, MarshalJSONTo or
// MarshalText method on its value or its pointer, such as time.Time,
// netip.Addr or big.Int, a []byte, a basic type, an interface, a func and
// a chan. The type supports come before the marshalers, so opt.Option is
// walked although it marshals itself. The target itself is always walked.
//
// # Directive
//
// A struct type carries the directive
//
//	//kama:fieldtree <id>[, key="yaml"][, label="App config"]
//
// in the doc comment of its declaration or of its spec inside a group. Its
// args are Starlark expressions as package
// github.com/ngicks/kama/kamalib/pkg/dircmnt/expr parses them. The id is
// the first positional item: a bare word or a string. A bare id holding a
// - is an error that says to quote it. key names the struct tag of the key
// paths, json by default, and label replaces the first line. Another
// keyword is warned about with ErrUnknownKey. A directive on anything but
// one type spec, and one on a type that is no struct, is an error.
//
// An id names one tree. The directives are taken in the order of package
// path, file name and position, then the targets of the param in id
// order. The first target of an id takes it, and the id given to several
// targets is warned about with ErrDuplicateId.
//
// # Proc
//
// The proc, the workflow step kind fieldtree.text, takes the packages a
// packages.load step loaded as its in and has no out. Its AsInput value is
// the Texts of the run, a map from id to tree. Its param has one key:
//
//   - types: a map from an id to a target named by type, for a type that
//     cannot carry a directive. type is the import path of the package and
//     the name of the type joined by a dot, as example.com/app.Config. key
//     and label are those of the directive.
//
// A key of the param that names nothing is warned about. A later step
// names the step in its optional list and reads a tree through the
// template function input of textmod:
//
//	version: 1
//	steps:
//	  root:
//	    proc: overlayfs.open
//	  load:
//	    proc: packages.load
//	    in: root
//	  tree:
//	    proc: fieldtree.text
//	    in: load
//	  replace:
//	    proc: textmod.replace
//	    in: load
//	    out: root
//	    optional: [tree]
//	    param:
//	      mods:
//	        help: |
//	          {{ .Value | replaceBetween "Fields:" "Example:" (index (input "tree") "config" |
//
// indent "  ") }}
//
//	final:
//	  proc: overlayfs.copy_down
//	  in: root
//	  after: [replace]
//
// The proc asks the default registry of genext. A text extension file
// describes a type for fieldtree through its section fieldtree, as package
// ext documents. The proc reads no text extension file yet. A caller of
// Generate puts the supports of such files into Input.Registry.
package fieldtree
