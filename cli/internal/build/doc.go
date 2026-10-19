// Package build turns a detected target into a [jobs.Job]: it assembles the
// target's environment, brings its compose env up and down around the command,
// and enforces the per-target deadline.
package build
