// Package overlayfs is the rooted target filesystem a pipeline runs against.
//
// It stacks a writable layer over the project directory. Nodes read the
// merged view, so generated output is visible to later nodes as if it were on
// disk, and nodes emit generated results into the writable layer. Nothing
// reaches the project directory until the pipeline succeeds and the layer is
// flushed.
package overlayfs
