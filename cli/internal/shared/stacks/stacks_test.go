package stacks

import "testing"

// Not parallel: every case calls t.Setenv, which forbids it.
func TestDefaultPath(t *testing.T) {
	cases := []struct {
		name    string
		stacks  string
		want    string
		wantErr bool
	}{
		{name: "FirstEntryWins", stacks: "main:/srv/main,fork:/srv/fork", want: "/srv/main"},
		{name: "UnsetFallsBackToImplicitDefault", stacks: "", want: "/home/tester/git-projects/a-novel"},
		{name: "MalformedEntry", stacks: "no-path-here", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(EnvVar, c.stacks)
			t.Setenv("HOME", "/home/tester")
			got, err := DefaultPath()
			if (err != nil) != c.wantErr {
				t.Fatalf("DefaultPath() error = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("DefaultPath() = %q, want %q", got, c.want)
			}
		})
	}
}
