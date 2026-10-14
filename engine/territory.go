package engine

import (
	"math"
	"slices"
)

const (
	// ElectEvery is how many ticks a territory waits before it reconsiders who
	// leads: the cadence at which fresh advertisements arrive.
	ElectEvery = 10
	// Hysteresis is how much better a challenger must be before the role moves.
	Hysteresis = 0.1
	// BackboneDegree is how many UDX peers a leader keeps. With no more leaders
	// than that, every leader peers with every other.
	BackboneDegree = 6
	// LeaderRange is how far two leaders hear each other over BLE, in radio
	// ranges: 120 m against a 50 m radio.
	LeaderRange = 2.4
)

// Link kinds for a transfer: the shared BLE radio, or a leader's own UDX stream.
const (
	KindBLE = 0
	KindUDX = 1
)

// Territories records who leads and who answers to whom. A leader is chosen on
// buffer headroom, which moves with traffic, so the assignment changes while
// the mesh runs.
type Territories struct {
	leader []bool
	of     []int // the leader node i answers to; -1 while it has none
	id     []int // territory id, which outlives whoever leads it; -1 while unattached
	nextID int
	over   [][]int // a leader's links to other leaders: long-range BLE (amigo) or UDX (amigo-udx)
	Round  int     // bumped whenever the set of leaders changes
}

func newTerritories(n int) *Territories {
	T := &Territories{
		leader: make([]bool, n),
		of:     make([]int, n),
		id:     make([]int, n),
		over:   make([][]int, n),
	}
	for i := range T.of {
		T.of[i], T.id[i] = -1, -1
	}
	return T
}

// territories returns the current assignment, electing the first one on demand.
func (e *Engine) territories() *Territories {
	if e.L.terr == nil {
		e.L.terr = newTerritories(len(e.G.Nodes))
		e.elect(true)
	}
	return e.L.terr
}

// IsLeader, LeaderOf and TerritoryOf describe node i. They answer -1 or false
// for a rule without territories.
func (e *Engine) IsLeader(i int) bool {
	return e.Rule.Territorial() && e.territories().leader[i]
}

func (e *Engine) LeaderOf(i int) int {
	if !e.Rule.Territorial() {
		return -1
	}
	return e.territories().of[i]
}

func (e *Engine) TerritoryOf(i int) int {
	if !e.Rule.Territorial() {
		return -1
	}
	return e.territories().id[i]
}

// TerritoryRound counts the election rounds that changed who leads.
func (e *Engine) TerritoryRound() int {
	if !e.Rule.Territorial() {
		return 0
	}
	return e.territories().Round
}

// OverlayLinks lists each leader-to-leader link once, as pairs, for drawing.
func (e *Engine) OverlayLinks() [][2]int {
	if !e.Rule.Territorial() {
		return nil
	}
	T := e.territories()
	var out [][2]int
	for a, peers := range T.over {
		for _, b := range peers {
			if a < b {
				out = append(out, [2]int{a, b})
			}
		}
	}
	return out
}

// score is how much custody room node k has left, which is all a node's
// suitability for the role is.
func (e *Engine) score(k int) float64 {
	return 1 - math.Min(1, float64(len(e.L.hold[k]))/float64(CustodyCap))
}

// canLead: a node may lead once it holds a link, because then it has members
// to serve.
func (e *Engine) canLead(h int) bool { return len(e.G.Adj[h]) > 0 }

// sortByScore orders nodes best score first, lowest id on ties.
func sortByScore(ids []int, sc []float64) {
	slices.SortFunc(ids, func(a, b int) int {
		switch {
		case sc[a] > sc[b]:
			return -1
		case sc[a] < sc[b]:
			return 1
		}
		return a - b
	})
}

// elect runs one election round. The first round takes candidates best score
// first and keeps one whenever no earlier leader is in radio range, so the
// number of territories falls out of coverage. Every later round gives the
// role to the local maxima of the link graph, an incumbent keeping it unless a
// linked peer beats it by the hysteresis margin.
func (e *Engine) elect(first bool) {
	T, n := e.L.terr, len(e.G.Nodes)
	sc := make([]float64, n)
	for k := range sc {
		sc[k] = e.score(k)
	}
	before := slices.Clone(T.leader)
	if first {
		e.seedLeaders(sc)
	} else {
		e.reelectLeaders(sc)
	}
	e.attach(sc)
	e.assignIDs()
	if first || !slices.Equal(before, T.leader) {
		T.Round++
		e.meshLeaders()
	}
}

func (e *Engine) seedLeaders(sc []float64) {
	T, n := e.L.terr, len(e.G.Nodes)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sortByScore(order, sc)
	clear(T.leader)
	for _, h := range order {
		if !e.canLead(h) {
			continue
		}
		covered := false
		for _, a := range e.G.InRange[h] {
			if T.leader[a] {
				covered = true
				break
			}
		}
		if !covered {
			T.leader[h] = true
		}
	}
}

func (e *Engine) reelectLeaders(sc []float64) {
	T, n, adj := e.L.terr, len(e.G.Nodes), e.G.Adj
	var claimants []int
	for h := 0; h < n; h++ {
		if !e.canLead(h) {
			continue
		}
		margin := 0.0
		if T.leader[h] {
			margin = Hysteresis
		}
		leads := true
		for _, p := range adj[h] {
			if sc[p] > sc[h]+margin {
				leads = false
				break
			}
		}
		if leads {
			claimants = append(claimants, h)
		}
	}
	sortByScore(claimants, sc)
	// The margin can leave both ends of a link claiming the role; the better
	// one keeps it.
	elected := make([]bool, n)
	for _, c := range claimants {
		beaten := false
		for _, p := range adj[c] {
			if elected[p] {
				beaten = true
				break
			}
		}
		if !beaten {
			elected[c] = true
		}
	}
	T.leader = elected
}

// attach puts every node under the best-scoring leader it is linked to, so a
// member is always one link from its leader. A node linked to no leader leads
// itself: a bad hand-over makes more territories, never an uncovered node.
func (e *Engine) attach(sc []float64) {
	T, n, adj := e.L.terr, len(e.G.Nodes), e.G.Adj
	for h := 0; h < n; h++ {
		if T.leader[h] {
			T.of[h] = h
			continue
		}
		best := -1
		for _, p := range adj[h] {
			if T.leader[p] && (best < 0 || sc[p] > sc[best] || (sc[p] == sc[best] && p < best)) {
				best = p
			}
		}
		T.of[h] = best
	}
	for h := 0; h < n; h++ {
		if T.of[h] < 0 && e.canLead(h) {
			T.leader[h] = true
			T.of[h] = h
		}
	}
}

// assignIDs gives a territory the id most of its members already carried, so a
// territory keeps its identity while the leadership moves underneath it. Two
// territories never share an id: the first leader to claim it keeps it.
func (e *Engine) assignIDs() {
	T, n := e.L.terr, len(e.G.Nodes)
	members := make([][]int, n)
	for h := 0; h < n; h++ {
		if l := T.of[h]; l >= 0 {
			members[l] = append(members[l], h)
		}
	}
	next := make([]int, n)
	for i := range next {
		next[i] = -1
	}
	claimed := map[int]bool{}
	for l := 0; l < n; l++ {
		if len(members[l]) == 0 {
			continue
		}
		var ids, votes []int
		for _, m := range members[l] {
			prev := T.id[m]
			if prev < 0 || claimed[prev] {
				continue
			}
			k := slices.Index(ids, prev)
			if k < 0 {
				ids, votes = append(ids, prev), append(votes, 0)
				k = len(ids) - 1
			}
			votes[k]++
		}
		id, best := -1, 0
		for k := range ids {
			if votes[k] > best || (votes[k] == best && ids[k] < id) {
				id, best = ids[k], votes[k]
			}
		}
		if id < 0 {
			id = T.nextID
			T.nextID++
		}
		claimed[id] = true
		for _, m := range members[l] {
			next[m] = id
		}
	}
	T.id = next
}

// meshLeaders rebuilds the links among leaders for the current set. Under amigo
// two leaders within LeaderRange hear each other over BLE; those links take the
// radio slots a leader has free, so pairs are taken nearest first and a leader
// stops when its slots are used. Under amigo-udx each leader holds UDX streams to
// the leaders on either side of it in id order, a ring that keeps the backbone
// connected, or to all of them when there are few.
func (e *Engine) meshLeaders() {
	T, n := e.L.terr, len(e.G.Nodes)
	T.over = make([][]int, n)
	var leaders []int
	for i := 0; i < n; i++ {
		if T.leader[i] {
			leaders = append(leaders, i)
		}
	}
	link := func(a, b int) {
		if a != b && !contains(T.over[a], b) {
			T.over[a] = append(T.over[a], b)
			T.over[b] = append(T.over[b], a)
		}
	}
	m := len(leaders)
	switch e.Rule {
	case Amigo:
		reach := LeaderRange * float64(e.Geo.Cell)
		type pair struct {
			a, b int
			d    float64
		}
		var pairs []pair
		for x, a := range leaders {
			for _, b := range leaders[x+1:] {
				dx := e.G.Nodes[a].X - e.G.Nodes[b].X
				dy := e.G.Nodes[a].Y - e.G.Nodes[b].Y
				d := float64(dx*dx) + float64(dy*dy)
				if d <= reach*reach && !contains(e.G.Adj[a], b) {
					pairs = append(pairs, pair{a, b, d})
				}
			}
		}
		slices.SortFunc(pairs, func(p, q pair) int {
			switch {
			case p.d < q.d:
				return -1
			case p.d > q.d:
				return 1
			case p.a != q.a:
				return p.a - q.a
			}
			return p.b - q.b
		})
		// A leader has the same MaxLinks radio slots as any node, so a long-range
		// link may only take a slot its radio links leave free.
		quota := make([]int, n)
		for _, a := range leaders {
			quota[a] = max(0, e.MaxLinks-len(e.G.Adj[a]))
		}
		for _, p := range pairs {
			if len(T.over[p.a]) < quota[p.a] && len(T.over[p.b]) < quota[p.b] {
				link(p.a, p.b)
			}
		}
	case AmigoUDX:
		if m-1 <= BackboneDegree {
			for x, a := range leaders {
				for _, b := range leaders[x+1:] {
					link(a, b)
				}
			}
			return
		}
		half := max(1, BackboneDegree/2)
		for x, a := range leaders {
			for i := 1; i <= half; i++ {
				link(a, leaders[(x+i)%m])
				link(a, leaders[(x-i+m)%m])
			}
		}
	}
}

// reaches reports whether node i holds a link to p: a radio link, or a leader's
// link to another leader.
func (e *Engine) reaches(i, p int) bool {
	if contains(e.G.Adj[i], p) {
		return true
	}
	return e.Rule.Territorial() && e.L != nil && e.L.terr != nil && contains(e.L.terr.over[i], p)
}

// overlayKind is the channel a transfer from i to peer uses: a leader's UDX
// stream when that is the link between them, the shared BLE radio otherwise.
func (e *Engine) overlayKind(i, peer int) int {
	if e.Rule == AmigoUDX && e.L.terr != nil && contains(e.L.terr.over[i], peer) {
		return KindUDX
	}
	return KindBLE
}

// territoryTargets is what the territorial rules offer a packet for dst.
//
// A member hands every packet to its own leader and to nobody else, and to the
// recipient too when it holds a link to it. A leader holding a packet for one of
// its own members delivers it on the link they share. For a recipient elsewhere
// it hands the packet in on a link to that territory's leader when it holds one,
// and nowhere else; with none, it offers the packet to every leader it is linked
// to, which do the same until the hop budget is spent.
func (e *Engine) territoryTargets(i, dst int, eligible func(int) bool) []int {
	T := e.territories()
	ok := func(p int) bool { return eligible == nil || eligible(p) }
	var out []int
	if e.reaches(i, dst) && ok(dst) {
		out = append(out, dst)
	}
	if !T.leader[i] {
		if l := T.of[i]; l >= 0 && l != dst && ok(l) {
			out = append(out, l)
		}
		return out
	}
	home := T.of[dst]
	if home == i {
		return out
	}
	if home >= 0 && e.reaches(i, home) {
		if home != dst && ok(home) {
			out = append(out, home)
		}
		return out
	}
	for _, p := range e.G.Adj[i] {
		if T.leader[p] && p != dst && ok(p) {
			out = append(out, p)
		}
	}
	for _, p := range T.over[i] {
		if p != dst && ok(p) {
			out = append(out, p)
		}
	}
	return out
}
