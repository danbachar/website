package engine

import "testing"

// A node's cached lane choice exists only while the node holds the packet. A
// cache that outlives its packets grows with every message the mesh ever carried.
func TestLaneCacheOnlyHoldsHeldPackets(t *testing.T) {
	cases := []struct {
		name         string
		rule         Rule
		n, seed      int
		maxAge       int
		trafficSeed  int64
		ticks        int
		reachesCap   bool // the run must fill a node's custody, so eviction is exercised
		wantDelivery bool
	}{
		{"flood, copies age out", Flood, 4, 11, 300, 1, 3000, false, true},
		{"top-3 with link rotation", F3, 4, 11, 300, 2, 3000, false, true},
		{"cluster entry, no leader", HMulti, 4, 11, 300, 3, 3000, false, true},
		{"flood past the custody cap", Flood, 8, 3, 100000, 5, 4200, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := New()
			e.Geo = Geometry{World: 200, Cell: 50}
			e.MaxLinks = 7
			e.MaxAge = c.maxAge
			e.SetSK(0.5, 0, 3)
			e.Rebuild(c.n, c.seed)
			e.Rule = c.rule
			e.StartLive(c.trafficSeed)

			atCap := false
			for tick := 1; tick <= c.ticks; tick++ {
				e.Step()
				for _, h := range e.L.hold {
					if len(h) == CustodyCap {
						atCap = true
					}
				}
				if tick%50 != 0 {
					continue
				}
				for k := range e.L.lanes {
					if !e.L.hold[k.holder].has(int(k.id)) {
						t.Fatalf("tick %d: node %d caches lanes for packet %d it no longer holds", tick, k.holder, k.id)
					}
				}
			}
			if c.reachesCap && !atCap {
				t.Fatal("no node's custody reached the cap, so eviction was not exercised")
			}
			if c.wantDelivery && e.L.Delivered == 0 {
				t.Fatal("nothing was delivered, so custody release was not exercised")
			}
		})
	}
}
