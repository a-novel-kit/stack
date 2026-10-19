// Package update implements a best-effort "a newer version is available" notice
// for the a-novel CLI. Every failure, from an offline network to an unwritable
// cache, is swallowed, because a version check must never disrupt or slow a
// command. The latest version is cached, so the network is hit at
// most once per checkInterval and the cached value drives the notice between
// fetches.
package update
