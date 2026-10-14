// The original page's engine, sliced out of original.html between the
// "algorithms" and "rendering" markers and wrapped in a function so each call
// gets its own state. The slice is not edited; only renderSends (DOM) is
// stubbed, and the accessors below are appended after it.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const lines = readFileSync(path.join(here, "original.html"), "utf8").split("\n");
const RULE = "  // ----------------------------------------------------------------";
const start = lines.findIndex((l) => l.startsWith(RULE) && l.includes("algorithms"));
const end = lines.findIndex((l) => l.startsWith(RULE) && l.includes("rendering"));
if (start < 0 || end < 0 || end <= start) throw new Error("engine markers not found in original.html");
const chunk = lines.slice(start, end).join("\n");

const factory = new Function(
  "renderSends",
  `"use strict";
${chunk}
return {
  ALGOS,
  get G() { return G; },
  get live() { return live; },
  get sends() { return sends; },
  get current() { return current; }, set current(v) { current = v; },
  SK,
  setGeometry(world, cell) { WORLD = world; CELL = cell; RANGE = cell; GRID = world / cell; },
  setMaxLinks(v) { MAX_LINKS = v; },
  setMaxAge(v) { MAX_AGE = v; },
  setTickMs(v) { TICK_MS = v; },
  get GRID() { return GRID; },
  get MAX_LINKS() { return MAX_LINKS; },
  get MAX_NODES() { return MAX_NODES; },
  buildTopology, bumpGraph, startLive, liveStep, sendTracked, runHeadless,
  ruleTargets, entrySet, custodyTotal,
  setGraph(g) { G = g; },
  clearLanes() { if (live) live.lanes.clear(); },
};`
);

export function makeRef() {
  return factory(() => {});
}

export const TTL = 7;
