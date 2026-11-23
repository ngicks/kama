// Package typequery answers small questions about go/types types that the
// generators share.
//
// [IsNamed] tells a named type by its package path and name, through
// aliases. [TypeArg] returns the first type argument of an instantiated
// type. [IsStructSliceOrMap] tells the underlying types the generators
// write declarations for. [OptPath], [UndPath] and [ElasticPath] are the
// import paths of the option types the generators recognize.
package typequery
