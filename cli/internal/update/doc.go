// Package update implements a best-effort "a newer version is available" notice
// for the a-novel CLI. Every failure, from an offline network to an unwritable
// cache, is swallowed, so a version check never fails a command. A successful
// fetch is cached, and the cached value drives the notice for checkInterval. A
// failed fetch caches nothing, so the next command tries again, bounded by
// httpTimeout.
package update
