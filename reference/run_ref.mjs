// Runs scenarios (JSON lines on stdin, or the built-in list) on the original
// engine and prints the checkpoint lines.
import { readFileSync } from "node:fs";
import { runScenario, scenarios } from "./harness.mjs";

const input = process.stdin.isTTY ? null : readFileSync(0, "utf8");
const list = input && input.trim()
  ? input.trim().split("\n").map((l) => JSON.parse(l))
  : scenarios();
for (const sc of list) for (const line of runScenario(sc)) process.stdout.write(line + "\n");
