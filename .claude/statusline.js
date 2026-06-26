// Claude Code status line — model · effort · cost · context · rate limits,
// shown under the prompt in color. Claude Code pipes the session JSON to this
// script on stdin on every refresh (https://code.claude.com/docs/en/statusline)
// and renders whatever it prints on stdout (ANSI color included).
//
// Node, not a jq/bash one-liner: every contributor already has Node (it is part
// of the a-novel toolchain), whereas jq is not guaranteed to be installed. Set
// NO_COLOR in the environment to disable coloring.
import { readFileSync } from "node:fs";

// ANSI coloring, honoring the NO_COLOR convention (https://no-color.org).
const useColor = !process.env.NO_COLOR;
function color(text, ...codes) {
  return useColor ? `\x1b[${codes.join(";")}m${text}\x1b[0m` : `${text}`;
}

// green → yellow → red as a usage percentage fills toward its ceiling.
function loadColor(pct) {
  return pct < 60 ? 92 : pct < 85 ? 93 : 91;
}

// Warmer as the reasoning effort climbs.
const EFFORT_COLOR = { low: 90, medium: 96, high: 92, xhigh: 93, max: 91 };

function shortTokens(n) {
  return n >= 1000 ? `${Math.round(n / 1000)}k` : `${n}`;
}

function readInput() {
  try {
    return JSON.parse(readFileSync(0, "utf8"));
  } catch {
    return {};
  }
}

const input = readInput();
const segments = [];

const model = input.model?.display_name || input.model?.id || "model?";
segments.push(color(model, 1, 96)); // bold bright-cyan

const effort = input.effort?.level;
if (effort) segments.push(color("effort:", 2) + color(effort, EFFORT_COLOR[effort] ?? 93));

const style = input.output_style?.name;
if (style && style !== "default") segments.push(color(style, 95)); // bright-magenta

const cost = input.cost?.total_cost_usd;
if (typeof cost === "number") segments.push(color(`$${cost.toFixed(2)}`, 92)); // bright-green

const ctx = input.context_window;
if (ctx && typeof ctx.used_percentage === "number") {
  const fill = loadColor(ctx.used_percentage);
  const window = ctx.context_window_size ? color(`/${shortTokens(ctx.context_window_size)}`, 2) : "";
  segments.push(
    color("ctx:", 2) +
      color(shortTokens(ctx.total_input_tokens || 0), fill) +
      window +
      " " +
      color(`${ctx.used_percentage}%`, fill)
  );
}

// Rate-limit budgets (percentage of the window already consumed).
const limits = input.rate_limits;
if (limits) {
  for (const [key, label] of [
    ["five_hour", "5h"],
    ["seven_day", "7d"],
  ]) {
    const pct = limits[key]?.used_percentage;
    if (typeof pct === "number") segments.push(color(`${label} `, 2) + color(`${pct}%`, loadColor(pct)));
  }
}

process.stdout.write(segments.join(color(" · ", 90))); // gray separators
