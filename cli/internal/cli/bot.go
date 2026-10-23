package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// botRunPoll and botRunFinishTimeout pace and bound the wait for a dispatched
// run's conclusion once gh run watch has stopped watching it.
var (
	botRunPoll          = 3 * time.Second
	botRunFinishTimeout = 10 * time.Minute
	// botRunLookupPoll paces the wait for a dispatched run to register.
	botRunLookupPoll = 2 * time.Second
)

const (
	// botWorkflowFile is the workflow_dispatch file, by basename.
	botWorkflowFile = "bot-comment.yaml"
	// botRunLookupTimeout bounds the wait for a dispatched run to register.
	// Dispatch returns no run ID, so the run list is polled until the
	// nonce shows up in a run name.
	botRunLookupTimeout = 60 * time.Second
	// botMaxCommentsBytes caps the JSON size of one `comments` dispatch.
	// workflow_dispatch limits all inputs together to about 64KB, and the cap
	// leaves room for the other inputs. A larger batch is split across
	// dispatches.
	botMaxCommentsBytes = 50000
)

// botBatchItem is one comment in a `--batch` payload: a target number, the
// markdown body, and an optional inline review-comment ID to reply to. The
// JSON tags match an element of the dispatcher's `comments` array, so the
// parsed items re-marshal straight into the dispatch input.
type botBatchItem struct {
	Number  int64  `json:"number"`
	Body    string `json:"body"`
	ReplyTo int64  `json:"reply_to,omitempty"`
}

// botCommentOpts holds the flags of `core bot-comment`.
type botCommentOpts struct {
	body      string
	replyTo   string
	batchPath string
}

// newCoreBotCommentCmd posts PR, issue and review comments as the org's GitHub
// App bot. It dispatches the org's bot-comment workflow with the operator's own
// gh token and waits for each run to finish.
//
// The workflow holds the App key, so the key never reaches a dev machine. An
// operator needs only actions:write on the dispatcher repository, revoked per
// user, and the workflow can only post comments, so a compromised trigger
// cannot push, merge or author a pull request.
func newCoreBotCommentCmd() *cobra.Command {
	var o botCommentOpts
	cmd := &cobra.Command{
		Use:   "bot-comment <org> <repo> [<number>]",
		Short: "Post pull request, issue and review comments as the organization's bot",
		Long: `Post comments as the organization's bot, through the bot-comment workflow of its
dispatcher repository, which you dispatch with your own gh token. The bot's key
stays in CI, and the workflow only posts comments.

<number> is a pull request or an issue. --reply-to answers an inline review
thread, which only a pull request has. --batch posts a JSON array of
{number, body, reply_to?} from a file, or from stdin with '-', in as many runs
as its size needs.

The command waits for each run, and fails when a run fails, printing its logs.
It needs actions:write on the dispatcher repository.`,
		Example: `  a-novel core bot-comment a-novel service-authentication 549 --body "Automated review note."
  a-novel core bot-comment a-novel service-authentication 549 --reply-to 2103847559 --body "Fixed in abc1234."
  echo '[{"number":549,"body":"Fixed."}]' | a-novel core bot-comment a-novel service-authentication --batch -`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return postBotComments(cmd, args, o)
		},
	}
	cmd.Flags().StringVar(&o.body, "body", "", "markdown comment body (required in single mode)")
	cmd.Flags().StringVar(&o.replyTo, "reply-to", "", "inline review-comment id to reply to (omit for a top-level comment)")
	cmd.Flags().StringVar(&o.batchPath, "batch", "", "path to a JSON array of {number, body, reply_to?} (or - for stdin); posts all in one run")
	return cmd
}

// postBotComments checks the arguments of the mode the flags select, one
// comment or a --batch file, before any dispatch is spent, then posts.
func postBotComments(cmd *cobra.Command, args []string, o botCommentOpts) error {
	org, repo := args[0], args[1]
	if err := validateBotOrgRepo(org, repo); err != nil {
		return err
	}

	if o.batchPath != "" {
		if len(args) != 2 {
			return errors.New("bot-comment: with --batch, pass <org> <repo> only — each comment carries its own number")
		}
		if o.body != "" || o.replyTo != "" {
			return errors.New("bot-comment: --batch cannot be combined with --body or --reply-to")
		}
		return runBotCommentBatch(cmd, org, repo, o.batchPath)
	}

	if len(args) != 3 {
		return errors.New("bot-comment: <number> is required (or use --batch to post many comments)")
	}
	number := args[2]
	if !positiveInt(number) {
		return fmt.Errorf("bot-comment: number %q must be a positive integer", number)
	}
	if strings.TrimSpace(o.body) == "" {
		return errors.New("bot-comment: --body is required and must be non-empty")
	}
	if o.replyTo != "" && !positiveInt(o.replyTo) {
		return fmt.Errorf("bot-comment: --reply-to %q must be a positive integer (a REST review-comment id)", o.replyTo)
	}
	return runBotComment(cmd, org, repo, number, o.body, o.replyTo)
}

// validateBotOrgRepo checks the org is a configured dispatcher target and
// the repo is a bare name. Shared by the single and batch paths.
func validateBotOrgRepo(org, repo string) error {
	if _, ok := repocfg.BotDispatchRepos[org]; !ok {
		return fmt.Errorf("bot-comment: org %q is not configured (known: %s, %s)", org, orgAnovel, orgAnovelKit)
	}
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("bot-comment: repo is required (name only, no %q prefix)", org+"/")
	}
	if strings.ContainsRune(repo, '/') {
		return fmt.Errorf("bot-comment: repo %q must be the bare repo name, without the org prefix", repo)
	}
	return nil
}

// positiveInt reports whether s is a decimal integer above zero.
func positiveInt(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n > 0
}

// runBotComment dispatches a single comment and watches its run.
func runBotComment(cmd *cobra.Command, org, repo, number, body, replyTo string) error {
	formArgs := []string{"-f", "repo=" + repo, "-f", "number=" + number, "-f", "body=" + body}
	if replyTo != "" {
		formArgs = append(formArgs, "-f", "reply_to="+replyTo)
	}
	label := fmt.Sprintf("for %s/%s#%s", org, repo, number)
	return dispatchBotComment(cmd, repocfg.BotDispatchRepos[org], label, formArgs...)
}

// runBotCommentBatch reads a JSON array of comments, splits it into payloads
// that fit one dispatch, and posts each as a single run.
func runBotCommentBatch(cmd *cobra.Command, org, repo, batchPath string) error {
	items, err := readBotBatch(batchPath, cmd.InOrStdin())
	if err != nil {
		return fmt.Errorf("bot-comment: %w", err)
	}
	chunks, err := chunkBotComments(items, botMaxCommentsBytes)
	if err != nil {
		return fmt.Errorf("bot-comment: %w", err)
	}
	dispatchRepo := repocfg.BotDispatchRepos[org]
	for i, chunk := range chunks {
		payload, err := json.Marshal(chunk)
		if err != nil {
			return fmt.Errorf("bot-comment: marshal chunk %d: %w", i+1, err)
		}
		label := fmt.Sprintf("for %s/%s (%d comments)", org, repo, len(chunk))
		if len(chunks) > 1 {
			label = fmt.Sprintf("for %s/%s (%d comments, chunk %d/%d)", org, repo, len(chunk), i+1, len(chunks))
		}
		if err := dispatchBotComment(cmd, dispatchRepo, label, "-f", "repo="+repo, "-f", "comments="+string(payload)); err != nil {
			return err
		}
	}
	return nil
}

// readBotBatch reads the --batch input, a file path or "-" for stdin, parses
// it into comment items, and validates the shape the dispatcher cannot
// report on cheaply: a non-empty array whose entries carry a positive number
// and a non-empty body. Per-comment posting errors stay with the GitHub API at
// run time.
func readBotBatch(path string, stdin io.Reader) ([]botBatchItem, error) {
	var (
		raw []byte
		err error
	)
	if path == "-" {
		raw, err = io.ReadAll(stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read --batch input: %w", err)
	}
	var items []botBatchItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("--batch must be a JSON array of {number, body, reply_to?}: %w", err)
	}
	if len(items) == 0 {
		return nil, errors.New("--batch payload is empty")
	}
	for i, item := range items {
		if item.Number <= 0 {
			return nil, fmt.Errorf("--batch item %d: number must be a positive integer", i)
		}
		if strings.TrimSpace(item.Body) == "" {
			return nil, fmt.Errorf("--batch item %d: body is required", i)
		}
		if item.ReplyTo < 0 {
			return nil, fmt.Errorf("--batch item %d: reply_to must be a positive integer", i)
		}
	}
	return items, nil
}

// chunkBotComments greedily packs comments into chunks whose marshaled JSON
// stays within maxBytes, so each chunk fits a single workflow_dispatch. A
// lone comment larger than maxBytes is an error, since it cannot be split.
func chunkBotComments(items []botBatchItem, maxBytes int) ([][]botBatchItem, error) {
	// A chunk's JSON is "[" plus each item followed by "," or the closing "]".
	const open = 1
	var (
		chunks  [][]botBatchItem
		current []botBatchItem
	)
	size := open
	for _, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("marshal comment number %d: %w", item.Number, err)
		}
		cost := len(raw) + 1
		if open+cost > maxBytes {
			return nil, fmt.Errorf("comment number %d is %d bytes, over the %d-byte dispatch limit", item.Number, open+cost, maxBytes)
		}
		if size+cost > maxBytes {
			chunks = append(chunks, current)
			current, size = nil, open
		}
		current = append(current, item)
		size += cost
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks, nil
}

// dispatchBotComment triggers the per-org bot-comment workflow with the
// given form inputs (-f key=value, beyond the shared nonce), waits for the
// resulting run by nonce, and watches it to completion.
func dispatchBotComment(cmd *cobra.Command, dispatchRepo, label string, formArgs ...string) error {
	nonce, err := newNonce()
	if err != nil {
		return fmt.Errorf("bot-comment: generate nonce: %w", err)
	}

	dispatchArgs := []string{"workflow", commandRun, botWorkflowFile, "--repo", dispatchRepo, "-f", "nonce=" + nonce}
	if _, err := gh(append(dispatchArgs, formArgs...)...); err != nil {
		return fmt.Errorf("bot-comment: dispatch to %s failed: %w", dispatchRepo, err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "dispatched bot-comment %s; waiting for the run...\n", label)

	runID, err := waitForDispatchedRun(dispatchRepo, nonce)
	if err != nil {
		return fmt.Errorf("bot-comment: %w", err)
	}

	// gh run watch streams progress and, with --exit-status, exits non-zero
	// when the run concluded in failure. It is wired straight to this
	// command's stdio so the operator sees live progress.
	watch := exec.Command("gh", commandRun, "watch", strconv.FormatInt(runID, 10),
		"--repo", dispatchRepo, "--exit-status", "--interval", "3")
	watch.Stdout = cmd.OutOrStdout()
	watch.Stderr = cmd.ErrOrStderr()
	if err := watch.Run(); err != nil {
		// A failed poll stops gh run watch while the run goes on to post, so the
		// run's own conclusion decides whether the comments landed.
		if succeeded, viewErr := runSucceeded(dispatchRepo, runID); viewErr == nil && succeeded {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "gh run watch stopped (%v), but run %d succeeded\n", err, runID)
			return nil
		}
		logs, _ := gh(commandRun, "view", strconv.FormatInt(runID, 10),
			"--repo", dispatchRepo, "--log-failed")
		return fmt.Errorf("bot-comment: workflow run %d failed: %w\n%s", runID, err, logs)
	}
	return nil
}

// runSucceeded waits for a run to complete and reports whether it concluded in
// success. A failed read counts as not yet complete until botRunFinishTimeout.
func runSucceeded(dispatchRepo string, runID int64) (bool, error) {
	deadline := time.Now().Add(botRunFinishTimeout)
	for {
		out, err := gh(commandRun, "view", strconv.FormatInt(runID, 10), "--repo", dispatchRepo,
			"--json", "status,conclusion", "--jq", `.status + " " + .conclusion`)
		if status, conclusion, _ := strings.Cut(strings.TrimSpace(out), " "); err == nil && status == "completed" {
			return conclusion == "success", nil
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("run %d did not complete within %s", runID, botRunFinishTimeout)
		}
		time.Sleep(botRunPoll)
	}
}

// waitForDispatchedRun polls the run list until a run whose name carries the
// nonce appears, and returns its database ID. workflow_dispatch returns no run
// ID and the run takes a moment to register, hence the bounded poll.
func waitForDispatchedRun(dispatchRepo, nonce string) (int64, error) {
	deadline := time.Now().Add(botRunLookupTimeout)
	for {
		out, err := gh(commandRun, "list",
			"--repo", dispatchRepo,
			"--workflow", botWorkflowFile,
			"--event", "workflow_dispatch",
			"--limit", "30",
			"--json", "databaseId,displayTitle")
		if id := runWithNonce(out, nonce); err == nil && id != 0 {
			return id, nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("dispatched run did not appear within %s "+
				"(check 'gh run list --repo %s')", botRunLookupTimeout, dispatchRepo)
		}
		time.Sleep(botRunLookupPoll)
	}
}

// runWithNonce returns the id of the run in a `gh run list` JSON listing whose
// name carries nonce, or 0 when none does.
func runWithNonce(listing, nonce string) int64 {
	var runs []struct {
		DatabaseID   int64  `json:"databaseId"`
		DisplayTitle string `json:"displayTitle"`
	}
	_ = json.Unmarshal([]byte(listing), &runs) // An unreadable listing names no run.
	for _, r := range runs {
		if strings.Contains(r.DisplayTitle, "("+nonce+")") {
			return r.DatabaseID
		}
	}
	return 0
}

// newNonce returns a short random hex token used to correlate a dispatch
// with its run (via the run-name) and to dedupe the comment.
func newNonce() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
