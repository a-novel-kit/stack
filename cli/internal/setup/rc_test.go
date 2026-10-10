package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpsertRCBlock(t *testing.T) {
	t.Parallel()

	const block = "a-novel core start\n"
	managed := beginMarker + "\n" + block + endMarker + "\n"

	testCases := []struct {
		name string

		existing *string // nil leaves the rc file absent

		want        string
		wantChanged bool
		wantErr     bool
	}{
		{name: "Success/MissingFile", want: managed, wantChanged: true},
		{name: "Success/EmptyFile", existing: new(""), want: managed, wantChanged: true},
		{
			name:     "Success/AppendAfterTrailingNewline",
			existing: new("export A=1\n"), want: "export A=1\n\n" + managed, wantChanged: true,
		},
		{
			name:     "Success/AppendWithoutTrailingNewline",
			existing: new("export A=1"), want: "export A=1\n\n" + managed, wantChanged: true,
		},
		{
			name:     "Success/ReplaceStaleBlock",
			existing: new("x\n" + beginMarker + "\nold\n" + endMarker + "\ny\n"), want: "x\n" + managed + "y\n",
			wantChanged: true,
		},
		{name: "Success/UpToDate", existing: new("x\n" + managed), want: "x\n" + managed},
		{
			name:     "Error/Malformed",
			existing: new("x\n" + beginMarker + "\n"), want: "x\n" + beginMarker + "\n", wantErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rcPath := filepath.Join(t.TempDir(), ".zshrc")
			if testCase.existing != nil {
				if err := os.WriteFile(rcPath, []byte(*testCase.existing), 0o600); err != nil {
					t.Fatalf("seed rc: %v", err)
				}
			}

			_, changed, err := upsertRCBlock(rcPath, block)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("upsertRCBlock error = %v, wantErr %v", err, testCase.wantErr)
			}
			if changed != testCase.wantChanged {
				t.Errorf("upsertRCBlock changed = %v, want %v", changed, testCase.wantChanged)
			}
			got, err := os.ReadFile(rcPath)
			if err != nil {
				t.Fatalf("read rc: %v", err)
			}
			if string(got) != testCase.want {
				t.Errorf("rc content = %q, want %q", got, testCase.want)
			}
		})
	}
}
