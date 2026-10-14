# web-sim-go

The browser simulation "Forwarding rules in the Grassroots BLE mesh", with its
engine rewritten in Go and compiled to WebAssembly with TinyGo. The page keeps
the original's interface; the topology, the forwarding rules and the live mesh
that the original computed in its `<script>` now live in `engine/`. Two rules
from the thesis, `amigo` and `amigo-udx`, were added to the original's list.

| path | what |
|---|---|
| `engine/` | the simulation: topology, rules, live mesh. Plain Go, no I/O |
| `bridge/` | the engine as flat arrays, for a host that can only pass numbers and read the module's memory |
| `cmd/wasm/` | the module's interface: `//go:wasmexport gsn_*` functions over the bridge. Builds only under TinyGo |
| `cmd/trace/` | runs scenarios natively and prints the checkpoint lines `compare.sh` checks |
| `sim/` | the simulator on the page: `app.js` (drawing and input), `engine.js` (loads the module), `layout.mjs` (snapshot layout), `engine.wasm` |
| `reference/` | the original page and the harness that runs its engine in Node |

## Build and run

```
make serve     # builds sim/engine.wasm, serves the site at http://127.0.0.1:8765/
```

`make wasm` only builds. It needs TinyGo, a Go version TinyGo supports (0.43
takes 1.25 to 1.27) and `wasm-opt` from Binaryen on PATH. The module targets
`wasm-unknown`, so it imports nothing, and comes to about 48 KB. It is built
with `-gc=precise` because that target's default collector never frees memory.

With Homebrew, `brew install tinygo-org/tools/tinygo go` provides all three;
put its `bin` first on PATH so `go` is the new one. On Apple silicon use the
native Homebrew in `/opt/homebrew`: under Rosetta it has no prebuilt Binaryen
and tries to compile CMake.

The engine uses no reflection or `fmt`, nothing in it depends on map iteration
order, and the module imports only `math` and `math/bits` beyond the runtime.

## Check

```
make test      # go vet (also of the module's exports), tests of the lane cache and of the territories, and a test that sim/layout.mjs matches the bridge's constants
make verify    # needs Node (tested with 25.9)
```

`make verify` feeds 547 scenarios (513 topologies across sizes, seeds,
geometries and link caps; 34 live runs covering the four rules the original
has, with custody overflow, mid-run changes to α, β and F, and repeated sends)
to the original page's engine and to this one, natively and as the compiled
module called through its exports. Each prints a hash of its state at every
checkpoint; the two outputs must be byte-identical.

The 16 scenarios of the two territorial rules have no original to run against.
They run on the native engine and on the compiled module, and those two must
agree.

## What differs from the original

- **The live mesh runs on ticks, not on the clock.** A transfer arrives after
  its duration in ticks (the original compared `performance.now()`), and
  background traffic comes from a seeded generator (the original used
  `Math.random`). A mesh is therefore reproducible from its seed. `make verify`
  gives the original the same generator and a clock that advances one tick per
  step, the case where wall-clock and tick arrival agree by construction.
- **A node's cached lane choice goes when the node drops the packet.** In the
  original it stays, so memory grows with every message the mesh ever carried:
  on the original engine at 64 nodes under top-3, `live.lanes` reached 316,000
  entries and the JS heap 59 MB after 100,000 ticks, while custody held about a
  thousand packets. Dropping the entry changes nothing the mesh does: the
  digests of three runs totalling 360,000 ticks are identical with and without
  it. `engine/lanes_test.go` guards it.
- **The animated single-message branch is not ported.** In the original it is
  dead (`else if (false && sim)`, `reference/original.html:1386`); the headless
  run that shares its rules is ported and checked.
- **Two rules the original lacks: `amigo` and `amigo-udx`**, the thesis's leader
  arms. Both elect leaders on buffer headroom, `S = 1 − ρ`: a node leads when no
  peer it is linked to has more headroom, an incumbent keeps the role unless a
  peer beats it by 0.1, and the election re-runs every 10 ticks
  (`engine/territory.go`). Every node answers to the best leader it is linked to,
  so a member is one link from its leader and hands every packet to that leader
  alone. A leader delivers to its own members and hands a packet for another
  territory to that territory's leader, or offers it to every leader it is
  linked to when it has no such link. Under `amigo` two leaders within 2.4 radio
  ranges hear each other over BLE. Under `amigo-udx` each leader holds UDX
  streams to six leaders, the ones either side of it in id order; a stream does
  not share the radio and carries one copy per tick. Nothing can check them
  against the original, so `engine/territory_test.go` covers them, and the
  native engine and the compiled module are run against each other.
- **Where `amigo` follows the thesis text, not the simulator behind its
  numbers.** The simulator scores leaders on buffer headroom alone, as here,
  where the thesis says buffer occupancy and node degree. And as its
  `territoryRelay` branch of `lanesFor` reads, a member holding a packet for
  another territory offers it only to a foreign leader it is directly linked to,
  never to its own leader. Here it goes to its own leader, as the thesis
  describes, so the delivery the simulator measured for `amigo` (0.78 at 392
  nodes) need not match what this model shows.
- **Leaders keep the link cap.** A leader has the same `MaxLinks` radio slots
  as any node, as in the simulator, where a link needs a free slot at both
  ends. Its members are the nodes it is linked to, so it never serves more than
  its radio links. Under `amigo`, the long-range links to leaders within reach
  take only the slots its radio links leave free, nearest pair first; a leader
  with a full radio gets none. The UDX backbone keeps its ring of 6, since
  those streams do not share the radio.
- **`hgateway` and `hcentral`, two of the original's six rules, are removed.**
- **Flash links are drawn in link order**, not insertion order.

## Memory

TinyGo's heap only grows: it doubles when a collection cannot free enough
contiguous space. With the lane cache pruned it levels off. After 200,000 ticks
at 64 nodes under top-3 the module holds 32 MiB; 256 nodes under flood hold
128 MiB after 60,000 ticks.
