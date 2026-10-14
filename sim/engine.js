// Loads the engine module and presents it to the page.
//
// The module is the TinyGo build of engine/ (see ../README.md): target
// wasm-unknown, so it imports nothing, and _initialize sets up its runtime
// before the first call.
import { HEADER, TRANSFER_WORDS, FLASH_WORDS, INFO } from "./layout.mjs";

async function instantiate(url) {
  const { instance } = await WebAssembly.instantiateStreaming(fetch(url), {});
  instance.exports._initialize();
  return instance.exports;
}

export async function loadEngine(url) {
  const x = await instantiate(url);
  const i32 = (addr, len) => new Int32Array(x.memory.buffer, addr, len);
  let adj = [], adjVer = -1;

  const E = {
    nodes: [],
    setGeometry: (world, cell) => x.gsn_set_geometry(world, cell),
    setMaxLinks: (v) => x.gsn_set_max_links(v),
    setMaxAge: (v) => x.gsn_set_max_age(v),
    setSK: (alpha, beta, f) => x.gsn_set_sk(alpha, beta, f),
    setRule: (r) => x.gsn_set_rule(r),
    startLive: (trafficSeed) => x.gsn_start_live(trafficSeed),
    step: () => x.gsn_step(),
    setEndpoints: (src, dst) => x.gsn_set_endpoints(src, dst),
    sendTracked: () => x.gsn_send_tracked() === 1,
    laneCount: (i) => x.gsn_lane_count(i),
    isEntry: (i) => x.gsn_is_entry(i) === 1,
    inRangeCount: (i) => x.gsn_in_range_count(i),
    src: () => x.gsn_src(),
    dst: () => x.gsn_dst(),

    // Draws a fresh topology and returns what the readout needs.
    rebuild(want, seed) {
      const total = x.gsn_rebuild(want, seed);
      const pos = new Float64Array(x.memory.buffer, x.gsn_positions_addr(), total * 2);
      const clu = i32(x.gsn_clusters_addr(), total);
      this.nodes = Array.from({ length: total }, (_, i) => ({ x: pos[2 * i], y: pos[2 * i + 1], cluster: clu[i] }));
      adjVer = -1;
      return {
        nodesPerCell: x.gsn_info(INFO.nodesPerCell), total: x.gsn_info(INFO.total),
        capped: x.gsn_info(INFO.capped) === 1, meanRange: x.gsn_info(INFO.meanRange),
        meanLink: x.gsn_info(INFO.meanLink), boundaryShare: x.gsn_info(INFO.boundaryShare),
      };
    },

    // Each node's links as arrays; rebuilt only when the link set changed.
    adjacency() {
      const ver = x.gsn_refresh_adjacency();
      if (ver !== adjVer) {
        const n = this.nodes.length;
        const off = i32(x.gsn_adj_off_addr(), n + 1);
        const data = i32(x.gsn_adj_data_addr(), x.gsn_adj_data_len());
        adj = Array.from({ length: n }, (_, i) => Array.from(data.subarray(off[i], off[i + 1])));
        adjVer = ver;
      }
      return adj;
    },

    // Every cluster's entry point(s) under the current rule. Copied out of the
    // module's memory: a later call into the module can grow that memory, which
    // detaches every view of the old one.
    entries() {
      const count = x.gsn_refresh_entries();
      return i32(x.gsn_entries_addr(), count).slice();
    },

    // The live state, copied out of the module's memory for the same reason.
    snapshot() {
      const len = x.gsn_snapshot();
      const w = i32(x.gsn_snapshot_addr(), len).slice();
      const n = w[HEADER.nodes], nTr = w[HEADER.transfers], nFl = w[HEADER.flash], nOv = w[HEADER.overlay];
      const S = { words: w };
      for (const [name, idx] of Object.entries(HEADER)) S[name] = w[idx];
      let at = HEADER.words;
      S.held = w.subarray(at, at + n); at += n;
      S.trackSeen = w.subarray(at, at + n); at += n;
      S.leaderOf = w.subarray(at, at + n); at += n;
      S.territory = w.subarray(at, at + n); at += n;
      S.transferWords = w.subarray(at, at + nTr * TRANSFER_WORDS); at += nTr * TRANSFER_WORDS;
      const fw = w.subarray(at, at + nFl * FLASH_WORDS); at += nFl * FLASH_WORDS;
      S.overlay = w.subarray(at, at + nOv * 2);
      S.flashes = () => Array.from({ length: nFl }, (_, k) => ({
        a: fw[k * FLASH_WORDS], b: fw[k * FLASH_WORDS + 1], until: fw[k * FLASH_WORDS + 2], kind: fw[k * FLASH_WORDS + 3],
      }));
      return S;
    },
  };
  return E;
}
