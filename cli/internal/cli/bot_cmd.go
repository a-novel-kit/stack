// `a-novel core bot-comment <org> <repo> <number> --body <text>` — post a
// PR/issue/review comment as the org's GitHub App bot, or many at once with
// `--batch`.
//
// The bot's signing key never lives on a dev machine. The command hands the
// comment (body plus target metadata) to the per-org `bot-comment`
// workflow_dispatch — one dispatcher per org, see botDispatchRepos — triggered
// with the operator's own gh token. The workflow holds the App key, mints a
// scoped short-lived token, and posts as <app-slug>[bot].
//
// That split is the trust boundary. A user needs only gh and actions:write on
// the dispatcher repo and never handles a .pem, so revocation is per-user. The
// workflow can only post a comment, so a compromised trigger cannot push, merge
// or author a PR. And the trigger token cannot change what the workflow does,
// which is code on the protected master branch.
//
// The command waits for each dispatched run synchronously (gh run watch
// --exit-status) and surfaces failures, so callers can react to errors.
package cli

import (
	"bytes"
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
)

const (
	// botWorkflowFile is the workflow_dispatch file, by basename.
	botWorkflowFile = "bot-comment.yaml"
	// botRunLookupTimeout bounds the wait for a dispatched run to register.
	// Dispatch returns no run ID, so the run list is polled until the
	// nonce shows up in a run name.
	botRunLookupTimeout = 60 * time.Second
	// botMaxCommentsBytes caps the JSON size of one `comments` dispatch.
	// workflow_dispatch limits the whole inputs payload to ~64KB; this
	// leaves headroom for the repo + nonce inputs and the request framing,
	// and a batch larger than this is split across dispatches.
	botMaxCommentsBytes = 50000
)

// botDispatchRepos maps each org to the repo hosting its bot-comment dispatcher
// workflow. The dispatcher must live inside the target org: it reads that org's
// App private-key secret, and org secrets do not cross org boundaries, so each
// org gets its own dispatcher and routing entry.
var botDispatchRepos = map[string]string{
	orgAnovelKit: orgAnovelKit + "/stack",
	orgAnovel:    orgAnovel + "/.github",
}

// botBatchItem is one comment in a `--batch` payload: a target number, the
// markdown body, and an optional inline review-comment ID to reply to. The
// JSON tags match an element of the dispatcher's `comments` array, so the
// parsed items re-marshal straight into the dispatch input.
type botBatchItem struct {
	Number  int64  `json:"number"`
	Body    string `json:"body"`
	ReplyTo int64  `json:"reply_to,omitempty"`
}

func newCoreBotCommentCmd() *cobra.Command {
	var (
		body      string
		replyTo   string
		batchPath string
	)
	cmd := &cobra.Command{
		Use:   "bot-comment <org> <repo> [<number>]",
		Short: "Post one or more PR/issue/review comments as the org App bot",
		Long: `Posts comments that attribute to <app-slug>[bot] instead of your
user account, by triggering the bot-comment workflow in the target org's
dispatcher repo with your own gh token. The App signing key never touches
your machine.

This is the ONLY supported way to comment as the bot. PR/issue authoring,
edits, ready/merge/close stay on your operator gh token (the bot cannot
do them — the workflow only posts comments).

Works against both PRs and issues, for both new comments and replies:

  # comment on a PR (top-level "Conversation" comment)
  a-novel core bot-comment a-novel service-authentication 549 \
      --body "automated review note"

  # reply inside a PR's inline review thread. <id> is a REST
  # review-comment id from repos/<org>/<repo>/pulls/<n>/comments.
  a-novel core bot-comment a-novel service-authentication 549 \
      --reply-to 2103847559 --body "Fixed in abc1234."

  # post MANY comments in a single run — a JSON array of
  # {number, body, reply_to?}, all targeting the one <repo>:
  a-novel core bot-comment a-novel service-authentication --batch replies.json
  # ... or from stdin:
  echo '[{"number":549,"reply_to":2103847559,"body":"Fixed in abc1234."}]' \
      | a-novel core bot-comment a-novel service-authentication --batch -

--reply-to targets a PR inline review thread only; issues have no review
threads, so to answer an issue comment just post a new comment (the body
can quote it with a leading '>').

The command blocks until each workflow run finishes and exits non-zero if
one failed, printing the failed step's logs. A --batch larger than one
dispatch is split into several runs.

Requires: gh authenticated as yourself, with actions:write on the target
org's dispatcher repo.`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			org, repo := args[0], args[1]
			if err := validateBotOrgRepo(org, repo); err != nil {
				return err
			}

			if batchPath != "" {
				if len(args) != 2 {
					return errors.New("bot-comment: with --batch, pass <org> <repo> only — each comment carries its own number")
				}
				if body != "" || replyTo != "" {
					return errors.New("bot-comment: --batch cannot be combined with --body or --reply-to")
				}
				return runBotCommentBatch(cmd, org, repo, batchPath)
			}

			if len(args) != 3 {
				return errors.New("bot-comment: <number> is required (or use --batch to post many comments)")
			}
			number := args[2]
			if err := validateBotCommentArgs(org, repo, number); err != nil {
				return err
			}
			if strings.TrimSpace(body) == "" {
				return errors.New("bot-comment: --body is required and must be non-empty")
			}
			if replyTo != "" {
				if n, err := strconv.Atoi(replyTo); err != nil || n <= 0 {
					return fmt.Errorf("bot-comment: --reply-to %q must be a positive integer (a REST review-comment id)", replyTo)
				}
			}
			return runBotComment(cmd, org, repo, number, body, replyTo)
		},
	}
	cmd.Flags().StringVar(&body, "body", "", "markdown comment body (required in single mode)")
	cmd.Flags().StringVar(&replyTo, "reply-to", "", "inline review-comment id to reply to (omit for a top-level comment)")
	cmd.Flags().StringVar(&batchPath, "batch", "", "path to a JSON array of {number, body, reply_to?} (or - for stdin); posts all in one run")
	return cmd
}

// validateBotOrgRepo checks the org is a configured dispatcher target and
// the repo is a bare name. Shared by the single and batch paths.
func validateBotOrgRepo(org, repo string) error {
	if _, ok := botDispatchRepos[org]; !ok {
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

// validateBotCommentArgs checks the single-comment positional args before a
// dispatch is spent on them.
func validateBotCommentArgs(org, repo, number string) error {
	if err := validateBotOrgRepo(org, repo); err != nil {
		return err
	}
	if n, err := strconv.Atoi(number); err != nil || n <= 0 {
		return fmt.Errorf("bot-comment: number %q must be a positive integer", number)
	}
	return nil
}

// runBotComment dispatches a single comment and watches its run.
func runBotComment(cmd *cobra.Command, org, repo, number, body, replyTo string) error {
	formArgs := []string{"-f", "repo=" + repo, "-f", "number=" + number, "-f", "body=" + body}
	if replyTo != "" {
		formArgs = append(formArgs, "-f", "reply_to="+replyTo)
	}
	label := fmt.Sprintf("for %s/%s#%s", org, repo, number)
	return dispatchBotComment(cmd, botDispatchRepos[org], label, formArgs...)
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
	dispatchRepo := botDispatchRepos[org]
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

// readBotBatch reads the --batch input — a file path, or "-" for stdin —
// parses it into comment items, and validates the shape the dispatcher cannot
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
// lone comment larger than maxBytes is a hard error — it cannot be split.
func chunkBotComments(items []botBatchItem, maxBytes int) ([][]botBatchItem, error) {
	var (
		chunks  [][]botBatchItem
		current []botBatchItem
	)
	for _, item := range items {
		solo, err := json.Marshal([]botBatchItem{item})
		if err != nil {
			return nil, fmt.Errorf("marshal comment number %d: %w", item.Number, err)
		}
		if len(solo) > maxBytes {
			return nil, fmt.Errorf("comment number %d is %d bytes, over the %d-byte dispatch limit", item.Number, len(solo), maxBytes)
		}
		if len(current) > 0 {
			// Full three-index slice so append can't alias current's backing
			// array while we are only measuring the trial size.
			trial, err := json.Marshal(append(current[:len(current):len(current)], item))
			if err != nil {
				return nil, fmt.Errorf("marshal batch chunk: %w", err)
			}
			if len(trial) > maxBytes {
				chunks = append(chunks, current)
				current = nil
			}
		}
		current = append(current, item)
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

	dispatchArgs := append([]string{
		"workflow", "run", botWorkflowFile,
		"--repo", dispatchRepo,
		"-f", "nonce=" + nonce,
	}, formArgs...)
	if _, err := runGHOut(dispatchArgs...); err != nil {
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
	watch := exec.Command("gh", "run", "watch", strconv.FormatInt(runID, 10),
		"--repo", dispatchRepo, "--exit-status", "--interval", "3")
	watch.Stdout = cmd.OutOrStdout()
	watch.Stderr = cmd.ErrOrStderr()
	if err := watch.Run(); err != nil {
		// The run failed; pull the failed step's logs to explain why.
		logs, _ := runGHOut("run", "view", strconv.FormatInt(runID, 10),
			"--repo", dispatchRepo, "--log-failed")
		return fmt.Errorf("bot-comment: workflow run %d failed: %w\n%s", runID, err, logs)
	}
	return nil
}

// waitForDispatchedRun polls the run list until a run whose name carries the
// nonce appears, and returns its database ID. workflow_dispatch returns no run
// ID and the run takes a moment to register, hence the bounded poll.
func waitForDispatchedRun(dispatchRepo, nonce string) (int64, error) {
	deadline := time.Now().Add(botRunLookupTimeout)
	for {
		out, err := runGHOut("run", "list",
			"--repo", dispatchRepo,
			"--workflow", botWorkflowFile,
			"--event", "workflow_dispatch",
			"--limit", "30",
			"--json", "databaseId,displayTitle")
		if err == nil {
			var runs []struct {
				DatabaseID   int64  `json:"databaseId"`
				DisplayTitle string `json:"displayTitle"`
			}
			if jsonErr := json.Unmarshal([]byte(out), &runs); jsonErr == nil {
				for _, r := range runs {
					if strings.Contains(r.DisplayTitle, "("+nonce+")") {
						return r.DatabaseID, nil
					}
				}
			}
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("dispatched run did not appear within %s "+
				"(check 'gh run list --repo %s')", botRunLookupTimeout, dispatchRepo)
		}
		time.Sleep(2 * time.Second)
	}
}

// runGHOut runs gh with the operator's own credentials and returns stdout
// only. stderr is captured separately, so gh's progress and warning chatter
// cannot corrupt the JSON parsed from stdout by the run-list poll; on failure
// it is folded into the returned error.
func runGHOut(args ...string) (string, error) {
	c := exec.Command("gh", args...)
	c.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return stdout.String(), nil
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
