// trace runs scenarios (JSON lines on stdin) on the Go engine and prints the
// same checkpoint lines reference/harness.mjs prints for the original page.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"

	"websim/engine"
)

// ruleNames is every rule in the engine's order. The topology scenarios cover
// only referenceRules: the ones the original page has.
var (
	ruleNames      = []string{"flood", "f3", "hmulti", "amigo", "amigo-udx", "oracle"}
	referenceRules = []string{"flood", "f3", "hmulti", "oracle"}
)

type send struct{ At, Src, Dst int }

type skChange struct {
	At          int
	Alpha, Beta float64
	F           int
}

type scenario struct {
	Kind        string
	Name        string
	N, Seed     int
	World, Cell int
	MaxLinks    int
	MaxAge      *int
	Rule        string
	SK          *struct {
		Alpha, Beta float64
		F           int
	}
	TrafficSeed int64
	Ticks       int
	Every       int
	Sends       []send
	SkChanges   []skChange
}

func ruleOf(name string) engine.Rule {
	for i, n := range ruleNames {
		if n == name {
			return engine.Rule(i)
		}
	}
	panic("unknown rule " + name)
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for sc.Scan() {
		var s scenario
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			panic(err)
		}
		run(out, &s)
	}
}

func run(out *bufio.Writer, s *scenario) {
	P := func(a ...any) {
		fmt.Fprint(out, s.Name)
		for _, v := range a {
			fmt.Fprint(out, " ", v)
		}
		fmt.Fprintln(out)
	}
	hex := func(v uint32) string { return fmt.Sprintf("%08x", v) }
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}

	e := engine.New()
	e.Geo = engine.Geometry{World: s.World, Cell: s.Cell}
	e.MaxLinks = s.MaxLinks
	if s.MaxAge != nil {
		e.MaxAge = *s.MaxAge
	}
	if s.SK != nil {
		e.SetSK(s.SK.Alpha, s.SK.Beta, s.SK.F)
	}
	e.Rebuild(s.N, s.Seed)
	g := e.G
	P("topo", len(g.Nodes), hex(e.TopologyDigest()))

	if s.Kind == "topology" {
		pr := engine.NewRNG(int64(s.Seed)*31 + 7)
		n := len(g.Nodes)
		pairs := [][2]int{{g.Src, g.Dst}}
		for k := 0; k < 3; k++ {
			a := int(pr.Float64() * float64(n))
			b := int(pr.Float64() * float64(n))
			if a != b {
				pairs = append(pairs, [2]int{a, b})
			}
		}
		for _, name := range referenceRules {
			rule := ruleOf(name)
			P("targets", name, hex(e.TargetsDigest(g.Dst, rule)))
			e.Rule = rule
			lanes := make([]int, len(g.Nodes))
			for i := range lanes {
				lanes[i] = e.LaneCount(i)
			}
			P("lanes", name, hex(engine.IntsDigest(lanes)))
			if rule.Hierarchical() {
				entries := e.EntryPoints()
				P("entries", name, len(entries), hex(engine.IntsDigestRaw(entries)))
				is := make([]int, len(g.Nodes))
				for i := range is {
					is[i] = b2i(e.IsEntryPoint(i))
				}
				P("isentry", name, hex(engine.IntsDigestRaw(is)))
			}
			for _, p := range pairs {
				r := e.RunHeadless(p[0], p[1], rule)
				P("headless", name, p[0], p[1], r.TX, r.Hops, b2i(r.Delivered))
			}
		}
		return
	}

	e.Rule = ruleOf(s.Rule)
	e.StartLive(s.TrafficSeed)
	P("tick", 0, hex(e.LiveDigest()))
	for t := 1; t <= s.Ticks; t++ {
		e.Step()
		if t%s.Every == 0 || t == s.Ticks {
			P("tick", t, hex(e.LiveDigest()))
		}
		for _, sd := range s.Sends {
			if sd.At != t {
				continue
			}
			e.SetEndpoints(sd.Src, sd.Dst)
			ok := e.SendTracked()
			P("send", t, b2i(ok), hex(e.LiveDigest()))
		}
		for _, c := range s.SkChanges {
			if c.At == t {
				e.SetSK(c.Alpha, c.Beta, c.F)
				P("sk", t, hex(e.LiveDigest()))
			}
		}
	}
	L := e.L
	for i, r := range L.Sends {
		copies := 0.0
		if r.CopyTks != 0 {
			copies = float64(r.CopiesSum) / float64(r.CopyTks)
		}
		P("rec", i, r.Outcome, r.Hops, r.Ticks, r.TX, fmt.Sprintf("%016x", math.Float64bits(copies)))
	}
	P("end", L.Created, L.Delivered, L.TX, L.Rotations, L.AgedOut, e.CustodyTotal())
}
