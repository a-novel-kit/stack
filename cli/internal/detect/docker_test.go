package detect_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		files        map[string]string
		expectName   string
		expectTag    func(string) string
		expectFile   string
		expectSecret string
	}{
		{
			name: "PlatformRootDockerfile",
			files: map[string]string{
				"Dockerfile": "FROM scratch\nRUN --mount=type=secret,id=npm_token,required=true echo ready\n",
			},
			expectName:   "Dockerfile",
			expectTag:    func(root string) string { return "localhost/" + filepath.Base(root) + ":local" },
			expectFile:   "Dockerfile",
			expectSecret: "id=npm_token,env=NPM_TOKEN",
		},
		{
			name: "NamedServiceDockerfile",
			files: map[string]string{
				"go.mod":                 "module github.com/a-novel/service-authentication/v2\n",
				"builds/rest.Dockerfile": "FROM scratch\n",
			},
			expectName: "rest.Dockerfile",
			expectTag:  func(string) string { return "ghcr.io/a-novel/service-authentication/rest:local" },
			expectFile: filepath.Join("builds", "rest.Dockerfile"),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for name, content := range testCase.files {
				mustWriteFile(t, filepath.Join(root, name), content)
			}

			targets, err := detect.Detect(root)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			podmanTargets := make([]detect.Target, 0, len(targets))
			for _, target := range targets {
				if target.Kind == detect.KindPodman {
					podmanTargets = append(podmanTargets, target)
				}
			}
			if len(podmanTargets) != 1 {
				t.Fatalf("Detect() Podman targets = %d, want 1", len(podmanTargets))
			}

			target := podmanTargets[0]
			if target.Name != testCase.expectName {
				t.Errorf("Detect() name = %q, want %q", target.Name, testCase.expectName)
			}
			if target.Detail != testCase.expectTag(root) {
				t.Errorf("Detect() tag = %q, want %q", target.Detail, testCase.expectTag(root))
			}

			expectArgs := []string{"build", "--format", "docker"}
			if testCase.expectSecret != "" {
				expectArgs = append(expectArgs, "--secret="+testCase.expectSecret)
			}
			expectArgs = append(expectArgs, "-f", testCase.expectFile, "-t", testCase.expectTag(root), ".")
			if !reflect.DeepEqual(target.Args, expectArgs) {
				t.Errorf("Detect() args = %v, want %v", target.Args, expectArgs)
			}
		})
	}
}
