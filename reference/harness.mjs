// Runs scenarios on the original page's engine and prints one line per
// checkpoint. cmd/trace prints the same lines from the Go engine; the two
// outputs must be byte-identical.
//
// Determinism: the original reads Math.random() for background traffic and
// performance.now() for when a copy lands. Both are replaced here, the clock
// advancing exactly one TICK_MS per tick, which is the case where wall-clock
// arrival and tick arrival agree by construction.
import { makeRef } from "./ref_engine.mjs";

// The rules the original page has, which its engine can run.
export const RULES = ["flood", "f3", "hmulti", "oracle"];
// Every rule, as the engine numbers them.
export const RULE_IDS = { flood: 0, f3: 1, hmulti: 2, amigo: 3, "amigo-udx": 4, oracle: 5 };
const TICK_MS = 100;

export function mulberry32(a) {
  return function () {
    a |= 0; a = (a + 0x6D2B79F5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// FNV-1a over 32-bit words; same stream as engine/digest.go.
export function Hasher() {
  let h = 0x811c9dc5;
  const f64 = new Float64Array(1), u32 = new Uint32Array(f64.buffer);
  const i32 = (v) => {
    const u = v >>> 0;
    for (let k = 0; k < 4; k++) h = Math.imul(h ^ ((u >>> (8 * k)) & 0xff), 16777619);
  };
  return {
    i32,
    bool: (v) => i32(v ? 1 : 0),
    f64: (v) => { f64[0] = v; i32(u32[0]); i32(u32[1]); },
    done: () => (h >>> 0).toString(16).padStart(8, "0"),
  };
}

function boundaryShare(G) {
  let members = 0, boundary = 0;
  for (let i = 0; i < G.nodes.length; i++) {
    members++;
    if (G.adj[i].some((j) => G.nodes[j].cluster !== G.nodes[i].cluster)) boundary++;
  }
  return boundary / Math.max(1, members);
}

function adjDigest(x, G) {
  for (let i = 0; i < G.nodes.length; i++) {
    x.i32(G.adj[i].length);
    for (const j of G.adj[i]) x.i32(j);
  }
}

function topologyDigest(G) {
  const x = Hasher();
  x.i32(G.nodes.length);
  for (const n of G.nodes) { x.f64(n.x); x.f64(n.y); x.i32(n.cluster); }
  for (let i = 0; i < G.nodes.length; i++) {
    x.i32(G.inRange[i].length);
    for (const j of G.inRange[i]) x.i32(j);
  }
  adjDigest(x, G);
  x.i32(G.src); x.i32(G.dst);
  x.f64(G.meanRange); x.f64(G.meanLink); x.f64(G.boundaryShare);
  return x.done();
}

function liveDigest(ref) {
  const L = ref.live, G = ref.G, n = G.nodes.length, x = Hasher();
  x.i32(L.t); x.i32(L.created); x.i32(L.delivered); x.i32(L.tx);
  x.i32(L.rotations); x.i32(L.agedOut); x.i32(L.nextId);
  x.i32(L.transfers.length); x.i32(ref.custodyTotal());
  for (let i = 0; i < n; i++) {
    x.i32(L.hold[i].size);
    for (const [id, ttl] of L.hold[i]) { x.i32(id); x.i32(ttl); }
  }
  for (const tr of L.transfers) {
    x.i32(tr.from); x.i32(tr.to); x.i32(tr.id); x.i32(tr.ttl);
    x.i32(tr.born / TICK_MS); x.i32(tr.ms / TICK_MS);
  }
  adjDigest(x, G);
  const fl = [...L.flash].map(([k, f]) => {
    const [a, b] = k.split(":").map(Number);
    return { a, b, until: f.until, kind: f.kind === "drop" ? 0 : 1 };
  }).sort((p, q) => p.a - q.a || p.b - q.b);
  x.i32(fl.length);
  for (const f of fl) { x.i32(f.a); x.i32(f.b); x.i32(f.until); x.i32(f.kind); }
  const T = L.track;
  if (T) {
    x.i32(1); x.i32(T.id); x.i32(T.tx); x.i32(T.hops === null ? -1 : T.hops);
    x.i32(T.born); x.bool(T.done); x.bool(!!T.expired);
    x.i32(T.copiesSum); x.i32(T.ticks); x.i32(T.at ?? 0);
  } else x.i32(0);
  x.i32(L.dstOf.size);
  for (const [id, dst] of L.dstOf) { x.i32(id); x.i32(dst); }
  x.i32(L.bornOf.size);
  for (const [id, born] of L.bornOf) { x.i32(id); x.i32(born); }
  for (let i = 0; i < n; i++) x.i32(L.seen[i].size);
  return x.done();
}

function targetsDigest(ref, dst, rule) {
  const x = Hasher();
  for (let i = 0; i < ref.G.nodes.length; i++) {
    const t = ref.ruleTargets(i, dst, rule);
    x.i32(t.length);
    for (const p of t) x.i32(p);
  }
  return x.done();
}

function build(ref, sc) {
  ref.setGeometry(sc.world, sc.cell);
  ref.setMaxLinks(sc.maxLinks);
  ref.setMaxAge(sc.maxAge ?? 1500);
  ref.setTickMs(TICK_MS);
  if (sc.sk) { ref.SK.alpha = sc.sk.alpha; ref.SK.beta = sc.sk.beta; ref.SK.F = sc.sk.F; }
  const cells = ref.GRID * ref.GRID;
  const n = Math.max(1, Math.min(sc.n, Math.floor(ref.MAX_NODES / cells)));
  const G = ref.buildTopology(n, sc.seed, 0);
  ref.setGraph(G);
  ref.bumpGraph();
  G.boundaryShare = boundaryShare(G);
  return G;
}

export function runScenario(sc) {
  const out = [], P = (...a) => out.push(`${sc.name} ${a.join(" ")}`);
  const ref = makeRef();
  const G = build(ref, sc);
  P("topo", G.nodes.length, topologyDigest(G));

  if (sc.kind === "topology") {
    const pr = mulberry32(sc.seed * 31 + 7), n = G.nodes.length;
    const pairs = [[G.src, G.dst]];
    for (let k = 0; k < 3; k++) {
      const a = (pr() * n) | 0, b = (pr() * n) | 0;
      if (a !== b) pairs.push([a, b]);
    }
    for (const rule of RULES) {
      P("targets", rule, targetsDigest(ref, G.dst, rule));
      const lx = Hasher();
      for (let i = 0; i < G.nodes.length; i++) lx.i32(ref.ruleTargets(i, G.dst, rule).length);
      P("lanes", rule, lx.done());
      if (rule.startsWith("h")) {
        const ex = Hasher(), ix = Hasher();
        let count = 0;
        for (let c = 0; c < ref.GRID * ref.GRID; c++) for (const i of ref.entrySet(c, rule)) { ex.i32(i); count++; }
        P("entries", rule, count, ex.done());
        for (let i = 0; i < G.nodes.length; i++) ix.i32(ref.entrySet(G.nodes[i].cluster, rule).has(i) ? 1 : 0);
        P("isentry", rule, ix.done());
      }
      for (const [s, d] of pairs) {
        const r = ref.runHeadless(s, d, rule);
        P("headless", rule, s, d, r.tx, r.hops === null ? -1 : r.hops, r.delivered ? 1 : 0);
      }
    }
    return out;
  }

  ref.current = sc.rule;
  const realRandom = Math.random, realPerf = Object.getOwnPropertyDescriptor(globalThis, "performance");
  let clock = 0;
  Math.random = mulberry32(sc.trafficSeed);
  Object.defineProperty(globalThis, "performance", { value: { now: () => clock }, configurable: true, writable: true });
  try {
    ref.startLive();
    P("tick", 0, liveDigest(ref));
    for (let t = 1; t <= sc.ticks; t++) {
      clock = t * TICK_MS;
      ref.liveStep();
      if (t % sc.every === 0 || t === sc.ticks) P("tick", t, liveDigest(ref));
      for (const s of sc.sends ?? []) {
        if (s.at !== t) continue;
        ref.G.src = s.src; ref.G.dst = s.dst;
        ref.live.track = null;
        ref.sendTracked();
        P("send", t, ref.live.track ? 1 : 0, liveDigest(ref));
      }
      for (const c of sc.skChanges ?? []) {
        if (c.at !== t) continue;
        ref.SK.alpha = c.alpha; ref.SK.beta = c.beta; ref.SK.F = c.F;
        ref.bumpGraph(); ref.clearLanes();
        P("sk", t, liveDigest(ref));
      }
    }
  } finally {
    Math.random = realRandom;
    if (realPerf) Object.defineProperty(globalThis, "performance", realPerf);
  }
  const L = ref.live;
  const fb = new Float64Array(1), ub = new Uint32Array(fb.buffer);
  const bitsHex = (v) => { fb[0] = v; return ub[1].toString(16).padStart(8, "0") + ub[0].toString(16).padStart(8, "0"); };
  ref.sends.forEach((r, i) => P("rec", i, r.outcome === "arrived" ? 0 : 1, r.hops ?? -1, r.ticks, r.tx, bitsHex(r.copies)));
  P("end", L.created, L.delivered, L.tx, L.rotations, L.agedOut, ref.custodyTotal());
  return out;
}

export function scenarios() {
  const sc = [];
  const geo = [[200, 50], [200, 25], [200, 20], [200, 40], [200, 100], [400, 50], [400, 100], [600, 100], [800, 100], [400, 25]];
  for (let seed = 1; seed <= 12; seed++) {
    for (const n of [1, 2, 4, 8, 16, 20]) {
      for (const maxLinks of [1, 2, 3, 5, 7, 12]) {
        sc.push({ kind: "topology", name: `topo-s${seed}-n${n}-l${maxLinks}-w200c50`, n, seed, world: 200, cell: 50, maxLinks });
      }
    }
  }
  for (const [world, cell] of geo.slice(1)) {
    for (const seed of [1, 2, 3]) {
      for (const n of [1, 4, 20]) {
        sc.push({ kind: "topology", name: `topo-s${seed}-n${n}-l7-w${world}c${cell}`, n, seed, world, cell, maxLinks: 7 });
      }
    }
  }

  const pickPair = (seed, k, total) => {
    const r = mulberry32(seed * 977 + k);
    let a = (r() * total) | 0, b = (r() * total) | 0;
    if (a === b) b = (b + 1) % total;
    return [a, b];
  };
  for (const rule of RULES) {
    for (const [n, seed, world, cell, maxLinks] of [[4, 1, 200, 50, 7], [4, 2, 200, 50, 7], [2, 3, 200, 50, 5], [8, 4, 200, 25, 7], [3, 5, 400, 100, 7]]) {
      const grid = world / cell, total = Math.max(1, Math.min(n, Math.floor(4000 / (grid * grid)))) * grid * grid;
      const sends = [30, 120, 260, 420].map((at, k) => { const [src, dst] = pickPair(seed, k, total); return { at, src, dst }; });
      sc.push({ kind: "live", name: `live-${rule}-s${seed}-n${n}-w${world}c${cell}-l${maxLinks}`, n, seed, world, cell, maxLinks,
        maxAge: 1500, rule, sk: { alpha: 0.5, beta: 0, F: 3 }, trafficSeed: seed * 13 + 5, ticks: 700, every: 5, sends });
    }
  }
  for (const sk of [{ alpha: 0.5, beta: 0, F: 3 }, { alpha: 1, beta: 0.5, F: 2 }, { alpha: 0.25, beta: 1, F: 7 }, { alpha: 0, beta: 1, F: 1 }]) {
    for (const maxAge of [200, 1500]) {
      const total = 4 * 16, [src, dst] = pickPair(9, 1, total);
      sc.push({ kind: "live", name: `live-f3-sk${sk.alpha}-${sk.beta}-${sk.F}-age${maxAge}`, n: 4, seed: 9, world: 200, cell: 50, maxLinks: 7,
        maxAge, rule: "f3", sk, trafficSeed: 71, ticks: 900, every: 5,
        sends: [{ at: 40, src, dst }, { at: 300, src: dst, dst: src }] });
    }
  }
  // Long, congested, never-aging floods: these are the settings under which a
  // node's custody passes the 60-packet cap and the oldest entry is evicted.
  sc.push({ kind: "live", name: "live-flood-custody-overflow-n8", n: 8, seed: 3, world: 200, cell: 50, maxLinks: 7, maxAge: 100000,
    rule: "flood", sk: { alpha: 0.5, beta: 0, F: 3 }, trafficSeed: 5, ticks: 4200, every: 300,
    sends: [{ at: 3300, src: 4, dst: 90 }] });
  sc.push({ kind: "live", name: "live-flood-custody-overflow-n20c100", n: 20, seed: 3, world: 200, cell: 100, maxLinks: 7, maxAge: 100000,
    rule: "flood", sk: { alpha: 0.5, beta: 0, F: 3 }, trafficSeed: 5, ticks: 2600, every: 200, sends: [] });
  sc.push({ kind: "live", name: "live-f3-sk-midrun", n: 4, seed: 2, world: 200, cell: 50, maxLinks: 7, maxAge: 1500,
    rule: "f3", sk: { alpha: 0.5, beta: 0, F: 3 }, trafficSeed: 8, ticks: 600, every: 10,
    sends: [{ at: 20, src: 3, dst: 50 }],
    skChanges: [{ at: 100, alpha: 1, beta: 0, F: 2 }, { at: 250, alpha: 0.25, beta: 1, F: 7 }, { at: 400, alpha: 0.5, beta: 0, F: 3 }] });
  sc.push({ kind: "live", name: "live-hmulti-same-endpoint", n: 4, seed: 1, world: 200, cell: 50, maxLinks: 7, maxAge: 1500,
    rule: "hmulti", sk: { alpha: 0.5, beta: 0, F: 3 }, trafficSeed: 2, ticks: 60, every: 10,
    sends: [{ at: 10, src: 5, dst: 5 }, { at: 20, src: 5, dst: 30 }] });
  for (const rule of ["flood", "hmulti"]) {
    sc.push({ kind: "live", name: `live-${rule}-long-age120`, n: 6, seed: 7, world: 200, cell: 50, maxLinks: 7, maxAge: 120,
      rule, sk: { alpha: 0.5, beta: 0, F: 3 }, trafficSeed: 3, ticks: 1500, every: 25,
      sends: [{ at: 10, src: 5, dst: 70 }, { at: 400, src: 9, dst: 33 }] });
  }
  return sc;
}

// Scenarios for the rules the original page lacks. No original engine can run
// them, so they are checked by running the native Go engine and the compiled
// module against each other.
export function territorialScenarios() {
  const sc = [];
  const pickPair = (seed, k, total) => {
    const r = mulberry32(seed * 977 + k);
    let a = (r() * total) | 0, b = (r() * total) | 0;
    if (a === b) b = (b + 1) % total;
    return [a, b];
  };
  const SK = { alpha: 0.5, beta: 0, F: 3 };
  for (const rule of ["amigo", "amigo-udx"]) {
    for (const [n, seed, world, cell, maxLinks] of [[4, 1, 200, 50, 7], [4, 2, 200, 50, 7], [2, 3, 200, 50, 5], [8, 4, 200, 25, 7], [3, 5, 400, 100, 7], [8, 6, 400, 50, 7]]) {
      const grid = world / cell, total = Math.max(1, Math.min(n, Math.floor(4000 / (grid * grid)))) * grid * grid;
      const sends = [30, 120, 260, 420].map((at, k) => { const [src, dst] = pickPair(seed, k, total); return { at, src, dst }; });
      sc.push({ kind: "live", name: `terr-${rule}-s${seed}-n${n}-w${world}c${cell}-l${maxLinks}`, n, seed, world, cell, maxLinks,
        maxAge: 1500, rule, sk: SK, trafficSeed: seed * 13 + 5, ticks: 700, every: 5, sends });
    }
    // Long enough for custody to fill, leaders to change hands many times and
    // copies to age out.
    const [src, dst] = pickPair(3, 1, 128), [src2, dst2] = pickPair(3, 2, 128);
    sc.push({ kind: "live", name: `terr-${rule}-long`, n: 8, seed: 3, world: 200, cell: 50, maxLinks: 7, maxAge: 1500,
      rule, sk: SK, trafficSeed: 5, ticks: 4200, every: 300,
      sends: [{ at: 400, src, dst }, { at: 2000, src: src2, dst: dst2 }] });
    sc.push({ kind: "live", name: `terr-${rule}-short-age`, n: 4, seed: 2, world: 200, cell: 50, maxLinks: 7, maxAge: 150,
      rule, sk: SK, trafficSeed: 9, ticks: 1500, every: 25,
      sends: [{ at: 10, src: 5, dst: 40 }, { at: 600, src: 9, dst: 33 }] });
  }
  return sc;
}
