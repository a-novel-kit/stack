package cli

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

func TestStampFile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name, content, prefix, version string
		wantCount                      int
		wantContent                    string
		wantErr                        bool
	}{
		{
			name: "Success/OpenAPIVersionLine", content: "info:\n  version: v1.2.3\n", prefix: "version: ", version: "2.0.0",
			wantCount: 1, wantContent: "info:\n  version: v2.0.0\n",
		},
		{
			name:    "Success/ModulePathWithRegexPrefix",
			content: "go get github.com/a-novel/service-json-keys/v2@v2.1.3\n", prefix: "a-novel/service-json-keys/[^/]+", version: "2.2.0",
			wantCount: 1, wantContent: "go get github.com/a-novel/service-json-keys/v2@v2.2.0\n",
		},
		{
			name: "Success/EveryOccurrence", content: "version: v1.0.0\nversion: v1.0.0\n", prefix: "version: ", version: "1.1.0",
			wantCount: 2, wantContent: "version: v1.1.0\nversion: v1.1.0\n",
		},
		{
			name: "Success/NoMatchLeavesFileUntouched", content: "nothing to see here\n", prefix: "version: ", version: "9.9.9",
			wantContent: "nothing to see here\n",
		},
		{name: "Error/InvalidPrefixRegex", content: "version: v1.0.0\n", prefix: "version: (", version: "1.1.0", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			fsutiltest.Write(t, dir, map[string]string{"doc.md": testCase.content})
			re, err := stampPattern(testCase.prefix)
			count := 0
			if err == nil {
				count, err = stampFile(filepath.Join(dir, "doc.md"), re, testCase.version)
			}
			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.wantCount, count)
			require.Equal(t, testCase.wantContent, readFixture(t, dir, "doc.md"))
		})
	}
}

func TestReadPackageVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		packageJSON string // "" leaves the file absent
		want        string
		wantErr     bool
	}{
		{name: "Success", packageJSON: `{"name": "stack", "version": "1.4.2"}`, want: "1.4.2"},
		{name: "Error/MissingVersion", packageJSON: `{"name": "stack"}`, wantErr: true},
		{name: "Error/InvalidJSON", packageJSON: `{`, wantErr: true},
		{name: "Error/MissingFile", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if testCase.packageJSON != "" {
				fsutiltest.Write(t, root, map[string]string{"package.json": testCase.packageJSON})
			}

			got, err := readPackageVersion(root)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestResolveStampTargets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fsutiltest.Write(t, dir, map[string]string{
		"a/x/action.yaml": "x",
		"a/y/action.yaml": "x",
		"b/z/action.yaml": "x",
		"top.yaml":        "x",
		"weird[1].yaml":   "x",
	})
	in := func(rels ...string) []string {
		out := make([]string, len(rels))
		for i, rel := range rels {
			out[i] = filepath.Join(dir, rel)
		}
		return out
	}
	actions := in("a/x/action.yaml", "a/y/action.yaml", "b/z/action.yaml")

	testCases := []struct {
		name     string
		patterns []string
		want     []string // nil: the patterns must be refused
	}{
		// A glob across two levels matches the three action files, not top.yaml.
		{name: "Success/Glob", patterns: in("*/*/action.yaml"), want: actions},
		{name: "Success/Literal", patterns: in("top.yaml"), want: in("top.yaml")},
		// Glob metacharacters in a literal filename are taken literally.
		{name: "Success/LiteralWithMetachars", patterns: in("weird[1].yaml"), want: in("weird[1].yaml")},
		{name: "Success/OverlappingPatternsDedupe", patterns: in("a/*/action.yaml", "*/*/action.yaml"), want: actions},
		// Nothing matched is an error: it catches typos.
		{name: "Error/NothingMatches", patterns: in("nope/*.yaml")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveStampTargets(testCase.patterns)
			if testCase.want == nil {
				require.Error(t, err)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			slices.Sort(got)
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestStampTargets(t *testing.T) {
	t.Parallel()

	t.Run("Success/OneMatchAmongSeveralFiles", func(t *testing.T) {
		t.Parallel()

		// The aggregate must be non-zero; a file that legitimately holds no
		// reference is not itself a failure.
		dir := t.TempDir()
		fsutiltest.Write(t, dir, map[string]string{"has.md": "x@v1.0.0\n", "none.md": "unrelated\n"})
		total, files, err := stampTargets([]string{filepath.Join(dir, "has.md"), filepath.Join(dir, "none.md")}, "x@", "1.1.0")
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Equal(t, 2, files)
	})

	t.Run("Error/ZeroMatches", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		fsutiltest.Write(t, dir, map[string]string{"a.md": "nothing here\n", "b.md": "nor here\n"})
		_, _, err := stampTargets([]string{filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")}, "version: ", "1.1.0")
		// The message names the pattern and the files it swept, so a broken
		// prepublish:doc script points at its own line.
		require.ErrorContains(t, err, "version: ")
		require.ErrorContains(t, err, "a.md")
	})
}
