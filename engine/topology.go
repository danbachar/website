package engine

import "math"

const (
	TTL      = 7
	MaxNodes = 4000
	inf      = int32(1 << 30)
)

// Rule is a forwarding rule. The order is the order the page lists them in.
type Rule int

const (
	Flood Rule = iota
	F3
	HMulti
	Amigo
	AmigoUDX
	Oracle
)

func (r Rule) Hierarchical() bool { return r == HMulti }

// Territorial rules route through elected leaders; see territory.go.
func (r Rule) Territorial() bool { return r == Amigo || r == AmigoUDX }

type Node struct {
	X, Y    float64
	Cluster int
}

type Graph struct {
	Nodes         []Node
	Adj           [][]int
	InRange       [][]int
	Src, Dst      int
	MeanRange     float64
	MeanLink      float64
	BoundaryShare float64
}

// Geometry: a cluster is one cell of the hall and the radio reaches one cell
// side, so Cell is both.
type Geometry struct{ World, Cell int }

func (g Geometry) Grid() int { return g.World / g.Cell }

// BuildTopology places npc nodes in every cell, links them greedily in
// per-node discovery order up to maxLinks, and draws a sender and recipient
// in different clusters, as far apart as the draw allows.
func BuildTopology(npc, seed, pairSalt int, geo Geometry, maxLinks int) *Graph {
	rnd := NewRNG(int64(seed)*7919 + 17)
	grid := geo.Grid()
	cells := grid * grid
	total := npc * cells
	cell := float64(geo.Cell)

	nodes := make([]Node, total)
	for k := 0; k < total; k++ {
		cluster := k % cells
		cx, cy := cluster%grid, cluster/grid
		x := (float64(cx) + rnd.Float64()) * cell
		y := (float64(cy) + rnd.Float64()) * cell
		nodes[k] = Node{X: x, Y: y, Cluster: cluster}
	}

	r2 := cell * cell
	inRange := make([][]int, total)
	for i := 0; i < total; i++ {
		for j := i + 1; j < total; j++ {
			dx := nodes[i].X - nodes[j].X
			dy := nodes[i].Y - nodes[j].Y
			if float64(dx*dx)+float64(dy*dy) <= r2 {
				inRange[i] = append(inRange[i], j)
				inRange[j] = append(inRange[j], i)
			}
		}
	}

	adj := make([][]int, total)
	order := make([]int, total)
	for i := range order {
		order[i] = i
	}
	for k := len(order) - 1; k > 0; k-- {
		s := int(math.Floor(rnd.Float64() * float64(k+1)))
		order[k], order[s] = order[s], order[k]
	}
	for _, i := range order {
		cand := append([]int(nil), inRange[i]...)
		for k := len(cand) - 1; k > 0; k-- {
			s := int(math.Floor(rnd.Float64() * float64(k+1)))
			cand[k], cand[s] = cand[s], cand[k]
		}
		for _, j := range cand {
			if len(adj[i]) >= maxLinks {
				break
			}
			if len(adj[j]) >= maxLinks || contains(adj[i], j) {
				continue
			}
			adj[i] = append(adj[i], j)
			adj[j] = append(adj[j], i)
		}
	}

	prnd := NewRNG(int64(seed)*104729 + int64(pairSalt)*1301 + 7)
	src, dst, best := 0, 0, -1.0
	for t := 0; t < 60; t++ {
		a := int(math.Floor(prnd.Float64() * float64(total)))
		b := int(math.Floor(prnd.Float64() * float64(total)))
		if nodes[a].Cluster == nodes[b].Cluster {
			continue
		}
		d := hypot(nodes[a].X-nodes[b].X, nodes[a].Y-nodes[b].Y)
		if d > best {
			best, src, dst = d, a, b
		}
	}

	var sumRange, sumLink int
	for i := 0; i < total; i++ {
		sumRange += len(inRange[i])
		sumLink += len(adj[i])
	}
	return &Graph{
		Nodes: nodes, Adj: adj, InRange: inRange, Src: src, Dst: dst,
		MeanRange: float64(sumRange) / float64(total),
		MeanLink:  float64(sumLink) / float64(total),
	}
}

// bfs returns hop counts from the nearest source, inf where unreachable.
func bfs(adj [][]int, sources []int) []int32 {
	d := make([]int32, len(adj))
	for i := range d {
		d[i] = inf
	}
	q := make([]int, 0, len(adj))
	for _, s := range sources {
		d[s] = 0
		q = append(q, s)
	}
	for h := 0; h < len(q); h++ {
		v := q[h]
		for _, w := range adj[v] {
			if d[w] == inf {
				d[w] = d[v] + 1
				q = append(q, w)
			}
		}
	}
	return d
}

func (g *Graph) boundaryShare() float64 {
	boundary := 0
	for i := range g.Nodes {
		for _, j := range g.Adj[i] {
			if g.Nodes[j].Cluster != g.Nodes[i].Cluster {
				boundary++
				break
			}
		}
	}
	m := len(g.Nodes)
	if m < 1 {
		m = 1
	}
	return float64(boundary) / float64(m)
}

// entryPoints lists the members of `cluster` that may accept inbound traffic:
// every member holding a link out of it, or all of them when none does.
func entryPoints(g *Graph, cluster int) []int {
	var members, boundary []int
	for i := range g.Nodes {
		if g.Nodes[i].Cluster != cluster {
			continue
		}
		members = append(members, i)
		for _, j := range g.Adj[i] {
			if g.Nodes[j].Cluster != cluster {
				boundary = append(boundary, i)
				break
			}
		}
	}
	if len(boundary) > 0 {
		return boundary
	}
	return members
}
