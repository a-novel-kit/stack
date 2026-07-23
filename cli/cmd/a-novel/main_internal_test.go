package main

import (
	"slices"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// withCoverage now leaves a Go target's selectors alone and only adds -cover, so every
// test package runs and the exclusion happens when the mean is computed. Filtering the run
// list is what dropped a package's tests the moment its path held mocks/test/protogen.
func TestWithCoverageKeepsSelectors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   detect.Target
		want []string
	}{
		{
			name: "env-less whole module",
			in:   detect.Target{Kind: detect.KindGo, Args: []string{"test", "./..."}},
			want: []string{"test", coverFlag, "./..."},
		},
		{
			name: "env-backed keeps its -count=1 and scope",
			in:   detect.Target{Kind: detect.KindGo, Args: []string{"test", "-count=1", "./internal/..."}},
			want: []string{"test", coverFlag, "-count=1", "./internal/..."},
		},
		{
			// The item-2 catch-all carries several selectors. All must survive — the old
			// code took only the last, so the others' coverage runs vanished.
			name: "multi-selector catch-all keeps every selector",
			in:   detect.Target{Kind: detect.KindGo, Args: []string{"test", "./pkg/go", "./cmd/rest"}},
			want: []string{"test", coverFlag, "./pkg/go", "./cmd/rest"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := withCoverage([]detect.Target{c.in})
			if !slices.Equal(got[0].Args, c.want) {
				t.Errorf("args = %v, want %v", got[0].Args, c.want)
			}
		})
	}
}

func TestWithCoveragePnpm(t *testing.T) {
	t.Parallel()

	got := withCoverage([]detect.Target{{
		Kind: detect.KindPnpm,
		Args: []string{"pnpm", "run", "test"},
	}})

	if !slices.Equal(got[0].Args, []string{"pnpm", "run", "test", "--", "--coverage"}) {
		t.Errorf("pnpm args = %v", got[0].Args)
	}
}
