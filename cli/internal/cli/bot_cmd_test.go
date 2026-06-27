package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// testBotRepo is one const so the repeated literal does not trip goconst.
const testBotRepo = "service-authentication"

func TestValidateBotCommentArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		org    string
		repo   string
		number string

		expectErr bool
	}{
		{
			name:   "valid a-novel target",
			org:    orgAnovel,
			repo:   testBotRepo,
			number: "549",
		},
		{
			name:   "valid a-novel-kit target",
			org:    orgAnovelKit,
			repo:   "golib",
			number: "1",
		},
		{
			name:      "unknown org",
			org:       "a-other",
			repo:      testBotRepo,
			number:    "549",
			expectErr: true,
		},
		{
			name:      "empty repo",
			org:       orgAnovel,
			repo:      "",
			number:    "549",
			expectErr: true,
		},
		{
			name:      "repo carries org prefix",
			org:       orgAnovel,
			repo:      orgAnovel + "/" + testBotRepo,
			number:    "549",
			expectErr: true,
		},
		{
			name:      "non-numeric number",
			org:       orgAnovel,
			repo:      testBotRepo,
			number:    "abc",
			expectErr: true,
		},
		{
			name:      "zero number",
			org:       orgAnovel,
			repo:      testBotRepo,
			number:    "0",
			expectErr: true,
		},
		{
			name:      "negative number",
			org:       orgAnovel,
			repo:      testBotRepo,
			number:    "-3",
			expectErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validateBotCommentArgs(testCase.org, testCase.repo, testCase.number)
			if testCase.expectErr && err == nil {
				t.Fatalf("expected an error for org=%q repo=%q number=%q, got nil",
					testCase.org, testCase.repo, testCase.number)
			}
			if !testCase.expectErr && err != nil {
				t.Fatalf("expected no error for org=%q repo=%q number=%q, got: %v",
					testCase.org, testCase.repo, testCase.number, err)
			}
		})
	}
}

func TestValidateBotOrgRepo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		org  string
		repo string

		expectErr bool
	}{
		{name: "valid a-novel", org: orgAnovel, repo: testBotRepo},
		{name: "valid a-novel-kit", org: orgAnovelKit, repo: "nodelib"},
		{name: "unknown org", org: "a-other", repo: testBotRepo, expectErr: true},
		{name: "empty repo", org: orgAnovel, repo: "", expectErr: true},
		{name: "repo carries org prefix", org: orgAnovel, repo: orgAnovel + "/" + testBotRepo, expectErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validateBotOrgRepo(testCase.org, testCase.repo)
			if testCase.expectErr && err == nil {
				t.Fatalf("expected an error for org=%q repo=%q, got nil", testCase.org, testCase.repo)
			}
			if !testCase.expectErr && err != nil {
				t.Fatalf("expected no error for org=%q repo=%q, got: %v", testCase.org, testCase.repo, err)
			}
		})
	}
}

func TestReadBotBatch(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		input   string
		wantLen int

		expectErr bool
	}{
		{name: "single comment", input: `[{"number":1,"body":"hi"}]`, wantLen: 1},
		{name: "comment with reply_to", input: `[{"number":1,"body":"hi","reply_to":99}]`, wantLen: 1},
		{name: "multiple comments", input: `[{"number":1,"body":"a"},{"number":2,"body":"b"}]`, wantLen: 2},
		{name: "not an array", input: `{"number":1,"body":"hi"}`, expectErr: true},
		{name: "empty array", input: `[]`, expectErr: true},
		{name: "malformed json", input: `[`, expectErr: true},
		{name: "zero number", input: `[{"number":0,"body":"hi"}]`, expectErr: true},
		{name: "negative number", input: `[{"number":-1,"body":"hi"}]`, expectErr: true},
		{name: "blank body", input: `[{"number":1,"body":"   "}]`, expectErr: true},
		{name: "negative reply_to", input: `[{"number":1,"body":"hi","reply_to":-2}]`, expectErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			items, err := readBotBatch("-", strings.NewReader(testCase.input))
			if testCase.expectErr {
				if err == nil {
					t.Fatalf("expected an error for input %q, got nil", testCase.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error for input %q, got: %v", testCase.input, err)
			}
			if len(items) != testCase.wantLen {
				t.Fatalf("expected %d items for input %q, got %d", testCase.wantLen, testCase.input, len(items))
			}
		})
	}
}

func TestChunkBotComments(t *testing.T) {
	t.Parallel()

	item := func(number int64, bodyLen int) botBatchItem {
		return botBatchItem{Number: number, Body: strings.Repeat("x", bodyLen)}
	}

	t.Run("packs into a single chunk under the cap", func(t *testing.T) {
		t.Parallel()

		items := []botBatchItem{item(1, 10), item(2, 10), item(3, 10)}
		chunks, err := chunkBotComments(items, botMaxCommentsBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chunks) != 1 {
			t.Fatalf("expected 1 chunk, got %d", len(chunks))
		}
		if len(chunks[0]) != 3 {
			t.Fatalf("expected 3 items in the chunk, got %d", len(chunks[0]))
		}
	})

	t.Run("splits across chunks over the cap, preserving order and size", func(t *testing.T) {
		t.Parallel()

		const limit = 70
		items := []botBatchItem{item(1, 20), item(2, 20), item(3, 20), item(4, 20)}
		chunks, err := chunkBotComments(items, limit)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(chunks) < 2 {
			t.Fatalf("expected the batch to split into multiple chunks, got %d", len(chunks))
		}

		var flat []int64
		for _, chunk := range chunks {
			marshaled, err := json.Marshal(chunk)
			if err != nil {
				t.Fatalf("marshal chunk: %v", err)
			}
			if len(marshaled) > limit {
				t.Fatalf("chunk of %d bytes exceeds the %d-byte limit", len(marshaled), limit)
			}
			for _, it := range chunk {
				flat = append(flat, it.Number)
			}
		}
		if len(flat) != len(items) {
			t.Fatalf("expected %d items across all chunks, got %d", len(items), len(flat))
		}
		for i, number := range flat {
			if number != int64(i+1) {
				t.Fatalf("order not preserved across chunks: %v", flat)
			}
		}
	})

	t.Run("a single oversized comment is a hard error", func(t *testing.T) {
		t.Parallel()

		if _, err := chunkBotComments([]botBatchItem{item(1, 200)}, 50); err == nil {
			t.Fatal("expected an error for a comment larger than the cap, got nil")
		}
	})
}
