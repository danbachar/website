package engine

import (
	"slices"
	"testing"
)

func territorialField(rule Rule, npc, seed, world, cell int) *Engine {
	e := New()
	e.Geo = Geometry{World: world, Cell: cell}
	e.MaxLinks = 7
	e.Rule = rule
	e.Rebuild(npc, seed)
	e.StartLive(int64(seed) + 100)
	return e
}

// checkTerritories asserts what the election promises: every node with a link
// answers to a leader it is linked to, territory ids belong to one leader each,
// and the links among leaders are symmetric and within the rule's reach.
func checkTerritories(t *testing.T, e *Engine, when string) {
	t.Helper()
	T, n := e.L.terr, len(e.G.Nodes)
	owner := map[int]int{}
	for i := 0; i < n; i++ {
		if len(e.G.Adj[i]) == 0 {
			continue // no link: nothing to lead and nothing to reach
		}
		l := T.of[i]
		if l < 0 {
			t.Fatalf("%s: node %d holds links but answers to nobody", when, i)
		}
		if T.leader[i] != (l == i) {
			t.Fatalf("%s: node %d leader=%v but answers to %d", when, i, T.leader[i], l)
		}
		if l != i && !contains(e.G.Adj[i], l) {
			t.Fatalf("%s: member %d is not linked to its leader %d", when, i, l)
		}
		if T.id[i] < 0 {
			t.Fatalf("%s: node %d has no territory id", when, i)
		}
		if prev, seen := owner[T.id[i]]; seen && prev != l {
			t.Fatalf("%s: territory id %d is shared by leaders %d and %d", when, T.id[i], prev, l)
		}
		owner[T.id[i]] = l
	}
	reach := LeaderRange * float64(e.Geo.Cell)
	for a, peers := range T.over {
		if len(peers) > 0 && !T.leader[a] {
			t.Fatalf("%s: member %d holds a leader link", when, a)
		}
		for _, b := range peers {
			if !T.leader[b] || !contains(T.over[b], a) {
				t.Fatalf("%s: leader link %d-%d is not between two leaders both ways", when, a, b)
			}
			if e.Rule == Amigo {
				dx, dy := e.G.Nodes[a].X-e.G.Nodes[b].X, e.G.Nodes[a].Y-e.G.Nodes[b].Y
				if float64(dx*dx)+float64(dy*dy) > reach*reach {
					t.Fatalf("%s: leaders %d and %d are linked beyond %v m", when, a, b, reach)
				}
			}
		}
		if e.Rule == Amigo && len(e.G.Adj[a])+len(peers) > e.MaxLinks {
			t.Fatalf("%s: leader %d drives %d radio and %d long-range links, over the %d-slot budget", when, a, len(e.G.Adj[a]), len(peers), e.MaxLinks)
		}
		if e.Rule == AmigoUDX && len(peers) > BackboneDegree {
			t.Fatalf("%s: leader %d keeps %d UDX peers, more than %d", when, a, len(peers), BackboneDegree)
		}
	}
}

func TestElectionInvariants(t *testing.T) {
	for _, rule := range []Rule{Amigo, AmigoUDX} {
		for _, seed := range []int{1, 2, 3} {
			e := territorialField(rule, 4, seed, 200, 50)
			checkTerritories(t, e, "seed")
			for tick := 1; tick <= 1500; tick++ {
				e.Step()
				if tick == 10 || tick == 40 || tick%250 == 0 {
					checkTerritories(t, e, "after rounds")
				}
			}
			if e.L.terr.Round < 2 {
				t.Fatalf("rule %d seed %d: leadership never moved while buffers filled", rule, seed)
			}
		}
	}
}

// handField is eight nodes in three territories: leader 0 with members 1 and 2,
// leader 3 with members 4 and 5, leader 6 with member 7. Members 1 and 4 are
// linked across the boundary. The leaders 0-3-6 are chained by leader links.
func handField(rule Rule) *Engine {
	adj := [][]int{{1, 2}, {0, 4}, {0}, {4, 5}, {3, 1}, {3}, {7}, {6}}
	e := New()
	e.Rule = rule
	e.G = &Graph{Nodes: make([]Node, 8), Adj: adj, InRange: adj}
	e.bump()
	e.StartLive(1)
	T := newTerritories(8)
	for l, members := range map[int][]int{0: {1, 2}, 3: {4, 5}, 6: {7}} {
		T.leader[l], T.of[l], T.id[l] = true, l, l
		for _, m := range members {
			T.of[m], T.id[m] = l, l
		}
	}
	T.over[0], T.over[3], T.over[6] = []int{3}, []int{0, 6}, []int{3}
	e.L.terr = T
	return e
}

func TestTerritoryTargets(t *testing.T) {
	cases := []struct {
		name     string
		from     int
		dst      int
		eligible func(int) bool
		want     []int
	}{
		{"member to a peer of its own territory goes through its leader", 1, 2, nil, []int{0}},
		{"member to another territory goes through its leader", 1, 5, nil, []int{0}},
		{"member delivers on a direct link to the recipient and still uses its leader", 1, 4, nil, []int{4, 0}},
		{"a member never hands a packet to a fellow member, whoever it is next to", 4, 2, nil, []int{3}},
		{"leader delivers to its own member", 0, 2, nil, []int{2}},
		{"leader hands a packet in on the link to the recipient's leader, and nowhere else", 0, 5, nil, []int{3}},
		{"leader with no link to the recipient's leader offers every leader it is linked to", 0, 7, nil, []int{3}},
		{"leader at the far end hands in directly", 3, 7, nil, []int{6}},
		{"nothing to offer once the recipient's leader holds the packet", 0, 5, func(p int) bool { return p != 3 }, nil},
	}
	for _, rule := range []Rule{Amigo, AmigoUDX} {
		e := handField(rule)
		for _, c := range cases {
			got := e.territoryTargets(c.from, c.dst, c.eligible)
			if !slices.Equal(got, c.want) {
				t.Errorf("rule %d: %s: got %v, want %v", rule, c.name, got, c.want)
			}
		}
	}
}

func TestOverlayKind(t *testing.T) {
	if k := handField(AmigoUDX).overlayKind(0, 3); k != KindUDX {
		t.Errorf("a leader link under amigo-udx is a UDX stream, got kind %d", k)
	}
	if k := handField(AmigoUDX).overlayKind(1, 0); k != KindBLE {
		t.Errorf("a member's link to its leader is BLE, got kind %d", k)
	}
	if k := handField(Amigo).overlayKind(0, 3); k != KindBLE {
		t.Errorf("a leader link under amigo is long-range BLE, got kind %d", k)
	}
}

// A hand-over: a leader whose custody fills is replaced once a member beats it
// by the hysteresis margin.
func TestLeadershipMovesWhenBufferFills(t *testing.T) {
	adj := [][]int{{1, 2, 3}, {0}, {0}, {0}}
	e := New()
	e.Rule = Amigo
	e.G = &Graph{Nodes: make([]Node, 4), Adj: adj, InRange: adj}
	e.bump()
	e.StartLive(1)
	if !e.IsLeader(0) {
		t.Fatalf("with empty buffers the lowest id leads: leaders %v", e.L.terr.leader)
	}
	for id := 1; id <= 50; id++ {
		e.L.hold[0].set(id, TTL)
	}
	e.elect(false)
	if e.IsLeader(0) {
		t.Fatalf("a leader at %d of %d custody slots still leads", len(e.L.hold[0]), CustodyCap)
	}
	checkTerritories(t, e, "after hand-over")
}

// Deliveries, and the star as the loop actually runs it: a member's transfer
// goes to its own leader or to the recipient itself, whatever it is next to.
func TestTerritorialDeliveryAndStar(t *testing.T) {
	for _, rule := range []Rule{Amigo, AmigoUDX} {
		e := territorialField(rule, 4, 1, 200, 50)
		udx, delivered := 0, 0
		for tick := 1; tick <= 3000; tick++ {
			if tick%300 == 1 {
				k := tick / 300
				e.SetEndpoints((k*7)%64, (k*13+5)%64)
				e.SendTracked()
			}
			e.Step()
			T := e.L.terr
			for _, tr := range e.L.Transfers {
				if tr.Born != e.L.T {
					continue
				}
				dst, ok := e.L.dst(tr.ID)
				if !T.leader[tr.From] && ok && tr.To != T.of[tr.From] && tr.To != dst {
					t.Fatalf("rule %d tick %d: member %d sent packet %d to %d, not its leader %d or recipient %d",
						rule, tick, tr.From, tr.ID, tr.To, T.of[tr.From], dst)
				}
				if tr.Kind == KindUDX {
					udx++
					if !T.leader[tr.From] || !T.leader[tr.To] {
						t.Fatalf("rule %d tick %d: UDX transfer %d->%d is not between two leaders", rule, tick, tr.From, tr.To)
					}
				}
			}
		}
		for _, r := range e.L.Sends {
			if r.Outcome == OutcomeArrived {
				delivered++
			}
		}
		if delivered == 0 {
			t.Fatalf("rule %d delivered none of %d tracked sends", rule, len(e.L.Sends))
		}
		if rule == AmigoUDX && udx == 0 {
			t.Fatal("amigo-udx never used its backbone")
		}
		if rule == Amigo && udx != 0 {
			t.Fatalf("amigo has no UDX but %d transfers were tagged UDX", udx)
		}
		if len(e.L.lanes) != 0 {
			t.Fatalf("rule %d filled the lane cache with %d entries it never reads", rule, len(e.L.lanes))
		}
	}
}
