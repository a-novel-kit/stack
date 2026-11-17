package detect_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

func TestBuilds(t *testing.T) {
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
			name: "Success/PlatformRootDockerfile",
			files: map[string]string{
				"Dockerfile": "FROM scratch\nRUN --mount=type=secret,id=npm_token,required=true echo ready\n",
			},
			expectName:   "Dockerfile",
			expectTag:    func(root string) string { return "localhost/" + filepath.Base(root) + ":local" },
			expectFile:   "Dockerfile",
			expectSecret: "id=npm_token,env=NPM_TOKEN",
		},
		{
			name: "Success/NamedServiceDockerfile",
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
			fsutiltest.Write(t, root, testCase.files)

			targets, err := detect.Builds(t.Context(), root)
			require.NoError(t, err)
			podmanTargets := make([]detect.Target, 0, len(targets))
			for _, target := range targets {
				if target.Kind == detect.KindPodman {
					podmanTargets = append(podmanTargets, target)
				}
			}
			require.Len(t, podmanTargets, 1)

			target := podmanTargets[0]
			require.Equal(t, testCase.expectName, target.Name)
			require.Equal(t, testCase.expectTag(root), target.Detail)

			expectArgs := []string{"build", "--format", "docker"}
			if testCase.expectSecret != "" {
				expectArgs = append(expectArgs, "--secret="+testCase.expectSecret)
			}
			expectArgs = append(expectArgs, "-f", testCase.expectFile, "-t", testCase.expectTag(root), ".")
			require.Equal(t, expectArgs, target.Args)
		})
	}
}
