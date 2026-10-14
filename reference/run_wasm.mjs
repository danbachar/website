// Runs scenarios (JSON lines on stdin) through the compiled module's exports
// and prints the same checkpoint lines harness.mjs prints for the original page.
//   WASM=<path to the compiled module>
import { readFileSync } from "node:fs";
import { loadWasm } from "./wasm_host.mjs";
import { Hasher, mulberry32, RULES, RULE_IDS } from "./harness.mjs";
import { HEADER } from "../sim/layout.mjs";

const x = await loadWasm(process.env.WASM);
const hex = (v) => (v >>> 0).toString(16).padStart(8, "0");
const i32 = (addr, len) => new Int32Array(x.memory.buffer, addr, len);
const fb = new Float64Array(1), ub = new Uint32Array(fb.buffer);
const bitsHex = (v) => { fb[0] = v; return ub[1].toString(16).padStart(8, "0") + ub[0].toString(16).padStart(8, "0"); };

function snapshot() {
  const len = x.gsn_snapshot();
  return i32(x.gsn_snapshot_addr(), len);
}

function run(sc) {
  const out = [], P = (...a) => out.push(`${sc.name} ${a.join(" ")}`);
  x.gsn_set_geometry(sc.world, sc.cell);
  x.gsn_set_max_links(sc.maxLinks);
  x.gsn_set_max_age(sc.maxAge ?? 1500);
  x.gsn_set_rule(2);
  if (sc.sk) x.gsn_set_sk(sc.sk.alpha, sc.sk.beta, sc.sk.F); else x.gsn_set_sk(0.5, 0, 3);
  const total = x.gsn_rebuild(sc.n, sc.seed);
  P("topo", total, hex(x.gsn_topology_digest()));

  if (sc.kind === "topology") {
    const src = x.gsn_src(), dst = x.gsn_dst();
    const pr = mulberry32(sc.seed * 31 + 7);
    const pairs = [[src, dst]];
    for (let k = 0; k < 3; k++) {
      const a = (pr() * total) | 0, b = (pr() * total) | 0;
      if (a !== b) pairs.push([a, b]);
    }
    RULES.forEach((rule) => {
      const ri = RULE_IDS[rule];
      P("targets", rule, hex(x.gsn_targets_digest(dst, ri)));
      x.gsn_set_rule(ri);
      const lx = Hasher();
      for (let i = 0; i < total; i++) lx.i32(x.gsn_lane_count(i));
      P("lanes", rule, lx.done());
      if (rule.startsWith("h")) {
        const cnt = x.gsn_refresh_entries(), ex = Hasher(), ix = Hasher();
        for (const v of i32(x.gsn_entries_addr(), cnt)) ex.i32(v);
        P("entries", rule, cnt, ex.done());
        for (let i = 0; i < total; i++) ix.i32(x.gsn_is_entry(i));
        P("isentry", rule, ix.done());
      }
      for (const [s, d] of pairs) {
        const v = x.gsn_headless(s, d, ri);
        P("headless", rule, s, d, v >> 5, ((v >> 1) & 15) - 1, v & 1);
      }
    });
    return out;
  }

  x.gsn_set_rule(RULE_IDS[sc.rule]);
  x.gsn_start_live(sc.trafficSeed);
  P("tick", 0, hex(x.gsn_live_digest()));
  const recs = [];
  const collect = () => {
    const h = snapshot();
    while (recs.length < h[HEADER.sends]) {
      const ct = h[HEADER.sendCopyTicks];
      recs.push([h[HEADER.sendOutcome], h[HEADER.sendHops], h[HEADER.sendTicks], h[HEADER.sendTX],
        ct ? h[HEADER.sendCopiesSum] / ct : 0]);
    }
    return h;
  };
  for (let t = 1; t <= sc.ticks; t++) {
    x.gsn_step();
    collect();
    if (t % sc.every === 0 || t === sc.ticks) P("tick", t, hex(x.gsn_live_digest()));
    for (const s of sc.sends ?? []) {
      if (s.at !== t) continue;
      x.gsn_set_endpoints(s.src, s.dst);
      const ok = x.gsn_send_tracked();
      P("send", t, ok, hex(x.gsn_live_digest()));
    }
    for (const c of sc.skChanges ?? []) {
      if (c.at !== t) continue;
      x.gsn_set_sk(c.alpha, c.beta, c.F);
      P("sk", t, hex(x.gsn_live_digest()));
    }
  }
  recs.forEach((r, i) => P("rec", i, r[0], r[1], r[2], r[3], bitsHex(r[4])));
  const h = snapshot();
  P("end", h[HEADER.created], h[HEADER.delivered], h[HEADER.tx], h[HEADER.rotations], h[HEADER.agedOut], h[HEADER.custody]);
  return out;
}

const lines = readFileSync(0, "utf8").trim().split("\n").filter(Boolean);
for (const l of lines) for (const o of run(JSON.parse(l))) process.stdout.write(o + "\n");
