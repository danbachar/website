package engine

import (
	"math"
	"math/bits"
)

// The digests are FNV-1a over a canonical stream of 32-bit words, defined
// identically in reference/ref_engine.mjs so two implementations can be
// compared tick by tick without exchanging the whole state.

type hasher struct{ h uint32 }

func newHasher() *hasher { return &hasher{h: 2166136261} }

func (x *hasher) i32(v int) {
	u := uint32(int32(v))
	for k := 0; k < 4; k++ {
		x.h ^= (u >> (8 * uint(k))) & 0xff
		x.h *= 16777619
	}
}

func (x *hasher) f64(v float64) {
	b := math.Float64bits(v)
	x.i32(int(int32(uint32(b))))
	x.i32(int(int32(uint32(b >> 32))))
}

func (x *hasher) bool(v bool) {
	if v {
		x.i32(1)
	} else {
		x.i32(0)
	}
}

// TopologyDigest covers node positions, both adjacency structures, the chosen
// pair and the means.
func (e *Engine) TopologyDigest() uint32 {
	x, g := newHasher(), e.G
	x.i32(len(g.Nodes))
	for _, n := range g.Nodes {
		x.f64(n.X)
		x.f64(n.Y)
		x.i32(n.Cluster)
	}
	for i := range g.Nodes {
		x.i32(len(g.InRange[i]))
		for _, j := range g.InRange[i] {
			x.i32(j)
		}
	}
	e.adjDigest(x)
	x.i32(g.Src)
	x.i32(g.Dst)
	x.f64(g.MeanRange)
	x.f64(g.MeanLink)
	x.f64(g.BoundaryShare)
	return x.h
}

func (e *Engine) adjDigest(x *hasher) {
	for i := range e.G.Nodes {
		x.i32(len(e.G.Adj[i]))
		for _, j := range e.G.Adj[i] {
			x.i32(j)
		}
	}
}

// LiveDigest covers everything the mesh is allowed to differ on.
func (e *Engine) LiveDigest() uint32 {
	x, L, n := newHasher(), e.L, len(e.G.Nodes)
	x.i32(L.T)
	x.i32(L.Created)
	x.i32(L.Delivered)
	x.i32(L.TX)
	x.i32(L.Rotations)
	x.i32(L.AgedOut)
	x.i32(L.NextID)
	x.i32(len(L.Transfers))
	x.i32(e.CustodyTotal())
	for i := 0; i < n; i++ {
		x.i32(len(L.hold[i]))
		for _, h := range L.hold[i] {
			x.i32(h.id)
			x.i32(h.ttl)
		}
	}
	for _, tr := range L.Transfers {
		x.i32(tr.From)
		x.i32(tr.To)
		x.i32(tr.ID)
		x.i32(tr.TTL)
		x.i32(tr.Born)
		x.i32(tr.Dur)
	}
	e.adjDigest(x)

	keys := L.flashKeys()
	x.i32(len(keys))
	for _, k := range keys {
		f := L.flash[k]
		x.i32(int(k.lo))
		x.i32(int(k.hi))
		x.i32(f.until)
		x.i32(f.kind)
	}

	if t := L.Track; t != nil {
		x.i32(1)
		x.i32(t.ID)
		x.i32(t.TX)
		x.i32(t.Hops)
		x.i32(t.Born)
		x.bool(t.Done)
		x.bool(t.Expired)
		x.i32(t.CopiesSum)
		x.i32(t.Ticks)
		x.i32(t.At)
	} else {
		x.i32(0)
	}

	live, born := 0, 0
	for id := 1; id < len(L.dstOf); id++ {
		if L.dstOf[id] >= 0 {
			live++
		}
		if L.bornOf[id] >= 0 {
			born++
		}
	}
	x.i32(live)
	for id := 1; id < len(L.dstOf); id++ {
		if L.dstOf[id] >= 0 {
			x.i32(id)
			x.i32(L.dstOf[id])
		}
	}
	x.i32(born)
	for id := 1; id < len(L.bornOf); id++ {
		if L.bornOf[id] >= 0 {
			x.i32(id)
			x.i32(L.bornOf[id])
		}
	}
	for i := 0; i < n; i++ {
		c := 0
		for _, w := range L.seen[i] {
			c += bits.OnesCount64(w)
		}
		x.i32(c)
	}
	if e.Rule.Territorial() {
		T := e.territories()
		x.i32(T.Round)
		for i := 0; i < n; i++ {
			x.i32(T.of[i])
			x.i32(T.id[i])
			x.i32(len(T.over[i]))
			for _, p := range T.over[i] {
				x.i32(p)
			}
		}
		for _, tr := range L.Transfers {
			x.i32(tr.Kind)
		}
	}
	return x.h
}

// TargetsDigest hashes ruleTargets for every node, for the given recipient and
// rule.
func (e *Engine) TargetsDigest(dst int, rule Rule) uint32 {
	x := newHasher()
	for i := range e.G.Nodes {
		t := e.ruleTargets(i, dst, rule, nil)
		x.i32(len(t))
		for _, p := range t {
			x.i32(p)
		}
	}
	return x.h
}

// IntsDigest hashes a list as the harness hashes per-node counts: no length
// prefix.
func IntsDigest(v []int) uint32 { return IntsDigestRaw(v) }

// IntsDigestRaw hashes the values in order.
func IntsDigestRaw(v []int) uint32 {
	x := newHasher()
	for _, i := range v {
		x.i32(i)
	}
	return x.h
}
