package engine

import "math"

// Transfer is a copy in flight. It lands in the tick Born+Dur. Kind says
// whether it rides the shared BLE radio or a leader's UDX stream.
type Transfer struct{ From, To, ID, TTL, Born, Dur, Kind int }

// Track follows the one message the viewer chose the endpoints for. Hops is -1
// until it arrives.
type Track struct {
	ID, TX, Hops, Born int
	Done, Expired      bool
	CopiesSum, Ticks   int
	At, DoneAt         int
}

const (
	OutcomeArrived = 0
	OutcomeExpired = 1
)

// SendRecord is what a finished tracked send leaves behind.
type SendRecord struct {
	Outcome            int
	Hops, Ticks, TX    int
	CopiesSum, CopyTks int
}

type fed struct{ from, t int }

type flashRec struct{ until, kind int }

const (
	FlashDrop = 0
	FlashForm = 1
)

// Live is the sustained mesh: background traffic, custody, in-flight copies
// and link rotation. It reports mechanism only, never a delivery ratio.
type Live struct {
	T, NextID int
	AgedOut   int

	// Message ids are handed out in ascending order and never reused, so a
	// slice indexed by id keeps JS Map insertion order for free.
	dstOf  []int // -1 once delivered or expired
	bornOf []int // -1 once reaped

	hold         []holdList
	seen         []bitset
	lanes        map[laneKey][]int
	linkBusy     map[linkKey]int
	inFlightAt   []int
	fedBy        [][]fed
	lastTraffic  map[linkKey]int
	formedAt     map[linkKey]int
	lastLinked   map[linkKey]int
	lastRotation []int
	flash        map[linkKey]flashRec

	Transfers []Transfer

	Created, Delivered, TX, Rotations int

	Track        *Track
	TrackFloodTX int
	Sends        []SendRecord

	rng     *RNG
	scratch []heldPkt
	terr    *Territories // leaders and their members; only the territorial rules fill it
}

// StartLive begins a fresh mesh on the current topology under e.Rule.
// Background traffic is drawn from trafficSeed.
func (e *Engine) StartLive(trafficSeed int64) {
	n := len(e.G.Nodes)
	L := &Live{
		NextID:       1,
		dstOf:        []int{-1},
		bornOf:       []int{-1},
		hold:         make([]holdList, n),
		seen:         make([]bitset, n),
		lanes:        map[laneKey][]int{},
		linkBusy:     map[linkKey]int{},
		inFlightAt:   make([]int, n),
		fedBy:        make([][]fed, n),
		lastTraffic:  map[linkKey]int{},
		formedAt:     map[linkKey]int{},
		lastLinked:   map[linkKey]int{},
		lastRotation: make([]int, n),
		flash:        map[linkKey]flashRec{},
		rng:          NewRNG(trafficSeed),
	}
	for i := range L.lastRotation {
		L.lastRotation[i] = -IdleWindow
	}
	for i := 0; i < n; i++ {
		for _, j := range e.G.Adj[i] {
			L.formedAt[lk(i, j)] = 0
			L.lastTraffic[lk(i, j)] = 0
		}
	}
	e.L = L
	if e.Rule.Territorial() {
		e.territories()
	}
}

func (L *Live) newID(dst int) int {
	id := L.NextID
	L.NextID++
	L.dstOf = append(L.dstOf, dst)
	L.bornOf = append(L.bornOf, L.T)
	return id
}

func (L *Live) dst(id int) (int, bool) {
	if id < len(L.dstOf) && L.dstOf[id] >= 0 {
		return L.dstOf[id], true
	}
	return 0, false
}

// busyNear counts in-range stations transmitting right now, this one included:
// the contention denominator.
func (e *Engine) busyNear(i int) int {
	a := 1
	for _, j := range e.G.InRange[i] {
		if e.L.inFlightAt[j] > 0 {
			a++
		}
	}
	return a
}

// release drops node i's copy of packet id together with the links it had
// picked for it, so the lane cache only ever holds packets that are still held.
func (L *Live) release(i, id int) {
	L.hold[i].del(id)
	delete(L.lanes, laneKey{int32(i), int32(id)})
}

// lanesFor is the links node i will offer packet id on, decided once when it
// takes the packet and remembered. Deciding afresh each tick would turn F
// into a rate limit rather than a fan-out cap.
func (e *Engine) lanesFor(i, id int) []int {
	if e.Rule.Territorial() {
		// What a territory offers follows who leads, which changes, so it is
		// decided afresh every tick.
		dst, ok := e.L.dst(id)
		if !ok {
			return nil
		}
		return e.ruleTargets(i, dst, e.Rule, func(p int) bool { return !e.L.seen[p].has(id) })
	}
	key := laneKey{int32(i), int32(id)}
	if lanes, ok := e.L.lanes[key]; ok {
		return lanes
	}
	var lanes []int
	if dst, ok := e.L.dst(id); ok {
		lanes = e.ruleTargets(i, dst, e.Rule, func(p int) bool { return !e.L.seen[p].has(id) })
	}
	e.L.lanes[key] = lanes
	return lanes
}

// Step advances the mesh one tick.
func (e *Engine) Step() {
	L, n := e.L, len(e.G.Nodes)
	L.T++

	if e.Rule.Territorial() {
		e.territories() // elects the first assignment if there is none
		if L.T%ElectEvery == 0 {
			e.elect(false)
		}
	}

	// 1. new traffic
	if L.T%GenEvery == 0 {
		src := L.rng.intBelow(n)
		dst := L.rng.intBelow(n)
		if dst == src {
			dst = (dst + 1) % n
		}
		id := L.newID(dst)
		L.hold[src].set(id, TTL)
		L.seen[src].add(id)
		L.Created++
	}

	// 2. One transfer per link per tick, but a packet is offered on every lane
	// the rule picks for it. The radio is time-multiplexed across the links a
	// node is driving, so its rate is divided by that count.
	for i := 0; i < n; i++ {
		if len(L.hold[i]) == 0 {
			continue
		}
		L.scratch = append(L.scratch[:0], L.hold[i]...)
		for _, hp := range L.scratch {
			id, ttl := hp.id, hp.ttl
			dst, ok := L.dst(id)
			if !ok {
				L.release(i, id)
				continue
			}
			var targets []int
			if ttl <= 0 { // hops spent: only the recipient itself is still reachable
				if e.reaches(i, dst) && !L.seen[dst].has(id) {
					targets = []int{dst}
				}
			} else {
				for _, p := range e.lanesFor(i, id) {
					if !L.seen[p].has(id) {
						targets = append(targets, p)
					}
				}
			}
			for _, peer := range targets {
				key := lk(i, peer)
				if L.linkBusy[key] > L.T {
					continue // link busy
				}
				kind := e.overlayKind(i, peer)
				dur := 1 // a UDX stream does not share the radio
				if kind == KindBLE {
					a := e.busyNear(i)
					share := L.inFlightAt[i] + 1 // one radio, split
					if share < 1 {
						share = 1
					}
					dur = int(jsRound(float64(share) * math.Sqrt(float64(a)*math.Log(math.Max(float64(a), 2))) / 2))
					if dur < 1 {
						dur = 1
					}
				}
				L.linkBusy[key] = L.T + dur
				if kind == KindBLE {
					L.inFlightAt[i]++
					L.inFlightAt[peer]++
				}
				L.TX++
				if L.Track != nil && id == L.Track.ID {
					L.Track.TX++
				}
				L.Transfers = append(L.Transfers, Transfer{From: i, To: peer, ID: id, TTL: ttl - 1, Born: L.T, Dur: dur, Kind: kind})
			}
		}
	}

	// 2b. follow our message: copies alive, for its memory footprint
	if t := L.Track; t != nil && !t.Done {
		if _, live := L.dst(t.ID); live {
			c := 0
			for i := 0; i < n; i++ {
				if L.hold[i].has(t.ID) {
					c++
				}
			}
			for _, tr := range L.Transfers {
				if tr.ID == t.ID {
					c++
				}
			}
			t.CopiesSum += c
			t.Ticks++
		}
	}

	// 3. arrivals
	still := make([]Transfer, 0, len(L.Transfers))
	for _, tr := range L.Transfers {
		if L.T-tr.Born < tr.Dur {
			still = append(still, tr)
			continue
		}
		if tr.Kind == KindBLE {
			L.inFlightAt[tr.From]--
			L.inFlightAt[tr.To]--
		}
		dst, wanted := L.dst(tr.ID)
		L.lastTraffic[lk(tr.From, tr.To)] = L.T
		e.feed(tr.To, tr.From)
		if L.seen[tr.To].has(tr.ID) {
			continue
		}
		L.seen[tr.To].add(tr.ID)
		if !wanted {
			continue // delivered while this copy flew
		}
		if tr.To == dst {
			if t := L.Track; t != nil && tr.ID == t.ID && t.Hops < 0 {
				t.Hops = TTL - tr.TTL
				t.At = L.T - t.Born
				t.Done = true
				e.recordSend(OutcomeArrived)
			}
			L.Delivered++
			L.dstOf[tr.ID] = -1 // custody release, everywhere
			for k := 0; k < n; k++ {
				L.release(k, tr.ID)
			}
		} else {
			L.hold[tr.To].set(tr.ID, tr.TTL)
			if len(L.hold[tr.To]) > CustodyCap {
				L.release(tr.To, L.hold[tr.To][0].id) // drop oldest
			}
		}
	}
	L.Transfers = still

	// 4. Two sweeps. A hop-exhausted copy can never be relayed again, so it
	// goes at once. Age is the long-stop for a copy that still has hops but no
	// way to spend them.
	if L.T%20 == 0 {
		for i := 0; i < n; i++ {
			L.scratch = append(L.scratch[:0], L.hold[i]...)
			for _, hp := range L.scratch {
				if d, ok := L.dst(hp.id); hp.ttl <= 0 && !(ok && d == i) {
					L.release(i, hp.id)
					continue
				}
				born := L.T
				if hp.id < len(L.bornOf) && L.bornOf[hp.id] >= 0 {
					born = L.bornOf[hp.id]
				}
				if L.T-born > e.MaxAge {
					L.release(i, hp.id)
				}
			}
		}
		for id := 1; id < len(L.bornOf); id++ {
			born := L.bornOf[id]
			if born < 0 || L.T-born <= e.MaxAge {
				continue
			}
			if _, live := L.dst(id); live {
				L.AgedOut++
				L.dstOf[id] = -1
				if t := L.Track; t != nil && id == t.ID && !t.Done {
					t.Done = true
					t.Expired = true
					e.recordSend(OutcomeExpired)
				}
			}
			L.bornOf[id] = -1
		}
	}

	// 5. rotation, gated on the policy: only the top-F rule runs with it on
	if e.Rule == F3 {
		e.rotate()
	}

	for k, f := range L.flash {
		if f.until < L.T {
			delete(L.flash, k)
		}
	}
}

func (e *Engine) feed(to, from int) {
	fs := e.L.fedBy[to]
	for i := range fs {
		if fs[i].from == from {
			fs[i].t = e.L.T
			return
		}
	}
	e.L.fedBy[to] = append(fs, fed{from, e.L.T})
}

func (e *Engine) recordSend(outcome int) {
	L, t := e.L, e.L.Track
	t.DoneAt = L.T
	L.Sends = append(L.Sends, SendRecord{
		Outcome: outcome, Hops: t.Hops, Ticks: L.T - t.Born, TX: t.TX,
		CopiesSum: t.CopiesSum, CopyTks: t.Ticks,
	})
}

// rotate evicts one link that has been quiet both ways, then fills the slot
// with the least-recently-linked candidate in range (novelty-first admission).
func (e *Engine) rotate() {
	L, g, n := e.L, e.G, len(e.G.Nodes)
	moved := false
	for i := 0; i < n; i++ {
		if L.T-L.lastRotation[i] < IdleWindow {
			continue
		}
		victim, quietest := -1, math.MaxInt
		for _, j := range g.Adj[i] {
			key := lk(i, j)
			if L.T-L.formedAt[key] < IdleWindow {
				continue // grace
			}
			q := L.lastTraffic[key]
			if L.T-q >= IdleWindow && q < quietest {
				quietest, victim = q, j
			}
		}
		if victim < 0 {
			continue
		}
		g.Adj[i] = without(g.Adj[i], victim)
		g.Adj[victim] = without(g.Adj[victim], i)
		L.lastLinked[lk(i, victim)] = L.T
		L.lastRotation[i] = L.T
		L.Rotations++
		L.flash[lk(i, victim)] = flashRec{L.T + 6, FlashDrop}

		best, oldest := -1, math.MaxInt
		for _, c := range g.InRange[i] {
			if c == victim || contains(g.Adj[i], c) || len(g.Adj[c]) >= e.MaxLinks {
				continue
			}
			seenAt := -1 // never linked sorts first
			if v, ok := L.lastLinked[lk(i, c)]; ok {
				seenAt = v
			}
			if seenAt < oldest {
				oldest, best = seenAt, c
			}
		}
		if best >= 0 {
			g.Adj[i] = append(g.Adj[i], best)
			g.Adj[best] = append(g.Adj[best], i)
			L.formedAt[lk(i, best)] = L.T
			L.lastTraffic[lk(i, best)] = L.T
			L.flash[lk(i, best)] = flashRec{L.T + 6, FlashForm}
		}
		moved = true
	}
	if moved {
		e.bump() // the link set changed; cached distances are stale
	}
}

// SetEndpoints picks the sender and recipient the next tracked send uses.
func (e *Engine) SetEndpoints(src, dst int) {
	e.G.Src, e.G.Dst = src, dst
	if e.L != nil {
		e.L.Track = nil
	}
}

// SendTracked injects the message whose endpoints were picked and follows it.
func (e *Engine) SendTracked() bool {
	L := e.L
	if L == nil || e.G.Src == e.G.Dst {
		return false
	}
	id := L.newID(e.G.Dst)
	L.hold[e.G.Src].set(id, TTL)
	L.seen[e.G.Src].add(id)
	L.Created++
	L.Track = &Track{ID: id, Hops: -1, Born: L.T}
	L.TrackFloodTX = e.RunHeadless(e.G.Src, e.G.Dst, Flood).TX
	return true
}

func (e *Engine) CustodyTotal() int {
	t := 0
	for _, h := range e.L.hold {
		t += len(h)
	}
	return t
}

// Accessors for hosts that draw the mesh.

func (L *Live) HeldCount(i int) int { return len(L.hold[i]) }

func (L *Live) Seen(i, id int) bool { return L.seen[i].has(id) }

// Wanted reports whether some node still wants message id.
func (L *Live) Wanted(id int) bool {
	_, ok := L.dst(id)
	return ok
}

// CopiesAlive counts the copies of message id held or in flight.
func (L *Live) CopiesAlive(id int) int {
	c := 0
	for i := range L.hold {
		if L.hold[i].has(id) {
			c++
		}
	}
	for _, tr := range L.Transfers {
		if tr.ID == id {
			c++
		}
	}
	return c
}

func (L *Live) FlashCount() int { return len(L.flash) }

// flashKeys lists the flashing links ordered by link, so the snapshot and the
// digest see the same order on every run whatever order the map yields.
func (L *Live) flashKeys() []linkKey {
	keys := make([]linkKey, 0, len(L.flash))
	for k := range L.flash {
		keys = append(keys, k)
	}
	for a := 1; a < len(keys); a++ {
		for b := a; b > 0 && (keys[b].lo < keys[b-1].lo || (keys[b].lo == keys[b-1].lo && keys[b].hi < keys[b-1].hi)); b-- {
			keys[b], keys[b-1] = keys[b-1], keys[b]
		}
	}
	return keys
}

// EachFlash visits the links that just formed or dropped, ordered by link.
func (L *Live) EachFlash(f func(a, b, until, kind int)) {
	for _, k := range L.flashKeys() {
		r := L.flash[k]
		f(int(k.lo), int(k.hi), r.until, r.kind)
	}
}
