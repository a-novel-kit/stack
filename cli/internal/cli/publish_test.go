package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestStampFile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeFixture(t, dir, "doc.md", tc.content)
			re, err := stampPattern(tc.prefix)
			count := 0
			if err == nil {
				count, err = stampFile(filepath.Join(dir, "doc.md"), re, tc.version)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got none")
				}
				return
			}
			if err != nil || count != tc.wantCount {
				t.Fatalf("stampFile = (%d, %v), want %d", count, err, tc.wantCount)
			}
			if got := readFixture(t, dir, "doc.md"); got != tc.wantContent {
				t.Errorf("content = %q, want %q", got, tc.wantContent)
			}
		})
	}
}

func TestReadPackageVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		packageJSON string // "" leaves the file absent
		want        string
		wantErr     bool
	}{
		{name: "Success", packageJSON: `{"name": "stack", "version": "1.4.2"}`, want: "1.4.2"},
		{name: "Error/MissingVersion", packageJSON: `{"name": "stack"}`, wantErr: true},
		{name: "Error/InvalidJSON", packageJSON: `{`, wantErr: true},
		{name: "Error/MissingFile", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if tc.packageJSON != "" {
				writeFixture(t, root, "package.json", tc.packageJSON)
			}
			if got, err := readPackageVersion(root); (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("readPackageVersion = (%q, %v), want (%q, error %v)", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestResolveStampTargets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, rel := range []string{"a/x/action.yaml", "a/y/action.yaml", "b/z/action.yaml", "top.yaml", "weird[1].yaml"} {
		writeFixture(t, dir, rel, "x")
	}
	in := func(rels ...string) []string {
		out := make([]string, len(rels))
		for i, rel := range rels {
			out[i] = filepath.Join(dir, rel)
		}
		return out
	}
	actions := in("a/x/action.yaml", "a/y/action.yaml", "b/z/action.yaml")

	for _, tc := range []struct {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveStampTargets(tc.patterns)
			slices.Sort(got)
			if (err != nil) != (tc.want == nil) || !slices.Equal(got, tc.want) {
				t.Fatalf("resolveStampTargets(%v) = (%v, %v), want %v", tc.patterns, got, err, tc.want)
			}
		})
	}
}

func TestStampTargets(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFixture(t, dir, "README.md", "go get x@v1.0.0\n")
		total, files, err := stampTargets([]string{filepath.Join(dir, "README.md")}, "x@", "1.1.0")
		if err != nil || total != 1 || files != 1 {
			t.Fatalf("stampTargets = (%d, %d, %v), want (1, 1, nil)", total, files, err)
		}
		if got := readFixture(t, dir, "README.md"); got != "go get x@v1.1.0\n" {
			t.Errorf("content = %q", got)
		}
	})

	t.Run("Success/OneMatchAmongSeveralFiles", func(t *testing.T) {
		t.Parallel()
		// The aggregate must be non-zero; a file that legitimately holds no
		// reference is not itself a failure.
		dir := t.TempDir()
		writeFixture(t, dir, "has.md", "x@v1.0.0\n")
		writeFixture(t, dir, "none.md", "unrelated\n")
		total, files, err := stampTargets([]string{filepath.Join(dir, "has.md"), filepath.Join(dir, "none.md")}, "x@", "1.1.0")
		if err != nil || total != 1 || files != 2 {
			t.Fatalf("stampTargets = (%d, %d, %v), want (1, 2, nil)", total, files, err)
		}
	})

	t.Run("Error/ZeroMatches", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFixture(t, dir, "a.md", "nothing here\n")
		writeFixture(t, dir, "b.md", "nor here\n")
		_, _, err := stampTargets([]string{filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")}, "version: ", "1.1.0")
		// The message names the pattern and the files it swept, so a broken
		// prepublish:doc script points at its own line.
		if err == nil || !strings.Contains(err.Error(), "version: ") || !strings.Contains(err.Error(), "a.md") {
			t.Errorf("error = %v, want the pattern and the files named", err)
		}
	})

	t.Run("Error/NoFilesMatched", func(t *testing.T) {
		t.Parallel()
		if _, _, err := stampTargets([]string{filepath.Join(t.TempDir(), "absent-*.md")}, "x@", "1.1.0"); err == nil {
			t.Fatal("stampTargets: got nil, want the no-files-matched error")
		}
	})
}
