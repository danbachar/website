// Package bridge adapts the engine to a host that can only exchange numbers
// and read the module's linear memory: configuration goes in as scalars, state
// comes out as flat arrays whose addresses the host asks for.
package bridge

import (
	"unsafe"

	"websim/engine"
)

// Snapshot header word indices. The host reads the same layout, so these are
// part of the interface.
const (
	HT = iota
	HCreated
	HDelivered
	HTX
	HRotations
	HAgedOut
	HCustody
	HGraphVer
	HSrc
	HDst
	HTrack // 1 when a tracked send exists
	HTrackID
	HTrackTX
	HTrackHops // -1 until it arrives
	HTrackBorn
	HTrackDone
	HTrackExpired
	HTrackCopiesSum
	HTrackTicks
	HTrackAt
	HTrackDoneAt
	HTrackFloodTX
	HTrackWanted // 1 while some node still wants the message
	HTrackCopies // copies alive: held plus in flight
	HSends       // finished tracked sends so far
	HSendOutcome // the latest one; -1 if none
	HSendHops
	HSendTicks
	HSendTX
	HSendCopiesSum
	HSendCopyTicks
	HTransfers // count, then Transfers*7 words after the per-node arrays
	HFlash     // count, then Flash*4 words
	HOverlay   // leader-to-leader links, then 2 words each after the flashes
	HLeaders   // how many nodes lead a territory
	HRound     // election rounds that changed who leads
	HNodes
	HeaderWords
)

// After the header: Held[nodes], TrackSeen[nodes], LeaderOf[nodes] (-1 for none),
// Territory[nodes] (-1 for none), then the transfers (from, to, id, ttl, born,
// dur, kind), the flashes (a, b, until, kind) and the leader links (a, b).
const (
	TransferWords = 7
	FlashWords    = 4
)

// Info selectors for Rebuild's results.
const (
	InfoNodesPerCell = iota
	InfoTotal
	InfoCapped
	InfoMeanRange
	InfoMeanLink
	InfoBoundaryShare
)

type Bridge struct {
	E *engine.Engine

	pos      []float64
	clusters []int32
	snap     []int32
	adjOff   []int32
	adjData  []int32
	adjVer   int
	entries  []int32
	entVer   int
	entRule  engine.Rule
	info     [6]float64
}

func New() *Bridge { return &Bridge{E: engine.New(), adjVer: -1, entVer: -1} }

func (b *Bridge) SetGeometry(world, cell int) { b.E.Geo = engine.Geometry{World: world, Cell: cell} }
func (b *Bridge) SetMaxLinks(v int)           { b.E.MaxLinks = v }
func (b *Bridge) SetMaxAge(v int)             { b.E.MaxAge = v }
func (b *Bridge) SetSK(a, be float64, f int)  { b.E.SetSK(a, be, f) }
func (b *Bridge) SetRule(r int)               { b.E.Rule = engine.Rule(r) }
func (b *Bridge) StartLive(trafficSeed int64) { b.E.StartLive(trafficSeed) }
func (b *Bridge) Step()                       { b.E.Step() }
func (b *Bridge) SetEndpoints(src, dst int)   { b.E.SetEndpoints(src, dst) }
func (b *Bridge) SendTracked() bool           { return b.E.SendTracked() }
func (b *Bridge) LaneCount(i int) int         { return b.E.LaneCount(i) }
func (b *Bridge) IsEntryPoint(i int) bool     { return b.E.IsEntryPoint(i) }
func (b *Bridge) LiveDigest() uint32          { return b.E.LiveDigest() }
func (b *Bridge) TopologyDigest() uint32      { return b.E.TopologyDigest() }
func (b *Bridge) Info(which int) float64      { return b.info[which] }

// Rebuild draws a fresh topology and returns the node count.
func (b *Bridge) Rebuild(want, seed int) int {
	r := b.E.Rebuild(want, seed)
	b.info = [6]float64{float64(r.NodesPerCell), float64(r.Total), 0, r.MeanRange, r.MeanLink, r.BoundaryShare}
	if r.Capped {
		b.info[InfoCapped] = 1
	}
	g := b.E.G
	b.pos = b.pos[:0]
	b.clusters = b.clusters[:0]
	for _, n := range g.Nodes {
		b.pos = append(b.pos, n.X, n.Y)
		b.clusters = append(b.clusters, int32(n.Cluster))
	}
	b.adjVer, b.entVer = -1, -1
	return r.Total
}

// Positions are x,y pairs; Clusters one id per node.
func (b *Bridge) PositionsAddr() uint32 { return addr(b.pos) }
func (b *Bridge) ClustersAddr() uint32  { return addr(b.clusters) }

// Adjacency is CSR: node i's links are AdjData[AdjOff[i]:AdjOff[i+1]].
// RefreshAdjacency rebuilds it only when the link set changed and returns the
// graph version it now reflects.
func (b *Bridge) RefreshAdjacency() int {
	if v := b.E.GraphVersion(); v != b.adjVer {
		b.adjOff = b.adjOff[:0]
		b.adjData = b.adjData[:0]
		for _, links := range b.E.G.Adj {
			b.adjOff = append(b.adjOff, int32(len(b.adjData)))
			for _, j := range links {
				b.adjData = append(b.adjData, int32(j))
			}
		}
		b.adjOff = append(b.adjOff, int32(len(b.adjData)))
		b.adjVer = v
	}
	return b.adjVer
}

func (b *Bridge) AdjOffAddr() uint32  { return addr(b.adjOff) }
func (b *Bridge) AdjDataAddr() uint32 { return addr(b.adjData) }
func (b *Bridge) AdjDataLen() int     { return len(b.adjData) }

// RefreshEntries lists every cluster's entry point(s) under the current rule,
// recomputing only when the links or the rule changed. It returns the count.
func (b *Bridge) RefreshEntries() int {
	if v := b.E.GraphVersion(); v != b.entVer || b.E.Rule != b.entRule {
		b.entries = b.entries[:0]
		for _, i := range b.E.EntryPoints() {
			b.entries = append(b.entries, int32(i))
		}
		b.entVer, b.entRule = v, b.E.Rule
	}
	return len(b.entries)
}

func (b *Bridge) EntriesAddr() uint32 { return addr(b.entries) }

// Snapshot writes the live state into the snapshot buffer and returns its
// length in words.
func (b *Bridge) Snapshot() int {
	e, L, n := b.E, b.E.L, len(b.E.G.Nodes)
	nTr, nFl, ov := len(L.Transfers), L.FlashCount(), e.OverlayLinks()
	words := HeaderWords + 4*n + nTr*TransferWords + nFl*FlashWords + 2*len(ov)
	if cap(b.snap) < words {
		b.snap = make([]int32, words, words+words/4)
	}
	s := b.snap[:words]
	for i := range s[:HeaderWords] {
		s[i] = 0
	}
	set := func(h, v int) { s[h] = int32(v) }
	set(HT, L.T)
	set(HCreated, L.Created)
	set(HDelivered, L.Delivered)
	set(HTX, L.TX)
	set(HRotations, L.Rotations)
	set(HAgedOut, L.AgedOut)
	set(HCustody, e.CustodyTotal())
	set(HGraphVer, e.GraphVersion())
	set(HSrc, e.G.Src)
	set(HDst, e.G.Dst)
	set(HTrackHops, -1)
	if t := L.Track; t != nil {
		set(HTrack, 1)
		set(HTrackID, t.ID)
		set(HTrackTX, t.TX)
		set(HTrackHops, t.Hops)
		set(HTrackBorn, t.Born)
		set(HTrackDone, b2i(t.Done))
		set(HTrackExpired, b2i(t.Expired))
		set(HTrackCopiesSum, t.CopiesSum)
		set(HTrackTicks, t.Ticks)
		set(HTrackAt, t.At)
		set(HTrackDoneAt, t.DoneAt)
		set(HTrackFloodTX, L.TrackFloodTX)
		set(HTrackWanted, b2i(L.Wanted(t.ID)))
		set(HTrackCopies, L.CopiesAlive(t.ID))
	}
	set(HSends, len(L.Sends))
	set(HSendOutcome, -1)
	if k := len(L.Sends); k > 0 {
		r := L.Sends[k-1]
		set(HSendOutcome, r.Outcome)
		set(HSendHops, r.Hops)
		set(HSendTicks, r.Ticks)
		set(HSendTX, r.TX)
		set(HSendCopiesSum, r.CopiesSum)
		set(HSendCopyTicks, r.CopyTks)
	}
	set(HTransfers, nTr)
	set(HFlash, nFl)
	set(HOverlay, len(ov))
	leaders := 0
	for i := 0; i < n; i++ {
		if e.IsLeader(i) {
			leaders++
		}
	}
	set(HLeaders, leaders)
	set(HRound, e.TerritoryRound())
	set(HNodes, n)

	at := HeaderWords
	for i := 0; i < n; i++ {
		s[at+i] = int32(L.HeldCount(i))
	}
	at += n
	for i := 0; i < n; i++ {
		if L.Track != nil && L.Seen(i, L.Track.ID) {
			s[at+i] = 1
		} else {
			s[at+i] = 0
		}
	}
	at += n
	for i := 0; i < n; i++ {
		s[at+i] = int32(e.LeaderOf(i))
	}
	at += n
	for i := 0; i < n; i++ {
		s[at+i] = int32(e.TerritoryOf(i))
	}
	at += n
	for _, tr := range L.Transfers {
		s[at], s[at+1], s[at+2] = int32(tr.From), int32(tr.To), int32(tr.ID)
		s[at+3], s[at+4], s[at+5], s[at+6] = int32(tr.TTL), int32(tr.Born), int32(tr.Dur), int32(tr.Kind)
		at += TransferWords
	}
	L.EachFlash(func(a, b, until, kind int) {
		s[at], s[at+1], s[at+2], s[at+3] = int32(a), int32(b), int32(until), int32(kind)
		at += FlashWords
	})
	for _, pair := range ov {
		s[at], s[at+1] = int32(pair[0]), int32(pair[1])
		at += 2
	}
	return words
}

func (b *Bridge) SnapshotAddr() uint32 { return addr(b.snap) }

func b2i(v bool) int {
	if v {
		return 1
	}
	return 0
}

// addr is a slice's address in linear memory. The module is 32-bit, and the
// slices are fields of a long-lived Bridge, so the address stays valid until
// the slice is next regrown.
func addr[T any](s []T) uint32 {
	if cap(s) == 0 {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(unsafe.SliceData(s))))
}

// Verification hooks: they let a host check the compiled module against the
// original page without exposing engine internals.

func (b *Bridge) TargetsDigest(dst, rule int) uint32 {
	return b.E.TargetsDigest(dst, engine.Rule(rule))
}

// Headless packs a single-message run as tx<<5 | (hops+1)<<1 | delivered.
func (b *Bridge) Headless(src, dst, rule int) int {
	r := b.E.RunHeadless(src, dst, engine.Rule(rule))
	return r.TX<<5 | (r.Hops+1)<<1 | b2i(r.Delivered)
}

func (b *Bridge) Src() int { return b.E.G.Src }
func (b *Bridge) Dst() int { return b.E.G.Dst }

// InRangeCount is how many peers node i can hear.
func (b *Bridge) InRangeCount(i int) int { return len(b.E.G.InRange[i]) }
