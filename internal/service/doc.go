// Package service coordinates world sessions and cancellable export builds.
// HTTP is an adapter over the same operations used by the CLI. Only one export
// build owns mutable session output at a time; preview tiles use a separate,
// bounded worker pool and borrow immutable resource archives while rendering.
package service
