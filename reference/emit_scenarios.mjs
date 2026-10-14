// Writes the scenario list as JSON lines, for cmd/trace to read. With
// --territorial it writes the scenarios of the rules the original page lacks.
import { scenarios, territorialScenarios } from "./harness.mjs";
const list = process.argv.includes("--territorial") ? territorialScenarios() : scenarios();
for (const sc of list) process.stdout.write(JSON.stringify(sc) + "\n");
