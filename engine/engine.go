package engine

import "math"

const (
	IdleWindow  = 90  // ticks a link may stay quiet before it is a rotation candidate
	CentralityW = 150 // sliding window for C_k, in ticks
	CustodyCap  = 60  // packets a node will hold for others
	GenEvery    = 7   // ticks between new background messages
)

// SK are the S_k score parameters: S_k = alpha*C_k + beta*(1-rho_k), a peer
// passes the gate at S_k >= Theta, and a node floods on its top F lanes.
type SK struct {
	Alpha, Beta, Theta float64
	F                  int
}

var MeasuredSK = SK{Alpha: 0.5, Beta: 0, Theta: 0, F: 3}

// caches hold values derived from the link set; they are dropped whenever
// the links change.
type caches struct {
	dist      map[int][]int32
	within    map[int][]int32
	entry     map[int][]bool
	entryList map[int][]int
	entryDist map[int][]int32
}

type Engine struct {
	Geo      Geometry
	MaxLinks int
	MaxAge   int // ticks a copy may sit in custody before it is dropped
	SK       SK
	Rule     Rule
	G        *Graph
	L        *Live
	oc       caches
	graphVer int
}

func New() *Engine {
	e := &Engine{
		Geo:      Geometry{World: 200, Cell: 50},
		MaxLinks: 7,
		MaxAge:   1500,
		SK:       MeasuredSK,
		Rule:     HMulti,
	}
	e.bump()
	return e
}

func (e *Engine) bump() {
	e.oc = caches{
		dist:      map[int][]int32{},
		within:    map[int][]int32{},
		entry:     map[int][]bool{},
		entryList: map[int][]int{},
		entryDist: map[int][]int32{},
	}
	e.graphVer++
}

// GraphVersion changes whenever the link set may have changed.
func (e *Engine) GraphVersion() int { return e.graphVer }

type RebuildInfo struct {
	NodesPerCell  int
	Total         int
	Capped        bool
	MeanRange     float64
	MeanLink      float64
	BoundaryShare float64
}

// Rebuild draws a fresh topology. The per-cell count is trimmed so the field
// stays drawable.
func (e *Engine) Rebuild(want, seed int) RebuildInfo {
	grid := e.Geo.Grid()
	cells := grid * grid
	n := MaxNodes / cells
	if want < n {
		n = want
	}
	if n < 1 {
		n = 1
	}
	e.G = BuildTopology(n, seed, 0, e.Geo, e.MaxLinks)
	e.bump()
	e.G.BoundaryShare = e.G.boundaryShare()
	return RebuildInfo{
		NodesPerCell: n, Total: len(e.G.Nodes), Capped: n < want,
		MeanRange: e.G.MeanRange, MeanLink: e.G.MeanLink,
		BoundaryShare: e.G.BoundaryShare,
	}
}

// SetSK replaces the score parameters; lane sets are derived from them, so
// everything cached is dropped.
func (e *Engine) SetSK(alpha, beta float64, f int) {
	e.SK.Alpha, e.SK.Beta, e.SK.F = alpha, beta, f
	e.bump()
	if e.L != nil {
		e.L.lanes = map[laneKey][]int{}
	}
}

func (e *Engine) distTo(dst int) []int32 {
	d, ok := e.oc.dist[dst]
	if !ok {
		d = bfs(e.G.Adj, []int{dst})
		e.oc.dist[dst] = d
	}
	return d
}

// distWithin is the hop distance to dst over the recipient cluster's own
// links only.
func (e *Engine) distWithin(dst int) []int32 {
	d, ok := e.oc.within[dst]
	if !ok {
		dc := e.G.Nodes[dst].Cluster
		adj := make([][]int, len(e.G.Adj))
		for i, links := range e.G.Adj {
			if e.G.Nodes[i].Cluster != dc {
				continue
			}
			for _, j := range links {
				if e.G.Nodes[j].Cluster == dc {
					adj[i] = append(adj[i], j)
				}
			}
		}
		d = bfs(adj, []int{dst})
		e.oc.within[dst] = d
	}
	return d
}

func (e *Engine) entrySet(cluster int) []bool {
	set, ok := e.oc.entry[cluster]
	if !ok {
		list := entryPoints(e.G, cluster)
		set = make([]bool, len(e.G.Nodes))
		for _, i := range list {
			set[i] = true
		}
		e.oc.entry[cluster] = set
		e.oc.entryList[cluster] = list
	}
	return set
}

func (e *Engine) distToEntry(cluster int) []int32 {
	d, ok := e.oc.entryDist[cluster]
	if !ok {
		e.entrySet(cluster)
		d = bfs(e.G.Adj, e.oc.entryList[cluster])
		e.oc.entryDist[cluster] = d
	}
	return d
}

// EntryPoints lists every cluster's entry point(s) under the current rule,
// for drawing. Empty for rules without clusters.
func (e *Engine) EntryPoints() []int {
	if !e.Rule.Hierarchical() {
		return nil
	}
	var out []int
	grid := e.Geo.Grid()
	for c := 0; c < grid*grid; c++ {
		e.entrySet(c)
		out = append(out, e.oc.entryList[c]...)
	}
	return out
}

// IsEntryPoint reports whether node i is an entry point of its own cluster.
func (e *Engine) IsEntryPoint(i int) bool {
	return e.Rule.Hierarchical() && e.entrySet(e.G.Nodes[i].Cluster)[i]
}

// centrality is C_k: distinct neighbours that fed k inside the window, over
// the link cap.
func (e *Engine) centrality(k int) float64 {
	if e.L == nil {
		return 0
	}
	f := 0
	for _, fd := range e.L.fedBy[k] {
		if e.L.T-fd.t <= CentralityW {
			f++
		}
	}
	return math.Min(1, float64(f)/float64(e.MaxLinks))
}

// suitability is S_k on the values the peer advertises.
func (e *Engine) suitability(k int) float64 {
	rho := 0.0
	if e.L != nil {
		rho = math.Min(1, float64(len(e.L.hold[k]))/float64(CustodyCap))
	}
	return float64(e.SK.Alpha*e.centrality(k)) + float64(e.SK.Beta*(1-rho))
}

// ruleTargets returns which of node i's links carry a copy addressed to dst.
//
// `eligible` removes peers that already hold the packet before the fan-out cap
// is applied, so top-F means the top F peers that can still use it.
func (e *Engine) ruleTargets(i, dst int, rule Rule, eligible func(int) bool) []int {
	if rule.Territorial() {
		return e.territoryTargets(i, dst, eligible)
	}
	g := e.G
	peers := make([]int, 0, len(g.Adj[i]))
	for _, p := range g.Adj[i] {
		if eligible == nil || eligible(p) {
			peers = append(peers, p)
		}
	}
	switch rule {
	case Flood:
		return peers
	case F3:
		cand := make([]int, 0, len(peers))
		score := make([]float64, 0, len(peers))
		for _, p := range peers {
			if s := e.suitability(p); s >= e.SK.Theta {
				cand = append(cand, p)
				score = append(score, s)
			}
		}
		// highest score first, lowest id on ties
		for a := 1; a < len(cand); a++ {
			for b := a; b > 0; b-- {
				if score[b] > score[b-1] || (score[b] == score[b-1] && cand[b] < cand[b-1]) {
					cand[b], cand[b-1] = cand[b-1], cand[b]
					score[b], score[b-1] = score[b-1], score[b]
				} else {
					break
				}
			}
		}
		if len(cand) > e.SK.F {
			cand = cand[:e.SK.F]
		}
		// a message addressed to a link's own peer bypasses lanes entirely
		if contains(peers, dst) && !contains(cand, dst) {
			cand = append(cand, dst)
		}
		return cand
	case Oracle:
		d := e.distTo(dst)
		here := d[i]
		out := peers[:0:0]
		for _, p := range peers {
			if d[p] < here {
				out = append(out, p)
			}
		}
		return out
	}
	dc := g.Nodes[dst].Cluster
	if g.Nodes[i].Cluster == dc {
		// Inside the recipient's cluster the message routes within it. If the
		// cluster is internally partitioned the inside distance is infinite
		// and the message falls back to the global gradient.
		din := e.distWithin(dst)
		inside := din[i]
		if inside != inf {
			var out []int
			for _, p := range peers {
				if p == dst || (g.Nodes[p].Cluster == dc && din[p] < inside) {
					out = append(out, p)
				}
			}
			return out
		}
		d := e.distTo(dst)
		here := d[i]
		var out []int
		for _, p := range peers {
			if d[p] < here {
				out = append(out, p)
			}
		}
		return out
	}
	entry := e.entrySet(dc)
	d := e.distToEntry(dc)
	here := d[i]
	var out []int
	for _, p := range peers {
		if p == dst {
			out = append(out, p)
			continue
		}
		if g.Nodes[p].Cluster == dc && !entry[p] {
			continue // one door
		}
		if d[p] < here {
			out = append(out, p)
		}
	}
	return out
}

// LaneCount is how many of node i's links the current rule would forward the
// current recipient's traffic on.
func (e *Engine) LaneCount(i int) int {
	return len(e.ruleTargets(i, e.G.Dst, e.Rule, nil))
}

type Headless struct {
	TX        int
	Hops      int // -1 if the recipient was never reached
	Delivered bool
}

// RunHeadless is an instant, silent single-message run: the paired flood
// baseline every transmission ratio is against.
func (e *Engine) RunHeadless(src, dst int, rule Rule) Headless {
	if rule.Territorial() {
		panic("headless runs are defined for the rules of the original page")
	}
	n := len(e.G.Nodes)
	seen := make([]bool, n)
	ttl := make([]int, n)
	for i := range ttl {
		ttl[i] = -1
	}
	seen[src], ttl[src] = true, TTL
	frontier := []int{src}
	tx, hops := 0, -1
	notSeen := func(q int) bool { return !seen[q] }
	for r := 0; r < 40 && len(frontier) > 0; r++ {
		var next []int
		for _, i := range frontier {
			if ttl[i] <= 0 && i != src {
				continue
			}
			for _, p := range e.ruleTargets(i, dst, rule, notSeen) {
				tx++
				seen[p] = true
				ttl[p] = ttl[i] - 1
				if p == dst && hops < 0 {
					hops = TTL - ttl[p]
				}
				if ttl[p] > 0 && p != dst {
					next = append(next, p)
				}
			}
		}
		frontier = next
	}
	return Headless{TX: tx, Hops: hops, Delivered: seen[dst]}
}
