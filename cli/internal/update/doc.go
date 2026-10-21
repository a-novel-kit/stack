// Package update implements a best-effort "a newer version is available" notice
// for the a-novel CLI. Every failure, from an offline network to an unwritable
// cache, is swallowed, so a version check never fails a command. Every fetch is
// cached, and the cached value drives the notice for checkInterval. A failed
// fetch caches no version, so an unreachable proxy shows no notice and costs one
// httpTimeout per checkInterval.
package update
