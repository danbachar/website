// The page of ../reference/original.html with its engine replaced by the
// WebAssembly build of engine/. Rendering, controls, charts and copy are the
// original's; the topology, rules and live mesh run in the module and arrive
// as a snapshot each frame.
import { loadEngine } from "./engine.js";
import { TRANSFER_WORDS } from "./layout.mjs";

const E = await loadEngine(new URL("engine.wasm", import.meta.url));

  // ---------------------------------------------------------------- algorithms
  const ALGOS = [
    { id: "flood", arm: "base", color: "--s-flood",
      name: "Epidemic flood", short: "flood", tag: "implemented",
      rule: "Relay every packet to every neighbor. The current implementation." },
    { id: "f3", arm: "f3", color: "--s-f3",
      name: "Top-3 by suitability", short: "top-3", tag: "implementable",
      rule: "Relay on the top F links by the S_k its peer advertises. Measured at F = 3; adjustable in the field." },
    { id: "hmulti", arm: "hmulti", color: "--s-hmulti",
      name: "Cluster entry, no leader", short: "no leader", tag: "implementable",
      rule: "Forward toward the recipient's cluster. Any member holding a link out of that cluster accepts it." },
    { id: "amigo", arm: "amigo", color: "--s-amigo",
      name: "Leader star over BLE", short: "amigo", tag: "needs election",
      rule: "Members hand every packet to their own leader. Leaders, elected on spare buffer, hand it across to the recipient's leader over long-range BLE." },
    { id: "amigo-udx", arm: "amigoudx", color: "--s-amigo-udx",
      name: "Leader star over UDX", short: "amigo-udx", tag: "needs election",
      rule: "The same star, but the leaders reach each other over UDX streams instead of the radio." },
    { id: "oracle", arm: "oracle", color: "--s-oracle",
      name: "Shortest path (oracle)", short: "shortest path", tag: "not implementable",
      rule: "Relay only to peers strictly closer to the recipient on the live link graph. An upper bound, not a design." },
  ];
  let current = "hmulti";
  const algoOf = id => ALGOS.find(a => a.id === id);
  const isTerritorial = () => current === "amigo" || current === "amigo-udx";
  // The tokens live on #sim, the page's scope for the simulator, not on the root.
  const simRoot = document.getElementById("sim");
  const colorOf = id => getComputedStyle(simRoot)
    .getPropertyValue(algoOf(id).color).trim();

// ---------------------------------------------------------------- field
const CELLS = [20, 25, 40, 50, 100];
const WORLDS = [200, 400, 600, 800];
let WORLD = 200, CELL = 50, GRID = WORLD / CELL;
let MAX_LINKS = 7;
let MAX_AGE = 1500;       // ticks a copy may sit in custody before it is dropped
let TICK_MS = 140;        // milliseconds of wall clock per simulated tick
// The measured f3 arm's parameters. Editable in the field; the charts below
// are always the measured values, so the field says when it has drifted.
const MEASURED_SK = { alpha: 0.5, beta: 0, theta: 0, F: 3 };
const SK = { ...MEASURED_SK };

let G = null;             // the topology as drawn: nodes, links, sender, recipient
let sends = [];           // one record per completed send, cleared when the field changes
let trackMeta = null;     // what the page knows about the tracked send, beyond the engine's counters
let recorded = 0;

const RULE_INDEX = Object.fromEntries(ALGOS.map((a, i) => [a.id, i]));

function pushParams() {
  E.setGeometry(WORLD, CELL);
  E.setMaxLinks(MAX_LINKS);
  E.setMaxAge(MAX_AGE);
  E.setSK(SK.alpha, SK.beta, SK.F);
  E.setRule(RULE_INDEX[current]);
}

function rebuild() {
  pushParams();
  const want = +document.getElementById("nodes").value;   // per cell
  const seed = +document.getElementById("seed").value;
  const info = E.rebuild(want, seed);
  G = {
    nodes: E.nodes, adj: E.adjacency(), src: E.src(), dst: E.dst(),
    meanRange: info.meanRange, meanLink: info.meanLink, boundaryShare: info.boundaryShare,
  };
  document.getElementById("nodes-out").textContent =
    `${info.nodesPerCell} · ${info.total} total` + (info.capped ? " (capped)" : "");
  document.getElementById("topo-readout").textContent =
    `${G.nodes.length} nodes in ${WORLD} m · hears ${G.meanRange.toFixed(1)} · links ${G.meanLink.toFixed(1)}`
    + ` · ${Math.round(G.boundaryShare * 100)}% on a cluster boundary`;
}

function startLive() {
  E.startLive((Math.random() * 2147483647) | 0);
  recorded = 0;
  trackMeta = null;
}

/** What distinguishes one curve from another: the rule plus its parameters. */
function configLabel() {
  const a = algoOf(current);
  return current === "f3"
    ? `${a.short} F=${SK.F} α=${SK.alpha} β=${SK.beta}`
    : a.short;
}

/** Inject the message whose endpoints we picked, and follow it. */
function sendTracked() {
  if (G.src === G.dst) return;
  const meta = { cfg: configLabel(), rule: current, pair: G.src + "->" + G.dst };
  if (E.sendTracked()) trackMeta = meta;
}

/** Turn each send the engine finished into a record for the per-pair charts. */
function syncSends(S) {
  let added = false;
  while (recorded < S.sends) {
    recorded++;
    sends.push({
      pair: trackMeta.pair, rule: trackMeta.rule, cfg: trackMeta.cfg,
      outcome: S.sendOutcome === 0 ? "arrived" : "expired",
      hops: S.sendHops >= 0 ? S.sendHops : null,
      ticks: S.sendTicks, tx: S.sendTX,
      copies: S.sendCopyTicks ? S.sendCopiesSum / S.sendCopyTicks : 0,
    });
    added = true;
  }
  if (added) renderSends();
}

  // ---------------------------------------------------------------- rendering
  const cv = document.getElementById("field");
  const ctx = cv.getContext("2d");
  let W = 1200, H = 760, DPR = 1;

  function resize() {
    const box = cv.parentElement.getBoundingClientRect();
    DPR = Math.min(2, window.devicePixelRatio || 1);
    W = Math.max(360, box.width);
    H = Math.round(W * 0.63);
    cv.width = W * DPR; cv.height = H * DPR;
    cv.style.height = H + "px";
    ctx.setTransform(DPR, 0, 0, DPR, 0, 0);
  }

  const css = v => getComputedStyle(simRoot).getPropertyValue(v).trim();

  function project(p) {
    const pad = 26;
    const k = Math.min((W - pad * 2) / WORLD, (H - pad * 2) / WORLD);
    return { x: (W - WORLD * k) / 2 + p.x * k, y: (H - WORLD * k) / 2 + p.y * k, k };
  }
  // How far into the current tick this frame is, for drawing copies in flight.
  let tickFrac = 0;

  function draw(now, S) {
    const g = G;
    ctx.clearRect(0, 0, W, H);
    const accent = colorOf(current);
    const inkSoft = css("--hairline");
    const nodeColor = css("--node"), ink = css("--ink"), oracle = css("--s-oracle");

    // cluster cells
    ctx.strokeStyle = inkSoft; ctx.lineWidth = 1;
    for (let i = 0; i <= GRID; i++) {
      const p = project({ x: i * CELL, y: 0 }), q = project({ x: i * CELL, y: WORLD });
      ctx.beginPath(); ctx.moveTo(p.x, p.y); ctx.lineTo(q.x, q.y); ctx.stroke();
      const r = project({ x: 0, y: i * CELL }), t = project({ x: WORLD, y: i * CELL });
      ctx.beginPath(); ctx.moveTo(r.x, r.y); ctx.lineTo(t.x, t.y); ctx.stroke();
    }

    // the recipient's cluster, for the rules that route to it
    const hier = current.startsWith("h");
    if (hier) {
      const dc = g.nodes[G.dst].cluster;
      const c0 = project({ x: (dc % GRID) * CELL, y: ((dc / GRID) | 0) * CELL });
      const c1 = project({ x: (dc % GRID + 1) * CELL, y: (((dc / GRID) | 0) + 1) * CELL });
      ctx.fillStyle = accent; ctx.globalAlpha = 0.08;
      ctx.fillRect(c0.x, c0.y, c1.x - c0.x, c1.y - c0.y);
      ctx.globalAlpha = 1;
    }    // links — the ones leaving a cluster are what make a member a boundary node
    for (const inter of [false, true]) {
      ctx.strokeStyle = inter ? css("--ink-3") : css("--link");
      ctx.globalAlpha = inter ? 0.5 : 1;
      ctx.lineWidth = 1;
      ctx.beginPath();
      for (let i = 0; i < g.nodes.length; i++) {
        const a = project(g.nodes[i]);
        for (const j of g.adj[i]) {
          if (j < i) continue;
          if ((g.nodes[i].cluster !== g.nodes[j].cluster) !== inter) continue;
          const b = project(g.nodes[j]);
          ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y);
        }
      }
      ctx.stroke();
    }
    ctx.globalAlpha = 1;
    // territories: each member's link to its leader, the links among leaders, the leaders
    if (isTerritorial()) {
      ctx.strokeStyle = accent; ctx.globalAlpha = 0.5; ctx.lineWidth = 1.4;
      ctx.beginPath();
      for (let i = 0; i < g.nodes.length; i++) {
        const l = S.leaderOf[i];
        if (l < 0 || l === i) continue;
        const a = project(g.nodes[i]), b = project(g.nodes[l]);
        ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y);
      }
      ctx.stroke();
      ctx.globalAlpha = 0.6; ctx.lineWidth = 1.3;
      ctx.setLineDash(current === "amigo" ? [6, 4] : [1.5, 3.5]);
      ctx.beginPath();
      for (let k = 0; k < S.overlay.length; k += 2) {
        const a = project(g.nodes[S.overlay[k]]), b = project(g.nodes[S.overlay[k + 1]]);
        ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y);
      }
      ctx.stroke();
      ctx.setLineDash([]); ctx.globalAlpha = 1;
      const home = S.leaderOf[G.dst];
      for (let i = 0; i < g.nodes.length; i++) {
        if (S.leaderOf[i] !== i) continue;
        const p = project(g.nodes[i]);
        ctx.strokeStyle = accent;
        ctx.lineWidth = i === home ? 2.6 : 1.8;
        ctx.beginPath(); ctx.arc(p.x, p.y, i === home ? 10 : 8, 0, Math.PI * 2); ctx.stroke();
      }
    }
    // every cluster's entry point(s) under the selected rule
    if (hier) {
      const one = current !== "hmulti";
      for (const i of E.entries()) {
        const p = project(g.nodes[i]);
        ctx.strokeStyle = accent;
        ctx.lineWidth = one ? 1.8 : 1.1;
        ctx.beginPath(); ctx.arc(p.x, p.y, one ? 8 : 6, 0, Math.PI * 2); ctx.stroke();
      }
    }

    // links that just formed or dropped
    for (const f of S.flashes()) {
      const u = project(g.nodes[f.a]), v = project(g.nodes[f.b]);
      const life = Math.max(0, (f.until - S.t) / 6);
      ctx.strokeStyle = ink;
      ctx.globalAlpha = 0.25 + 0.5 * life;
      ctx.lineWidth = 2;
      ctx.setLineDash(f.kind === 0 ? [3, 3] : []);   // a dropped link is gone
      ctx.beginPath(); ctx.moveTo(u.x, u.y); ctx.lineTo(v.x, v.y); ctx.stroke();
    }
    ctx.setLineDash([]); ctx.globalAlpha = 1;

    // nodes
    const tracking = S.track && !S.trackExpired;
    for (let i = 0; i < g.nodes.length; i++) {
      const p = project(g.nodes[i]);
      const held = S.held[i];
      const ours = tracking && S.trackSeen[i] === 1;
      let r = 3.1 + Math.min(3.4, held * 0.3);
      ctx.beginPath(); ctx.arc(p.x, p.y, r, 0, Math.PI * 2);
      ctx.fillStyle = ours ? accent : nodeColor;
      ctx.globalAlpha = ours ? 1 : (held ? 0.55 : 0.85);
      ctx.fill(); ctx.globalAlpha = 1;
      if (i === G.src) {
        ctx.fillStyle = ink;
        ctx.beginPath(); ctx.arc(p.x, p.y, 5.4, 0, Math.PI * 2); ctx.fill();
      } else if (i === G.dst) {
        ctx.fillStyle = oracle;
        ctx.beginPath(); ctx.arc(p.x, p.y, 5.4, 0, Math.PI * 2); ctx.fill();
        if (S.track && S.trackHops >= 0) {
          ctx.strokeStyle = oracle; ctx.lineWidth = 1.5;
          ctx.beginPath(); ctx.arc(p.x, p.y, 11, 0, Math.PI * 2); ctx.stroke();
        }
      }
    }

    // in-flight copies
    const tw = S.transferWords;
    for (let k = 0; k < S.transfers; k++) {
      const o = k * TRANSFER_WORDS;
      const u = Math.min(1, (S.t - tw[o + 4] + tickFrac) / tw[o + 5]);
      const a = project(g.nodes[tw[o]]), b = project(g.nodes[tw[o + 1]]);
      const x = a.x + (b.x - a.x) * u, y = a.y + (b.y - a.y) * u;
      const ours = S.track && tw[o + 2] === S.trackId;
      if (ours) {
        ctx.strokeStyle = accent; ctx.globalAlpha = 0.55; ctx.lineWidth = 1.4;
        ctx.beginPath(); ctx.moveTo(a.x, a.y); ctx.lineTo(x, y); ctx.stroke();
        ctx.globalAlpha = 1;
      }
      ctx.fillStyle = ours ? accent : nodeColor;
      ctx.globalAlpha = ours ? 1 : 0.5;
      ctx.beginPath();
      if (tw[o + 6] === 1) {
        const d = ours ? 4.4 : 3;
        ctx.moveTo(x, y - d); ctx.lineTo(x + d, y); ctx.lineTo(x, y + d); ctx.lineTo(x - d, y); ctx.closePath();
      } else {
        ctx.arc(x, y, ours ? 3.2 : 2, 0, Math.PI * 2);
      }
      ctx.fill();
      ctx.globalAlpha = 1;
    }
  }

  let lastTick = 0;
  function frame(now) {
    if (now - lastTick >= TICK_MS) { E.step(); lastTick = now; }
    tickFrac = Math.min(1, (now - lastTick) / TICK_MS);
    const S = E.snapshot();
    G.adj = E.adjacency();
    syncSends(S);
    paintLive(S);
    draw(now, S);
    requestAnimationFrame(frame);
  }

  function paintLive(S) {
    const T = S.track ? {
      id: S.trackId, tx: S.trackTX, hops: S.trackHops >= 0 ? S.trackHops : null,
      born: S.trackBorn, at: S.trackAt,
    } : null;
    const L = { t: S.t, rule: current, rotations: S.rotations, trackFloodTx: S.trackFloodTX, track: T };
    paintInspect(S);
    document.getElementById("m-hold").textContent = S.custody;
    const age = T ? L.t - T.born : 0;
    document.getElementById("m-hops").innerHTML = !T ? "—"
      : T.hops !== null ? T.hops + ' <span class="unit">hops · ' + T.at + ' ticks</span>'
      : '<span class="unit">in flight · ' + age + ' ticks</span>';
    document.getElementById("m-tx").textContent = T ? T.tx : "—";
    document.getElementById("m-ratio").innerHTML = !T || !T.tx ? "—"
      : current === "flood" ? '<span class="unit">is the flood</span>'
      : (T.tx / Math.max(1, L.trackFloodTx)).toFixed(2) + ' <span class="unit">× flood</span>';

    const st = document.getElementById("status");
    st.style.setProperty("--c", colorOf(current));
    if (L.rule === "hmulti" && G.boundaryShare > 0.9) {
      st.style.setProperty("--c", colorOf(current));
      st.innerHTML = `${L.track && L.track.hops !== null
          ? `Our message arrived in <b>${L.track.hops} hops</b> after ${L.track.at} ticks.`
          : L.track ? `Our message is in flight.` : `Click two nodes, then <b>Send message</b>.`}
        Note that <b>${Math.round(G.boundaryShare * 100)}% of every cluster's members hold a link out of
        it</b> at this density — cells are 50 m and the radio reaches 50 m — so "any boundary member"
        admits nearly everyone. What this rule restricts is the <em>direction</em>, not who lets a message
        in. <b>Mechanism only:</b> read rates from the charts below.`;
      return;
    }
    const rot = L.rule === "f3"
      ? `Idle links rotate under this rule (${L.rotations} so far), so a copy with no lane left is carried
         until a redraw gives it one.`
      : isTerritorial()
        ? `Leaders are re-elected as buffers fill, and the role has changed hands <b>${S.round}</b> times so
           far: a leader's custody fills fastest, so the copies it holds climb to whoever replaces it.`
        : `This rule runs with <b>the policy off</b>, so its link set is frozen, as in the measured runs.`;
    let head;
    if (!T) {
      const slow = current === "flood" || current === "f3";
      head = `Click a node for the sender, another for the recipient, then <b>Send message</b>.`
        + (slow ? ` Under this rule the mesh is congested, so expect the message to take tens of
           seconds — that delay is the result, not a stall. Compare against shortest path or cluster
           addressing, which arrive in a few seconds.` : "");
    } else if (T.hops !== null) {
      head = `Our message arrived in <b>${T.hops} hops</b> after ${T.at} ticks, on ${T.tx} transmissions.`;
    } else if (!S.trackWanted) {
      head = `Our message <b>expired</b> after ${MAX_AGE} ticks without arriving — every copy sat in
        custody with no link that would carry it onward. This is the age bound, the long-stop the app
        sets at 6 h.`;
    } else {
      const copies = S.trackCopies;
      head = `Our message is in flight: <b>${copies} ${copies === 1 ? "copy" : "copies"}</b> of it are on
        the mesh, ${T.tx} transmissions so far, ${age} ticks elapsed.`
        + (age > 60 ? ` It is queued behind other traffic — this wait is what the measured median
           latency reports.` : "");
    }
    st.innerHTML = `${head} ${rot} <b>Mechanism only:</b> this field offers a far heavier load per node
      against a 60-packet custody cap, so backlog appears sooner than in the measured runs. Read rates
      from the charts below, never from here.`;
  }
  // ---------------------------------------------------------------- control plane
  const COMMON = [
    { h: "Advertise", p: "A service UUID derived from the public key. A discovery hint, not a proof.", wire: "SERVICE UUID = SHA-256(pubkey)[0..8]" },
    { h: "ANNOUNCE", p: "A self-signed presence broadcast, verified hop-locally. It is never relayed.", wire: "pubkey · nickname · Ed25519 signature" },
    { h: "Noise handshake", p: "Both sides derive a session. The Ed25519 identity travels inside the encrypted payload.", wire: "msg1 → msg2 → session" },
    { h: "Sync on connect", p: "The only conveyance path: offer the ids we hold, the peer requests what its seen-set lacks, we convey exactly that. Sealed to the session.", wire: "offer ids → request → convey" },
  ];
  const EXTRA = {
    flood: [],
    f3: [{ h: "Advertise suitability", p: "Each ANNOUNCE carries the sender's centrality and buffer occupancy, refreshed once per announce epoch. Every decision runs on values as stale as that interval.", wire: "ANNOUNCE += C_k · ρ_k" }],
    hmulti: [
      { h: "State the cluster", p: "Each peer adds its cluster id and whether it holds an external link. Both are facts a node states about itself, so nothing is agreed with anyone.", wire: "ANNOUNCE += cluster id · boundary flag" },
      { h: "Learn the distance to each cluster", p: "Direction is not geography: a node forwards to a peer whose hop distance to the destination cluster's entry point is smaller than its own. That gradient is a distance-vector — advertise your best hop count per cluster, take min+1 from neighbors — so it costs one counter per cluster, not one per node. The oracle grants it free and never stale; a real one lags by the gossip period.", wire: "ANNOUNCE += hops to each cluster" },
      { h: "Address the recipient's cluster", p: "The sender must know which cluster the recipient is in. Nothing in the envelope carries it — the outer header holds only the recipient id — so this needs a directory, and a recipient-to-cluster directory is the location index the sender-anonymity model exists to avoid. This is the unpriced step.", wire: "recipient → cluster · NOT SOLVED" },
    ],
    amigo: [
      { h: "Advertise headroom", p: "Each ANNOUNCE carries the sender's custody occupancy, refreshed once per announce epoch. A leader is only as good as the room it has left to hold other people's traffic.", wire: "ANNOUNCE += ρ_k" },
      { h: "Elect leaders", p: "A node leads when no peer it is linked to has more headroom, and an incumbent keeps the role unless a peer beats it by 0.1. Everyone answers to the best leader they are linked to; a node with none leads itself. It is re-run every announce epoch, so the role moves as custody fills. Modelled here as the state a distributed election over ANNOUNCE would converge to — the protocol that gets there is not priced.", wire: "S = 1 − ρ · local maxima · hysteresis 0.1" },
      { h: "Pair the leaders", p: "Leaders have to reach each other without the members' help. Here two leaders within 2.4 radio ranges hold a BLE link to each other, which a phone could only keep up by scanning harder and transmitting at higher power. Every such link shares the leader's one radio with its own members.", wire: "leader ⇄ leader · 2.4 × range" },
      { h: "Address the recipient's territory", p: "The sender must know which territory the recipient is in, and a territory id follows its membership, not any place. Nothing in the envelope carries it — the outer header holds only the recipient id — so this needs a directory, and a recipient-to-territory directory is the location index the sender-anonymity model exists to avoid. This is the unpriced step.", wire: "recipient → territory · NOT SOLVED" },
    ],
    "amigo-udx": [
      { h: "Advertise headroom", p: "Each ANNOUNCE carries the sender's custody occupancy, refreshed once per announce epoch. A leader is only as good as the room it has left to hold other people's traffic.", wire: "ANNOUNCE += ρ_k" },
      { h: "Elect leaders", p: "A node leads when no peer it is linked to has more headroom, and an incumbent keeps the role unless a peer beats it by 0.1. Everyone answers to the best leader they are linked to; a node with none leads itself. It is re-run every announce epoch, so the role moves as custody fills. Modelled here as the state a distributed election over ANNOUNCE would converge to — the protocol that gets there is not priced.", wire: "S = 1 − ρ · local maxima · hysteresis 0.1" },
      { h: "Open the backbone", p: "Each leader opens UDX streams to six other leaders, the ones on either side of it in id order, so the ring stays connected as the role moves. A stream does not share the radio and carries one copy per tick. Opening it needs the other leader's address; every pair is assumed to be friends here, so the signaling is taken as granted, and each leader is now an address on the Internet.", wire: "UDX ⇄ 6 leaders · signaling assumed" },
      { h: "Address the recipient's territory", p: "The sender must know which territory the recipient is in, and a territory id follows its membership, not any place. Nothing in the envelope carries it — the outer header holds only the recipient id — so this needs a directory, and a recipient-to-territory directory is the location index the sender-anonymity model exists to avoid. This is the unpriced step.", wire: "recipient → territory · NOT SOLVED" },
    ],
    oracle: [{ h: "Know the whole graph", p: "Every node would need the live link graph of the mesh, correct at the instant it forwards. There is no protocol for this; it is a bound, not a design.", wire: "∅ — not implementable" }],
  };
  const LEDGER = {
    flood: { cost: [["Extra bytes per ANNOUNCE", "0"], ["Extra round trips", "0"], ["State agreed with others", "none"]],
      verdict: "Nothing to learn and nothing to maintain. It pays on every message instead, and stops working past 320 nodes." },
    f3: { cost: [["Extra bytes per ANNOUNCE", "2 fields"], ["Extra round trips", "0"], ["State agreed with others", "none"]],
      verdict: "Self-stated and cheap, but it loses delivery at every density until the flood itself is failing." },
    hmulti: { cost: [["Extra bytes per ANNOUNCE", "cluster id + 1 counter per cluster"], ["Extra round trips", "0"], ["State agreed with others", "none — the gradient converges by gossip"], ["Still unpriced", "recipient → cluster"]],
      verdict: "A hierarchy for two self-stated fields. It holds 0.973 delivery where the flood reaches 0.389." },
    amigo: { cost: [["Extra bytes per ANNOUNCE", "1 field (ρ)"], ["Extra round trips", "election"], ["State agreed with others", "who leads"], ["Still unpriced", "recipient → territory"]],
      verdict: "Every member's traffic funnels through one leader, and leaders share a radio with their members. In the thesis hall (392 nodes, 350 m) it delivers 0.78 at 331 relays per delivered message; the flood delivers 0.80 at 445." },
    "amigo-udx": { cost: [["Extra bytes per ANNOUNCE", "1 field (ρ)"], ["Extra round trips", "election + UDX streams"], ["State agreed with others", "who leads"], ["Still unpriced", "recipient → territory · leaders' exposure"]],
      verdict: "The backbone takes cross-territory traffic off the radio: 0.999 delivery in the thesis hall (392 nodes, 350 m) at 280 relays per delivered message, and every leader is now reachable from the Internet." },
    oracle: { cost: [["Extra bytes per ANNOUNCE", "—"], ["Extra round trips", "—"], ["State agreed with others", "the entire graph"]],
      verdict: "Not implementable. Its value is as a bound on what any directory, election or gossiped index could achieve." },
  };

  function renderControlPlane() {
    const a = algoOf(current);
    const steps = document.getElementById("steps");
    const extras = EXTRA[current];
    steps.innerHTML = "";
    const all = [...COMMON.map(s => ({ ...s, extra: false })), ...extras.map(s => ({ ...s, extra: true }))];
    all.forEach((s, i) => {
      const el = document.createElement("div");
      el.className = "step" + (s.extra ? " extra" : "");
      el.style.setProperty("--c", colorOf(current));
      el.innerHTML = `<div class="step-n">${String(i + 1).padStart(2, "0")}</div>
        <div><h3>${s.h}</h3><p>${s.p}</p><div class="wire">${s.wire}</div></div>`;
      steps.appendChild(el);
    });
    document.getElementById("cp-algo").textContent = a.name;
    // reveal in sequence
    const rows = [...steps.children];
    const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
    rows.forEach((r, i) => setTimeout(() => r.classList.add("on"), reduce ? 0 : 90 + i * 260));

    const L = LEDGER[current];
    const led = document.getElementById("ledger");
    led.innerHTML = `<div class="cost">${L.cost.map(([k, v]) =>
      `<div class="cost-row"><span>${k}</span><span>${v}</span></div>`).join("")}</div>
      <div class="verdict" style="--c:${colorOf(current)}">${L.verdict}</div>`;
  }

  // ---------------------------------------------------------------- measured
  const MEASURED = [
    ["oracle", [1.000, 0.997, 1.000, 0.996, 0.987], 10.0],
    ["hmulti", [0.993, 0.987, 0.979, 0.970, 0.961], 16.8],
    ["f3", [0.859, 0.323, 0.099, 0.028, 0.014], 288.8],
    ["flood", [0.404, 0.263, 0.152, 0.086, 0.069], 371.2],
  ];
  const LABEL = Object.fromEntries(ALGOS.map(a => [a.id, a.short]));

  function renderMeasured() {
    document.getElementById("measured-body").innerHTML = MEASURED.map(([id, ds, rpd]) => {
      const cells = ds.map((d, i) => {
        const cliff = id === "flood" && i >= 3;
        return `<td class="${cliff ? "cliff" : ""}">${d.toFixed(3)}</td>`;
      }).join("");
      return `<tr><td class="name"><i class="dot" style="background:var(${algoOf(id).color})"></i>${algoOf(id).name}</td>${cells}<td>${rpd.toFixed(1)}</td></tr>`;
    }).join("");
  }
  // ------------------------------------------------------- sends on this pair
  const SEND_METRICS = [
    { key: "hops",   title: "Hops",          unit: "hops to arrive",              fmt: v => v.toFixed(0) },
    { key: "ticks",  title: "Time",          unit: "ticks to arrive",             fmt: v => v.toFixed(0) },
    { key: "tx",     title: "Transmissions", unit: "transfers spent on it",       fmt: v => v.toFixed(0) },
    { key: "copies", title: "Copies held",   unit: "mean copies alive per tick",  fmt: v => v.toFixed(1) },
  ];
  const DASHES = ["", "4 3", "1 3", "6 2 2 2"];

  function renderSends() {
    const panel = document.getElementById("sends-panel");
    const pair = G.src + "->" + G.dst;
    const mine = sends.filter(r => r.pair === pair);
    panel.hidden = mine.length === 0;
    if (!mine.length) return;

    // one series per rule+parameters, in the order they were first used
    const series = new Map();
    for (const r of mine) {
      if (!series.has(r.cfg)) {
        const sameRule = [...series.values()].filter(v => v.rule === r.rule).length;
        series.set(r.cfg, { cfg: r.cfg, rule: r.rule, dash: DASHES[sameRule % DASHES.length], rows: [] });
      }
      series.get(r.cfg).rows.push(r);
    }
    const all = [...series.values()];
    const most = Math.max(...all.map(s => s.rows.length));
    document.getElementById("sends-hd").textContent =
      `node ${G.src} → ${G.dst} · ${mine.length} send${mine.length === 1 ? "" : "s"} · ${all.length} configuration${all.length === 1 ? "" : "s"}`;

    const host = document.getElementById("send-charts");
    host.innerHTML = "";
    const W = 320, H = 190, ML = 42, MR = 14, MT = 10, MB = 26;
    for (const m of SEND_METRICS) {
      const vals = mine.filter(r => r[m.key] !== null).map(r => r[m.key]);
      const hi = Math.max(1, ...vals) * 1.15, lo = 0;
      const px = i => most < 2 ? (ML + W - MR) / 2
        : ML + i * (W - ML - MR) / (most - 1);
      const py = v => H - MB - ((v - lo) / (hi - lo)) * (H - MB - MT);
      const g = [];
      for (let t = 0; t <= 4; t++) {
        const v = lo + (hi - lo) * t / 4, y = py(v).toFixed(1);
        g.push(`<line class="grid-line" x1="${ML}" x2="${W - MR}" y1="${y}" y2="${y}"/>`);
        g.push(`<text class="ax" x="${ML - 6}" y="${(+y + 3).toFixed(1)}" text-anchor="end">${m.fmt(v)}</text>`);
      }
      for (let i = 0; i < most; i++) {
        g.push(`<text class="ax" x="${px(i).toFixed(1)}" y="${H - MB + 14}" text-anchor="middle">${i + 1}</text>`);
      }
      for (const sv of all) {
        const col = `var(${algoOf(sv.rule).color})`;
        const pts = sv.rows.map((r, i) => ({ x: px(i), y: r[m.key] === null ? null : py(r[m.key]), r }));
        const drawn = pts.filter(p => p.y !== null);
        if (drawn.length > 1) {
          g.push(`<path d="${drawn.map((p, i) => `${i ? "L" : "M"}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join("")}" fill="none" stroke="${col}" stroke-width="2" stroke-dasharray="${sv.dash}" stroke-linejoin="round"/>`);
        }
        for (const p of drawn) {
          g.push(`<circle cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="3.2" fill="${p.r.outcome === "expired" ? "var(--surface)" : col}" stroke="${col}" stroke-width="2"/>`);
        }
      }
      const wrap = document.createElement("div");
      wrap.className = "chart";
      wrap.innerHTML = `<h3>${m.title}</h3>
        <svg viewBox="0 0 ${W} ${H}" role="img" aria-label="${m.title} for each send on this pair, one line per rule and parameter set."> ${g.join("")}</svg>
        <div class="unit-cap">${m.unit} · by send</div>`;
      host.appendChild(wrap);
    }

    document.getElementById("send-keys").innerHTML = all.map(sv => {
      const exp = sv.rows.filter(r => r.outcome === "expired").length;
      return `<div class="key"><i class="dot" style="background:var(${algoOf(sv.rule).color})"></i>
        <div><b>${sv.cfg}</b><span>${sv.rows.length} send${sv.rows.length === 1 ? "" : "s"}${exp ? `, ${exp} expired (hollow)` : ""}</span></div></div>`;
    }).join("");
  }
  // ---------------------------------------------------------------- curves
  const NODES = [160, 240, 320, 400, 480];
  const CURVE_KEY = { flood: "base" };            // the sweep names the flood arm "base"
  const curve = id => CURVES[CURVE_KEY[id] || id];
  const MEASURED_ALGOS = () => ALGOS.filter(a => curve(a.id));
  const CURVES = {"base":{"160":{"delivery":[0.4043,0.2315,0.6054],"latency":[327.0,31.0,686.0],"rpd":[116.3825,102.3929,126.0902]},"240":{"delivery":[0.2632,0.0974,0.3744],"latency":[126.5,8.0,407.0],"rpd":[185.6113,169.3584,203.7218]},"320":{"delivery":[0.1519,0.0739,0.2108],"latency":[36.5,6.0,128.0],"rpd":[251.469,220.4952,268.8879]},"400":{"delivery":[0.0861,0.0244,0.1757],"latency":[8.7,5.0,22.0],"rpd":[309.3293,278.8397,344.9131]},"480":{"delivery":[0.0687,0.0314,0.1211],"latency":[7.9,6.0,12.0],"rpd":[371.2182,337.3445,400.4634]}},"f3":{"160":{"delivery":[0.859,0.4884,0.9104],"latency":[13.9,11.0,16.0],"rpd":[47.5479,45.2889,63.3476]},"240":{"delivery":[0.3234,0.1166,0.7643],"latency":[10.1,7.0,15.0],"rpd":[110.0497,73.1466,141.3707]},"320":{"delivery":[0.0986,0.0303,0.1961],"latency":[7.3,5.0,10.0],"rpd":[192.5918,162.5797,211.6163]},"400":{"delivery":[0.0281,0.0102,0.0586],"latency":[5.5,3.0,7.0],"rpd":[249.3796,196.7836,285.356]},"480":{"delivery":[0.0142,0.0053,0.0273],"latency":[4.6,3.0,7.0],"rpd":[288.8092,260.9382,346.805]}},"hmulti":{"160":{"delivery":[0.9927,0.8705,0.9988],"latency":[4.0,4.0,5.0],"rpd":[10.709,10.1479,11.3011]},"240":{"delivery":[0.9875,0.9706,0.9939],"latency":[5.0,5.0,5.0],"rpd":[12.6783,11.9757,13.1639]},"320":{"delivery":[0.979,0.9637,0.9871],"latency":[5.0,5.0,5.0],"rpd":[14.2675,13.7211,15.0141]},"400":{"delivery":[0.9701,0.95,0.98],"latency":[5.0,5.0,5.0],"rpd":[15.6557,14.9958,16.3474]},"480":{"delivery":[0.961,0.9146,0.9744],"latency":[5.0,5.0,5.0],"rpd":[16.8216,15.8987,17.5651]}},"oracle":{"160":{"delivery":[0.9998,0.9996,0.9999],"latency":[4.0,4.0,4.0],"rpd":[8.1568,7.8052,8.5957]},"240":{"delivery":[0.9968,0.9366,0.9999],"latency":[4.0,4.0,4.0],"rpd":[8.8995,8.5295,9.2831]},"320":{"delivery":[0.9998,0.9996,0.9999],"latency":[4.0,4.0,4.0],"rpd":[9.4382,9.1962,9.7807]},"400":{"delivery":[0.9957,0.8028,0.9999],"latency":[4.0,4.0,4.0],"rpd":[9.7258,9.3939,10.031]},"480":{"delivery":[0.9869,0.4889,0.9999],"latency":[4.0,4.0,4.0],"rpd":[10.0054,9.7902,10.7938]}}};   // [mean, min, max] over the fifty paired runs

  const CHARTS = [
    { key: "delivery", title: "Delivery", unit: "fraction of messages delivered",
      scale: "linear", domain: [0, 1], ticks: [0, 0.25, 0.5, 0.75, 1],
      fmt: v => v.toFixed(3), tick: v => v.toFixed(2), hi: true,
      note: "The flood fails at every density tested, worst where it is densest. Cluster addressing stays within 4 points of an omniscient router throughout." },
    { key: "latency", title: "Median latency", unit: "seconds · log scale",
      scale: "log", domain: [3, 800], ticks: [10, 100],
      fmt: v => v < 10 ? v.toFixed(1) + " s" : Math.round(v) + " s", tick: v => v + " s", hi: false,
      note: "Read against delivery: the flood's median falls as density rises only because the slow messages stop arriving at all." },
    { key: "rpd", title: "Retransmissions", unit: "relays per delivered message · log scale",
      scale: "log", domain: [6, 460], ticks: [10, 30, 100, 300],
      fmt: v => v.toFixed(1), tick: v => String(v), hi: false,
      note: "Directed forwarding is near-flat in node count. The flood rises with it, slightly faster than proportionally." },
  ];

  function renderCharts() {
    const host = document.getElementById("charts");
    host.innerHTML = "";
    const W = 320, H = 200, ML = 42, MR = 14, MT = 10, MB = 28;
    const px = i => ML + i * (W - ML - MR) / (NODES.length - 1);

    for (const c of CHARTS) {
      const lg = c.scale === "log";
      const lo = lg ? Math.log10(c.domain[0]) : c.domain[0];
      const hiD = lg ? Math.log10(c.domain[1]) : c.domain[1];
      const py = v => {
        const t = ((lg ? Math.log10(Math.max(v, c.domain[0])) : v) - lo) / (hiD - lo);
        return H - MB - t * (H - MB - MT);
      };

      const g = [];
      for (const t of c.ticks) {
        const y = py(t).toFixed(1);
        g.push(`<line class="grid-line" x1="${ML}" x2="${W - MR}" y1="${y}" y2="${y}"/>`);
        g.push(`<text class="ax" x="${ML - 6}" y="${(py(t) + 3).toFixed(1)}" text-anchor="end">${c.tick(t)}</text>`);
      }
      NODES.forEach((n, i) => {
        g.push(`<text class="ax" x="${px(i).toFixed(1)}" y="${H - MB + 15}" text-anchor="middle">${n}</text>`);
      });

      for (const a of MEASURED_ALGOS()) {
        const col = `var(${a.color})`;
        const pts = NODES.map((n, i) => {
          const st = curve(a.id)[n][c.key];
          return { x: px(i), y: py(st[0]), a: py(st[1]), b: py(st[2]) };
        });
        for (const p of pts) {
          if (Math.abs(p.a - p.b) > 1.5) {
            g.push(`<line x1="${p.x.toFixed(1)}" x2="${p.x.toFixed(1)}" y1="${p.a.toFixed(1)}" y2="${p.b.toFixed(1)}" stroke="${col}" stroke-width="1.5" opacity="0.35"/>`);
          }
        }
        g.push(`<path d="${pts.map((p, i) => `${i ? "L" : "M"}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join("")}" fill="none" stroke="${col}" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>`);
        for (const p of pts) {
          g.push(`<circle cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="3.2" fill="${col}" stroke="var(--surface)" stroke-width="2"/>`);
        }
        if (a.id === "flood" || a.id === "oracle") {
          const L = pts[pts.length - 1];
          const dy = (c.key === "delivery") === (a.id === "flood") ? 14 : -9;
          g.push(`<text class="ax" x="${(L.x - 4).toFixed(1)}" y="${(L.y + dy).toFixed(1)}" text-anchor="end" fill="${col}" style="font-weight:600">${a.id === "flood" ? "flood" : "shortest path"}</text>`);
        }
      }

      const wrap = document.createElement("div");
      wrap.className = "chart";
      wrap.innerHTML = `<h3>${c.title}</h3><p>${c.note}</p>
        <svg viewBox="0 0 ${W} ${H}" role="img" aria-label="${c.title} against node count, one line per forwarding rule; the table below lists the same values.">
          ${g.join("")}
          <line class="cross" x1="0" x2="0" y1="${MT}" y2="${H - MB}" stroke="var(--ink-3)" stroke-width="1" stroke-dasharray="2 2" opacity="0"/>
          <rect x="0" y="0" width="${W}" height="${H}" fill="transparent"/>
        </svg>
        <div class="unit-cap">${c.unit}</div>
        <div class="tip"></div>`;
      host.appendChild(wrap);

      const svg = wrap.querySelector("svg"), tip = wrap.querySelector(".tip"),
            cross = wrap.querySelector(".cross");
      svg.addEventListener("mousemove", ev => {
        const r = svg.getBoundingClientRect();
        const xv = (ev.clientX - r.left) / r.width * W;
        let i = 0, best = Infinity;
        NODES.forEach((n, k) => { const d = Math.abs(px(k) - xv); if (d < best) { best = d; i = k; } });
        cross.setAttribute("x1", px(i)); cross.setAttribute("x2", px(i));
        cross.setAttribute("opacity", "0.55");
        const rows = MEASURED_ALGOS().map(a => ({ a, v: curve(a.id)[NODES[i]][c.key][0] }))
          .sort((u, w) => c.hi ? w.v - u.v : u.v - w.v);
        tip.innerHTML = `<div class="tip-hd">${NODES[i]} NODES</div>` + rows.map(r =>
          `<div class="tip-row"><i class="dot" style="background:var(${r.a.color})"></i>${LABEL[r.a.id]}<b>${c.fmt(r.v)}</b></div>`).join("");
        tip.style.opacity = "1";
        tip.style.left = Math.min(Math.max(0, (px(i) / W) * r.width - 76), r.width - 152) + "px";
        tip.style.top = (svg.offsetTop + 6) + "px";
      });
      svg.addEventListener("mouseleave", () => {
        tip.style.opacity = "0"; cross.setAttribute("opacity", "0");
      });
    }

    document.getElementById("keys").innerHTML = ALGOS.map(a =>
      `<div class="key"><i class="dot" style="background:var(${a.color})"></i>
        <div><b>${a.name}</b><span>${a.rule}${curve(a.id) ? "" : " No curve here: the runs behind these charts do not include it."}</span></div></div>`).join("");
  }
  // ---------------------------------------------------------------- wiring
  function renderAlgoList(host) {
    host.innerHTML = "";
    for (const a of ALGOS) {
      const b = document.createElement("button");
      b.className = "algo";
      b.style.setProperty("--c", `var(${a.color})`);
      b.setAttribute("aria-pressed", String(a.id === current));
      b.innerHTML = `<i class="swatch"></i>
        <span><span class="algo-name">${a.name}<span class="tag">${a.tag}</span></span>
        <span class="algo-rule">${a.rule}</span></span>`;
      b.addEventListener("click", (ev) => {
        select(a.id);
        if (a.id === "f3") openSk(ev); else closeSk();
      });
      host.appendChild(b);
    }
  }
  // The score parameters open as a small dialog at the point the Top-3 rule was clicked.
  const skPop = document.getElementById("sk-pop");
  function closeSk() {
    if (skPop.hidden) return;
    skPop.hidden = true;
    document.removeEventListener("pointerdown", onSkOutside, true);
    document.removeEventListener("keydown", onSkKey);
  }
  function onSkOutside(ev) {
    if (!skPop.contains(ev.target) && !ev.target.closest(".algo")) closeSk();
  }
  function onSkKey(ev) { if (ev.key === "Escape") closeSk(); }
  function openSk(ev) {
    const root = simRoot.getBoundingClientRect();
    const x = ev.clientX - root.left, y = ev.clientY - root.top;
    skPop.hidden = false;
    const w = skPop.offsetWidth, h = skPop.offsetHeight;
    const left = Math.max(8, Math.min(x, root.width - w - 8));
    const top = Math.max(0, Math.min(y, root.height - h - 8));
    skPop.style.left = left + "px";
    skPop.style.top = top + "px";
    skPop.style.transformOrigin = `${x - left}px ${y - top}px`;   // grows out of the click point
    skPop.style.animation = "none"; void skPop.offsetWidth; skPop.style.animation = "";
    document.addEventListener("pointerdown", onSkOutside, true);
    document.addEventListener("keydown", onSkKey);
  }
  document.getElementById("sk-close").addEventListener("click", closeSk);

  function select(id) {
    current = id;
    if (id !== "f3") closeSk();
    for (const host of [document.getElementById("algos"), document.getElementById("algos2")]) {
      [...host.children].forEach((b, i) => b.setAttribute("aria-pressed", String(ALGOS[i].id === id)));
    }
    document.getElementById("lg-hold").style.background = colorOf(id);
    const ENTRY_LABEL = {
      hmulti:    "boundary members — any of them is a way in",
      amigo:     "territory leader — every member answers to one",
      "amigo-udx": "territory leader — every member answers to one",
    };
    const entryLegend = document.getElementById("lg-entry");
    const interLegend = document.getElementById("lg-inter");
    entryLegend.hidden = interLegend.hidden = !ENTRY_LABEL[id];
    const overLegend = document.getElementById("lg-over");
    overLegend.hidden = !isTerritorial();
    interLegend.querySelector("i").className = isTerritorial() ? "seg solid" : "seg";
    document.getElementById("lg-inter-text").textContent = isTerritorial() ? "member's link to its leader" : "link leaving a cluster";
    if (isTerritorial()) {
      interLegend.style.setProperty("--c", colorOf(id));
      overLegend.style.setProperty("--c", colorOf(id));
      document.getElementById("lg-over-text").textContent =
        id === "amigo" ? "leader-to-leader link, long-range BLE" : "leader-to-leader UDX stream";
    }
    for (const el of ["lg-form", "lg-drop"]) {
      const node = document.getElementById(el);
      node.hidden = id !== "f3";          // only the policy arm rotates links
      node.style.setProperty("--c", css("--ink"));
    }
    if (ENTRY_LABEL[id]) {
      entryLegend.style.setProperty("--c", colorOf(id));
      document.getElementById("lg-entry-text").textContent = ENTRY_LABEL[id];
    }
    restart(false);      // same field, same pair: keep what we have measured
    paintSk();
    renderControlPlane();
  }
  /** Keep the score controls honest about what they affect and when they drift
   *  from the parameters the measured curves were run at. */
  function paintSk() {
    const drift = SK.alpha !== MEASURED_SK.alpha || SK.beta !== MEASURED_SK.beta
               || SK.F !== MEASURED_SK.F;
    const applies = current === "f3";
    document.getElementById("sk-group").classList.toggle("off", !applies);
    document.getElementById("sk-scope").textContent = applies
      ? "— ranks this rule's lanes" : "— only top-3 reads these";
    const inert = SK.beta > 0
      ? ` &#946; only separates peers whose custody differs, so while buffers sit near empty it changes
          no ranking at all.` : "";
    document.getElementById("sk-note").innerHTML = (drift
      ? `Field is running &#945; ${SK.alpha}, &#946; ${SK.beta}, F ${SK.F}. The measured curves below
         are &#945; 0.5, &#946; 0, F 3 — they do not move with these sliders.`
      : `&#952; stays 0 with the gate inclusive, so every peer passes and only the ranking decides.`)
      + inert;
  }
  for (const [id, key, fmt] of [["p-alpha","alpha",v=>v.toFixed(2)],
                                ["p-beta","beta",v=>v.toFixed(2)],
                                ["p-fanout","F",v=>String(v)]]) {
    document.getElementById(id).addEventListener("input", e => {
      SK[key] = +e.target.value;
      document.getElementById(id + "-out").textContent = fmt(+e.target.value);
      E.setSK(SK.alpha, SK.beta, SK.F);   // lane sets are derived; the engine drops what it cached
      paintSk();
    });
  }

  document.getElementById("p-tick").addEventListener("input", e => {
    // Only new transfers pick up the new tick length; ones already in flight
    // keep the duration they were started with, so nothing jumps.
    TICK_MS = +e.target.value;
    document.getElementById("p-tick-out").textContent = TICK_MS + " ms";
  });
  // One control: a cluster's side and the radio range are the same length.
  const applyGeometry = () => {
    GRID = WORLD / CELL;
    document.getElementById("p-cell-out").textContent = `${CELL} m · ${GRID}×${GRID}`;
    document.getElementById("p-world-out").textContent = `${WORLD} m`;
    restart();
  };
  document.getElementById("p-cell").addEventListener("input", e => {
    CELL = CELLS[+e.target.value]; applyGeometry();
  });
  document.getElementById("p-world").addEventListener("input", e => {
    WORLD = WORLDS[+e.target.value]; applyGeometry();
  });
  document.getElementById("p-age").addEventListener("input", e => {
    // Applies to the running mesh: the next sweep uses the new bound, so a
    // shorter one reaps the backlog that is already stuck.
    MAX_AGE = +e.target.value;
    E.setMaxAge(MAX_AGE);
    document.getElementById("p-age-out").textContent = MAX_AGE + " ticks";
  });
  document.getElementById("p-links").addEventListener("input", e => {
    MAX_LINKS = +e.target.value;
    document.getElementById("p-links-out").textContent = MAX_LINKS + " per node";
    restart();
  });
  /** Restart the mesh. Changing the FIELD invalidates the comparison and clears
   *  it; changing the RULE is the comparison, so those samples are kept. */
  const restart = (clear = true) => {
    if (clear) sends = [];
    rebuild(); startLive(); renderSends();
  };
  document.getElementById("nodes").addEventListener("input", () => restart());
  document.getElementById("seed").addEventListener("input", e => {
    document.getElementById("seed-out").textContent = e.target.value; restart();
  });
  /** Nearest node to a canvas point, if the click was close enough. */
  function nodeAt(ev) {
    const r = cv.getBoundingClientRect();
    const x = (ev.clientX - r.left) * (W / r.width), y = (ev.clientY - r.top) * (W / r.width);
    let best = -1, bestD = 14 * 14;
    for (let i = 0; i < G.nodes.length; i++) {
      const p = project(G.nodes[i]);
      const d = (p.x - x) ** 2 + (p.y - y) ** 2;
      if (d < bestD) { bestD = d; best = i; }
    }
    return best;
  }
  let picking = "src", inspect = null;
  cv.addEventListener("click", ev => {
    const i = nodeAt(ev);
    if (i < 0) return;
    inspect = i;
    if (picking === "src") { G.src = i; picking = "dst"; }
    else if (i !== G.src) { G.dst = i; picking = "src"; }
    E.setEndpoints(G.src, G.dst);   // changing the endpoints drops the tracked send
  });

  /** What the clicked node can hear, what it is connected to, and what it holds. */
  function paintInspect(S) {
    const el = document.getElementById("inspect");
    if (inspect === null || !G.nodes[inspect]) { el.hidden = true; return; }
    el.hidden = false;
    const i = inspect;
    const inRange = E.inRangeCount(i), linked = G.adj[i].length;
    const role = i === G.src ? "sender" : i === G.dst ? "recipient" : "relay";
    const held = S.held[i];
    const lanes = E.laneCount(i);
    const lead = S.leaderOf[i];
    const entry = current.startsWith("h") && E.isEntry(i) ? " · cluster entry point"
      : isTerritorial() && lead >= 0
        ? (lead === i ? ` · leads territory ${S.territory[i]}` : ` · territory ${S.territory[i]}, leader node ${lead}`)
        : "";
    el.innerHTML = `<b>Node ${i}</b> · ${role} · cluster ${G.nodes[i].cluster}${entry} —
      hears <b>${inRange}</b> peers, connected to <b>${linked}</b> of them
      (cap ${MAX_LINKS}), forwards on ${lanes} of those for the current recipient,
      holds ${held} ${held === 1 ? "packet" : "packets"}.`;
  }

  document.getElementById("send").addEventListener("click", sendTracked);
  document.getElementById("replay").addEventListener("click", () => {
    const n = G.nodes.length;
    G.src = (Math.random() * n) | 0;
    do { G.dst = (Math.random() * n) | 0; } while (G.dst === G.src);
    picking = "src";
    E.setEndpoints(G.src, G.dst);
  });

  const tabs = { data: document.getElementById("tab-data"), ctrl: document.getElementById("tab-ctrl") };
  const views = { data: document.getElementById("view-data"), ctrl: document.getElementById("view-ctrl") };
  function showTab(which) {
    for (const k of ["data", "ctrl"]) {
      tabs[k].setAttribute("aria-selected", String(k === which));
      views[k].hidden = k !== which;
    }
    if (which === "ctrl") renderControlPlane(); else resize();
  }
  tabs.data.addEventListener("click", () => showTab("data"));
  tabs.ctrl.addEventListener("click", () => showTab("ctrl"));

  window.addEventListener("resize", resize);
  matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => select(current));

  renderAlgoList(document.getElementById("algos"));
  renderAlgoList(document.getElementById("algos2"));
  renderMeasured();
  renderCharts();
  resize();
  rebuild();
  startLive();
  select("hmulti");   // opens on a rule that demonstrates in seconds
  requestAnimationFrame(frame);
