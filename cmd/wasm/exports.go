//go:build tinygo.wasm

// Command wasm is the engine as a WebAssembly module. The host calls the
// exported functions below; there is no other entry point.
package main

import "websim/bridge"

var b = bridge.New()

func main() {}

//go:wasmexport gsn_set_geometry
func setGeometry(world, cell int32) { b.SetGeometry(int(world), int(cell)) }

//go:wasmexport gsn_set_max_links
func setMaxLinks(v int32) { b.SetMaxLinks(int(v)) }

//go:wasmexport gsn_set_max_age
func setMaxAge(v int32) { b.SetMaxAge(int(v)) }

//go:wasmexport gsn_set_sk
func setSK(alpha, beta float64, f int32) { b.SetSK(alpha, beta, int(f)) }

//go:wasmexport gsn_set_rule
func setRule(r int32) { b.SetRule(int(r)) }

//go:wasmexport gsn_rebuild
func rebuild(want, seed int32) int32 { return int32(b.Rebuild(int(want), int(seed))) }

//go:wasmexport gsn_info
func info(which int32) float64 { return b.Info(int(which)) }

//go:wasmexport gsn_start_live
func startLive(trafficSeed int32) { b.StartLive(int64(trafficSeed)) }

//go:wasmexport gsn_step
func step() { b.Step() }

//go:wasmexport gsn_set_endpoints
func setEndpoints(src, dst int32) { b.SetEndpoints(int(src), int(dst)) }

//go:wasmexport gsn_send_tracked
func sendTracked() int32 {
	if b.SendTracked() {
		return 1
	}
	return 0
}

//go:wasmexport gsn_lane_count
func laneCount(i int32) int32 { return int32(b.LaneCount(int(i))) }

//go:wasmexport gsn_is_entry
func isEntry(i int32) int32 {
	if b.IsEntryPoint(int(i)) {
		return 1
	}
	return 0
}

//go:wasmexport gsn_positions_addr
func positionsAddr() uint32 { return b.PositionsAddr() }

//go:wasmexport gsn_clusters_addr
func clustersAddr() uint32 { return b.ClustersAddr() }

//go:wasmexport gsn_refresh_adjacency
func refreshAdjacency() int32 { return int32(b.RefreshAdjacency()) }

//go:wasmexport gsn_adj_off_addr
func adjOffAddr() uint32 { return b.AdjOffAddr() }

//go:wasmexport gsn_adj_data_addr
func adjDataAddr() uint32 { return b.AdjDataAddr() }

//go:wasmexport gsn_adj_data_len
func adjDataLen() int32 { return int32(b.AdjDataLen()) }

//go:wasmexport gsn_refresh_entries
func refreshEntries() int32 { return int32(b.RefreshEntries()) }

//go:wasmexport gsn_entries_addr
func entriesAddr() uint32 { return b.EntriesAddr() }

//go:wasmexport gsn_snapshot
func snapshot() int32 { return int32(b.Snapshot()) }

//go:wasmexport gsn_snapshot_addr
func snapshotAddr() uint32 { return b.SnapshotAddr() }

//go:wasmexport gsn_topology_digest
func topologyDigest() uint32 { return b.TopologyDigest() }

//go:wasmexport gsn_live_digest
func liveDigest() uint32 { return b.LiveDigest() }

//go:wasmexport gsn_targets_digest
func targetsDigest(dst, rule int32) uint32 { return b.TargetsDigest(int(dst), int(rule)) }

//go:wasmexport gsn_headless
func headless(src, dst, rule int32) int32 { return int32(b.Headless(int(src), int(dst), int(rule))) }

//go:wasmexport gsn_src
func src() int32 { return int32(b.Src()) }

//go:wasmexport gsn_dst
func dst() int32 { return int32(b.Dst()) }

//go:wasmexport gsn_in_range_count
func inRangeCount(i int32) int32 { return int32(b.InRangeCount(int(i))) }
