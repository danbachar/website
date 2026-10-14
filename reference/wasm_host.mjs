// Loads the engine module in Node. The module is the TinyGo wasm-unknown build,
// so it imports nothing; _initialize sets up its runtime before the first call.
import { readFileSync } from "node:fs";

export async function loadWasm(path) {
  const { instance } = await WebAssembly.instantiate(readFileSync(path), {});
  instance.exports._initialize();
  return instance.exports;
}
