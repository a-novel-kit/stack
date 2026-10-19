/**
 * Verifies that the agent-skill symlinks git tracks are still symlinks.
 *
 * Skills live once under `.agents/skills`, the path Codex and Copilot scan directly. Claude
 * Code scans only `.claude/skills`, so that path is a link into the same source. A checkout
 * on a filesystem without symlink support materializes the link as a small text file holding
 * its target path, and staging that file rewrites the entry as a regular blob, which drops
 * the link for every other contributor. Comparing the recorded mode catches that before it
 * lands.
 */
import { execFileSync } from "node:child_process";
import { existsSync, lstatSync, readlinkSync } from "node:fs";
import { dirname, join } from "node:path";

/** Git's blob mode for a symbolic link. Regular files are `100644`. */
const SYMLINK_MODE = "120000";

/** Each tracked link, mapped to the path it must point at. */
const TRACKED_LINKS = {
  ".claude/skills": "../.agents/skills",
};

/** Returns why a tracked link is broken, or "" when it is sound. */
function linkProblem(link, target) {
  const entry = execFileSync("git", ["ls-files", "-s", "--", link], { encoding: "utf8" }).trim();
  if (!entry) {
    return `${link} is not tracked`;
  }

  const mode = entry.split(/\s+/)[0];
  if (mode !== SYMLINK_MODE) {
    return `${link} is recorded as mode ${mode}, so it is committed as a regular file; restore it with \`ln -s ${target} ${link}\``;
  }

  // Checkouts without symlink support hold a regular file here. The commit itself is
  // already verified above, so only a link that survived checkout gets validated.
  if (!lstatSync(link, { throwIfNoEntry: false })?.isSymbolicLink()) {
    return "";
  }

  const actual = readlinkSync(link);
  if (actual !== target) {
    return `${link} points at ${actual}, expected ${target}`;
  }
  if (!existsSync(join(dirname(link), target))) {
    return `${link} points at ${target}, which does not exist`;
  }
  return "";
}

const failures = Object.entries(TRACKED_LINKS)
  .map(([link, target]) => linkProblem(link, target))
  .filter(Boolean);

for (const failure of failures) {
  // GitHub renders `::error::` as an annotation on the run, which surfaces the fix without
  // opening the job log.
  console.error(process.env.GITHUB_ACTIONS ? `::error::${failure}` : `error: ${failure}`);
}

process.exit(failures.length > 0 ? 1 : 0);
