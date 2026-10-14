#!/usr/bin/env bash
# Runs every scenario of the original page on its engine (Node) and on the Go
# engine and requires the checkpoint lines to be identical.
#
# The territorial rules (amigo, amigo-udx) have no original. Their scenarios run
# on the native Go engine and, when WASM is set, on the compiled module, and the
# two must agree.
#   FILTER=<regex>   only scenarios whose name matches
#   WASM=<path>      run the compiled module (through its exports) instead of
#                    the native Go engine against the original
set -euo pipefail
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

go build -o "$tmp/trace" ./cmd/trace
filtered() { # list file, output file
  if [ -n "${FILTER:-}" ]; then
    grep -E "\"name\":\"[^\"]*(${FILTER})" "$1" > "$2" || true
  else
    cp "$1" "$2"
  fi
}

node reference/emit_scenarios.mjs > "$tmp/all.jsonl"
filtered "$tmp/all.jsonl" "$tmp/scenarios.jsonl"
echo "scenarios: $(wc -l < "$tmp/scenarios.jsonl" | tr -d ' ')"

if [ -s "$tmp/scenarios.jsonl" ]; then
  if [ -n "${WASM:-}" ]; then
    echo "engine under test: $WASM"
    node reference/run_wasm.mjs < "$tmp/scenarios.jsonl" > "$tmp/go.txt"
  else
    echo "engine under test: native Go"
    "$tmp/trace" < "$tmp/scenarios.jsonl" > "$tmp/go.txt"
  fi
  node reference/run_ref.mjs < "$tmp/scenarios.jsonl" > "$tmp/js.txt"

  echo "lines: go=$(wc -l < "$tmp/go.txt" | tr -d ' ') js=$(wc -l < "$tmp/js.txt" | tr -d ' ')"
  if cmp -s "$tmp/go.txt" "$tmp/js.txt"; then
    echo "IDENTICAL"
  else
    echo "DIFFERENT -- first differences (go < / js >):"
    diff "$tmp/go.txt" "$tmp/js.txt" | head -20
    cp "$tmp/go.txt" /tmp/websim_go.txt; cp "$tmp/js.txt" /tmp/websim_js.txt
    echo "full outputs kept at /tmp/websim_go.txt and /tmp/websim_js.txt"
    exit 1
  fi
fi

node reference/emit_scenarios.mjs --territorial > "$tmp/terr_all.jsonl"
filtered "$tmp/terr_all.jsonl" "$tmp/terr.jsonl"
echo "territorial scenarios: $(wc -l < "$tmp/terr.jsonl" | tr -d ' ')"
[ -s "$tmp/terr.jsonl" ] || exit 0
"$tmp/trace" < "$tmp/terr.jsonl" > "$tmp/terr_native.txt"
if [ -z "${WASM:-}" ]; then
  echo "territorial scenarios have no original; set WASM to compare the module against the native engine"
  exit 0
fi
node reference/run_wasm.mjs < "$tmp/terr.jsonl" > "$tmp/terr_wasm.txt"
echo "lines: native=$(wc -l < "$tmp/terr_native.txt" | tr -d ' ') module=$(wc -l < "$tmp/terr_wasm.txt" | tr -d ' ')"
if cmp -s "$tmp/terr_native.txt" "$tmp/terr_wasm.txt"; then
  echo "IDENTICAL (native Go and compiled module)"
else
  echo "DIFFERENT -- first differences (native < / module >):"
  diff "$tmp/terr_native.txt" "$tmp/terr_wasm.txt" | head -20
  cp "$tmp/terr_native.txt" /tmp/websim_terr_native.txt; cp "$tmp/terr_wasm.txt" /tmp/websim_terr_wasm.txt
  echo "full outputs kept at /tmp/websim_terr_native.txt and /tmp/websim_terr_wasm.txt"
  exit 1
fi
