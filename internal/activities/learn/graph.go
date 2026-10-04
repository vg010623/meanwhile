package learn

import (
	"sort"

	"github.com/vg010623/meanwhile/internal/curriculum"
)

// graph answers "what is this user ready for?" over the prerequisite graph.
// It recommends; it never blocks: any concept can be opened.
type graph struct {
	cur        *curriculum.Curriculum
	dependents map[string][]string // concept → concepts that list it as a prerequisite
	unlocks    map[string]int      // concept → how many concepts depend on it, directly or not
}

func newGraph(cur *curriculum.Curriculum) *graph {
	g := &graph{cur: cur, dependents: map[string][]string{}, unlocks: map[string]int{}}
	for _, c := range cur.Concepts {
		for _, p := range c.Prerequisites {
			g.dependents[p] = append(g.dependents[p], c.ID)
		}
	}
	for _, c := range cur.Concepts {
		seen := map[string]bool{}
		var walk func(id string)
		walk = func(id string) {
			for _, d := range g.dependents[id] {
				if !seen[d] {
					seen[d] = true
					walk(d)
				}
			}
		}
		walk(c.ID)
		g.unlocks[c.ID] = len(seen)
	}
	return g
}

// unmet returns the prerequisites of c that are not done yet.
func (g *graph) unmet(c *curriculum.Concept, prog map[string]Progress) []*curriculum.Concept {
	var out []*curriculum.Concept
	for _, id := range c.Prerequisites {
		if !prog[id].Status.Done() {
			if p, ok := g.cur.Concept(id); ok {
				out = append(out, p)
			}
		}
	}
	return out
}

// ready reports whether c is not done and all its prerequisites are.
func (g *graph) ready(c *curriculum.Concept, prog map[string]Progress) bool {
	return !prog[c.ID].Status.Done() && len(g.unmet(c, prog)) == 0
}

// recommend lists up to n ready concepts, most useful first: the ones that
// unlock the most others, then by the author's `order`, then by title.
func (g *graph) recommend(prog map[string]Progress, exclude string, n int) []*curriculum.Concept {
	var out []*curriculum.Concept
	for _, c := range g.cur.Concepts {
		if c.ID != exclude && g.ready(c, prog) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if g.unlocks[a.ID] != g.unlocks[b.ID] {
			return g.unlocks[a.ID] > g.unlocks[b.ID]
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Title < b.Title
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// newlyUnlocked lists concepts that became ready because id was finished.
func (g *graph) newlyUnlocked(id string, prog map[string]Progress) []*curriculum.Concept {
	var out []*curriculum.Concept
	for _, d := range g.dependents[id] {
		if c, ok := g.cur.Concept(d); ok && g.ready(c, prog) {
			out = append(out, c)
		}
	}
	return out
}

// byTrack groups every concept by track for "browse all", in a stable order:
// tracks by their first concept's order, concepts by order then title.
func (g *graph) byTrack() (tracks []string, concepts map[string][]*curriculum.Concept) {
	concepts = map[string][]*curriculum.Concept{}
	first := map[string]int{}
	for _, c := range g.cur.Concepts {
		t := c.Track
		if t == "" {
			t = "other"
		}
		if _, ok := concepts[t]; !ok {
			tracks = append(tracks, t)
			first[t] = c.Order
		}
		concepts[t] = append(concepts[t], c)
		first[t] = min(first[t], c.Order)
	}
	sort.SliceStable(tracks, func(i, j int) bool { return first[tracks[i]] < first[tracks[j]] })
	for _, list := range concepts {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Order != list[j].Order {
				return list[i].Order < list[j].Order
			}
			return list[i].Title < list[j].Title
		})
	}
	return tracks, concepts
}

// cardIndex finds the saved card, preferring its ID, falling back to the
// saved index if an author removed or renamed the card.
func cardIndex(c *curriculum.Concept, p Progress) int {
	for i, card := range c.Cards {
		if card.ID == p.CardID {
			return i
		}
	}
	return min(max(p.CardIndex, 0), len(c.Cards)-1)
}
