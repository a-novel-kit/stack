// Package stacks parses and validates the A_NOVEL_STACKS environment variable,
// the daemon's single source of truth for which checkouts to manage.
//
// The variable reads "name1:/path1,name2:/path2", and its first entry is the
// default stack. Unset or empty, it names a single "default" stack at
// ~/git-projects/a-novel.
package stacks
