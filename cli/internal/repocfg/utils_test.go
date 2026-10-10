package repocfg_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// loadWorkflow decodes the governance workflow template name into out.
func loadWorkflow(t *testing.T, name string, out any) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("templates", "governance", name))
	if err != nil {
		panic(err)
	}
	if err := yaml.Unmarshal(content, out); err != nil {
		panic(err)
	}
}

func runGovernanceScript(t *testing.T, script string, environment []string) (string, string, error) {
	t.Helper()
	directory := t.TempDir()
	stub := `#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >> "$CALLS"
case "$*" in
 *enqueuePullRequest*) printf '{}\n';;
 *'/reviews'*) printf '{}\n';;
 *graphql*) printf '%s\n' "$CURRENT";;
 *'/pulls') printf '%s\n' "$CANDIDATES";;
 *'/commits') printf '%s\n' "$COMMITS";;
 *) exit 91;;
esac
`
	if err := os.WriteFile(filepath.Join(directory, "gh"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(directory, "output")
	callsPath := filepath.Join(directory, "calls")
	for _, p := range []string{outputPath, callsPath} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), "bash", "-c", script)
	command.Env = append(os.Environ(), "PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_TOKEN=fixture", "REPOSITORY=example/repo", "GITHUB_OUTPUT="+outputPath, "CALLS="+callsPath)
	command.Env = append(command.Env, environment...)
	logs, err := command.CombinedOutput()
	output, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	calls, readErr := os.ReadFile(callsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(logs) + string(output), string(calls), err
}
