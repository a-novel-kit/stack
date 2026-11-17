package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/workspace"
	"github.com/a-novel-kit/stack/cli/internal/workspace/workspacetest"
)

// testBotRepo is one const so the repeated literal does not trip goconst.
const testBotRepo = "service-authentication"

// TestPositiveInt pins the check on a comment number and a --reply-to id.
func TestPositiveInt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name, input string
		want        bool
	}{
		{"Success", "549", true},
		{"Success/One", "1", true},
		{"Error/NonNumeric", "abc", false},
		{"Error/Zero", "0", false},
		{"Error/Negative", "-3", false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, positiveInt(testCase.input))
		})
	}
}

func TestValidateBotOrgRepo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name, org, repo string
		wantErr         bool
	}{
		{"Success", workspace.OrgAnovel, testBotRepo, false},
		{"Success/AnovelKit", workspace.OrgAnovelKit, "nodelib", false},
		{"Error/UnknownOrg", "a-other", testBotRepo, true},
		{"Error/EmptyRepo", workspace.OrgAnovel, "", true},
		{"Error/RepoCarriesOrgPrefix", workspace.OrgAnovel, workspace.OrgAnovel + "/" + testBotRepo, true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validateBotOrgRepo(testCase.org, testCase.repo)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestReadBotBatch(t *testing.T) {
	t.Parallel()

	testCases := []struct {
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
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			items, err := readBotBatch("-", strings.NewReader(testCase.input))
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, items, testCase.wantLen)
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
		require.NoError(t, err)
		require.Len(t, chunks, 1)
		require.Len(t, chunks[0], 3)
	})

	t.Run("Success/SplitsOverTheCapPreservingOrder", func(t *testing.T) {
		t.Parallel()

		const limit = 70
		chunks, err := chunkBotComments([]botBatchItem{item(1, 20), item(2, 20), item(3, 20), item(4, 20)}, limit)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(chunks), 2, "want several chunks")
		var flat []int64
		for _, chunk := range chunks {
			marshaled, err := json.Marshal(chunk)
			require.NoError(t, err)
			require.LessOrEqual(t, len(marshaled), limit, "chunk exceeds the byte limit")
			for _, it := range chunk {
				flat = append(flat, it.Number)
			}
		}
		require.Equal(t, []int64{1, 2, 3, 4}, flat, "want every item across chunks, in order")
	})

	t.Run("Error/OversizedComment", func(t *testing.T) {
		t.Parallel()

		_, err := chunkBotComments([]botBatchItem{item(1, 200)}, 50)
		require.Error(t, err)
	})
}

// TestRunSucceeded pins that the run's own conclusion decides, waiting while it
// is unfinished: gh run watch can stop before a run that goes on to succeed.
//
//nolint:paralleltest // swaps the package-level ghExec seam and the poll pacing.
func TestRunSucceeded(t *testing.T) {
	workspacetest.Swap(t, &botRunPoll, time.Millisecond)
	workspacetest.Swap(t, &botRunFinishTimeout, 50*time.Millisecond)

	testCases := []struct {
		name    string
		replies []string
		want    bool
		wantErr bool
	}{
		{name: "Success", replies: []string{"completed success"}, want: true},
		{name: "Success/FailedRun", replies: []string{"completed failure"}},
		{name: "Success/WaitsForAnUnfinishedRun", replies: []string{"in_progress ", "queued ", "completed success"}, want: true},
		{name: "Error/NeverCompletes", replies: []string{"in_progress "}, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			call := 0
			stubGH(t, func(string) (string, error) {
				reply := testCase.replies[min(call, len(testCase.replies)-1)]
				call++
				return reply + "\n", nil
			})

			got, err := runSucceeded("a-novel-kit/stack", 42)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.want, got)
		})
	}
}
