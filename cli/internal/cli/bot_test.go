package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// testBotRepo is one const so the repeated literal does not trip goconst.
const testBotRepo = "service-authentication"

func TestValidateBotCommentArgs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, org, repo, number string
		wantErr                 bool
	}{
		{"Success", orgAnovel, testBotRepo, "549", false},
		{"Success/AnovelKit", orgAnovelKit, "golib", "1", false},
		{"Error/UnknownOrg", "a-other", testBotRepo, "549", true},
		{"Error/EmptyRepo", orgAnovel, "", "549", true},
		{"Error/RepoCarriesOrgPrefix", orgAnovel, orgAnovel + "/" + testBotRepo, "549", true},
		{"Error/NonNumericNumber", orgAnovel, testBotRepo, "abc", true},
		{"Error/ZeroNumber", orgAnovel, testBotRepo, "0", true},
		{"Error/NegativeNumber", orgAnovel, testBotRepo, "-3", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := validateBotCommentArgs(tc.org, tc.repo, tc.number); (err != nil) != tc.wantErr {
				t.Fatalf("validateBotCommentArgs(%q, %q, %q) = %v, wantErr %v", tc.org, tc.repo, tc.number, err, tc.wantErr)
			}
		})
	}
}

func TestValidateBotOrgRepo(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, org, repo string
		wantErr         bool
	}{
		{"Success", orgAnovel, testBotRepo, false},
		{"Success/AnovelKit", orgAnovelKit, "nodelib", false},
		{"Error/UnknownOrg", "a-other", testBotRepo, true},
		{"Error/EmptyRepo", orgAnovel, "", true},
		{"Error/RepoCarriesOrgPrefix", orgAnovel, orgAnovel + "/" + testBotRepo, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := validateBotOrgRepo(tc.org, tc.repo); (err != nil) != tc.wantErr {
				t.Fatalf("validateBotOrgRepo(%q, %q) = %v, wantErr %v", tc.org, tc.repo, err, tc.wantErr)
			}
		})
	}
}

func TestReadBotBatch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, input string
		wantLen     int
		wantErr     bool
	}{
		{name: "Success", input: `[{"number":1,"body":"hi"}]`, wantLen: 1},
		{name: "Success/ReplyTo", input: `[{"number":1,"body":"hi","reply_to":99}]`, wantLen: 1},
		{name: "Success/Multiple", input: `[{"number":1,"body":"a"},{"number":2,"body":"b"}]`, wantLen: 2},
		{name: "Error/NotAnArray", input: `{"number":1,"body":"hi"}`, wantErr: true},
		{name: "Error/EmptyArray", input: `[]`, wantErr: true},
		{name: "Error/MalformedJSON", input: `[`, wantErr: true},
		{name: "Error/ZeroNumber", input: `[{"number":0,"body":"hi"}]`, wantErr: true},
		{name: "Error/NegativeNumber", input: `[{"number":-1,"body":"hi"}]`, wantErr: true},
		{name: "Error/BlankBody", input: `[{"number":1,"body":"   "}]`, wantErr: true},
		{name: "Error/NegativeReplyTo", input: `[{"number":1,"body":"hi","reply_to":-2}]`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items, err := readBotBatch("-", strings.NewReader(tc.input))
			if (err != nil) != tc.wantErr || len(items) != tc.wantLen {
				t.Fatalf("readBotBatch(%q) = (%d items, %v), want (%d, error %v)", tc.input, len(items), err, tc.wantLen, tc.wantErr)
			}
		})
	}
}

func TestChunkBotComments(t *testing.T) {
	t.Parallel()

	item := func(number int64, bodyLen int) botBatchItem {
		return botBatchItem{Number: number, Body: strings.Repeat("x", bodyLen)}
	}

	t.Run("Success/SingleChunkUnderTheCap", func(t *testing.T) {
		t.Parallel()
		chunks, err := chunkBotComments([]botBatchItem{item(1, 10), item(2, 10), item(3, 10)}, botMaxCommentsBytes)
		if err != nil || len(chunks) != 1 || len(chunks[0]) != 3 {
			t.Fatalf("chunkBotComments = (%d chunks, %v), want one chunk of 3", len(chunks), err)
		}
	})

	t.Run("Success/SplitsOverTheCapPreservingOrder", func(t *testing.T) {
		t.Parallel()
		const limit = 70
		chunks, err := chunkBotComments([]botBatchItem{item(1, 20), item(2, 20), item(3, 20), item(4, 20)}, limit)
		if err != nil || len(chunks) < 2 {
			t.Fatalf("chunkBotComments = (%d chunks, %v), want several", len(chunks), err)
		}
		var flat []int64
		for _, chunk := range chunks {
			if marshaled, _ := json.Marshal(chunk); len(marshaled) > limit {
				t.Fatalf("chunk of %d bytes exceeds the %d-byte limit", len(marshaled), limit)
			}
			for _, it := range chunk {
				flat = append(flat, it.Number)
			}
		}
		if !slices.Equal(flat, []int64{1, 2, 3, 4}) {
			t.Fatalf("items across chunks = %v, want every item in order", flat)
		}
	})

	t.Run("Error/OversizedComment", func(t *testing.T) {
		t.Parallel()
		if _, err := chunkBotComments([]botBatchItem{item(1, 200)}, 50); err == nil {
			t.Fatal("expected an error for a comment larger than the cap, got nil")
		}
	})
}
