// Package detect discovers the build and test targets under a directory tree:
// Go modules, package.json scripts and Dockerfiles, laid out the way every
// A-Novel repository lays them out.
//
// Discovery recurses from the scan root, so nested modules and workspace
// packages are found, and prunes hidden, git-ignored and vendored trees.
package detect
