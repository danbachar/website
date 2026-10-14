package engine

import "math"

// RNG is mulberry32, bit-identical to the page's generator so a seed gives the
// same topology and the same traffic in either implementation.
type RNG struct{ a uint32 }

func NewRNG(seed int64) *RNG { return &RNG{a: uint32(seed)} }

func (r *RNG) Float64() float64 {
	r.a += 0x6D2B79F5
	t := (r.a ^ (r.a >> 15)) * (1 | r.a)
	t = (t + (t^(t>>7))*(61|t)) ^ t
	return float64(t^(t>>14)) / 4294967296
}

// intBelow is the page's `(Math.random() * n) | 0`.
func (r *RNG) intBelow(n int) int { return int(r.Float64() * float64(n)) }

// hypot follows V8's Math.hypot (max-scaled, Kahan-summed) for two arguments.
// The float64() conversions stop the compiler fusing multiply-adds, which
// would change the last bit.
func hypot(a, b float64) float64 {
	a, b = math.Abs(a), math.Abs(b)
	max := a
	if b > max {
		max = b
	}
	if max == 0 {
		return 0
	}
	var sum, comp float64
	for _, v := range [2]float64{a, b} {
		n := v / max
		summand := float64(n*n) - comp
		prelim := sum + summand
		comp = (prelim - sum) - summand
		sum = prelim
	}
	return math.Sqrt(sum) * max
}

// jsRound is Math.round for non-negative x: ties go up.
func jsRound(x float64) float64 {
	f := math.Floor(x)
	if x-f >= 0.5 {
		return f + 1
	}
	return f
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func without(s []int, v int) []int {
	out := make([]int, 0, len(s))
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

type linkKey struct{ lo, hi int32 }

func lk(a, b int) linkKey {
	if a < b {
		return linkKey{int32(a), int32(b)}
	}
	return linkKey{int32(b), int32(a)}
}

type laneKey struct{ holder, id int32 }

type bitset []uint64

func (b bitset) has(id int) bool {
	w := id >> 6
	return w < len(b) && b[w]>>(uint(id)&63)&1 == 1
}

func (b *bitset) add(id int) {
	w := id >> 6
	for len(*b) <= w {
		*b = append(*b, 0)
	}
	(*b)[w] |= 1 << (uint(id) & 63)
}

// heldPkt is one custody entry. A node's custody keeps insertion order,
// because overflow evicts the oldest entry.
type heldPkt struct{ id, ttl int }

type holdList []heldPkt

func (h holdList) find(id int) int {
	for i := range h {
		if h[i].id == id {
			return i
		}
	}
	return -1
}

func (h holdList) has(id int) bool { return h.find(id) >= 0 }

func (h *holdList) set(id, ttl int) {
	if i := h.find(id); i >= 0 {
		(*h)[i].ttl = ttl
		return
	}
	*h = append(*h, heldPkt{id, ttl})
}

func (h *holdList) del(id int) {
	if i := h.find(id); i >= 0 {
		copy((*h)[i:], (*h)[i+1:])
		*h = (*h)[:len(*h)-1]
	}
}
