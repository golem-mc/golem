package entity

import (
	"bytes"
	"slices"
	"sort"

	"github.com/google/uuid"
)

// GossipType is one of a villager's opinions about a player, in
// GossipType's declaration order.
type GossipType uint8

// The gossip types. vanilla: GossipType
const (
	GossipMajorNegative GossipType = iota
	GossipMinorNegative
	GossipMinorPositive
	GossipMajorPositive
	GossipTrading
)

type gossipTypeInfo struct {
	id                                         string
	weight, max, decayPerDay, decayPerTransfer int32
}

// gossipTypes is GossipType's key, weight, cap and decays.
var gossipTypes = [...]gossipTypeInfo{
	{"major_negative", -5, 100, 10, 10}, {"minor_negative", -1, 200, 20, 20},
	{"minor_positive", 1, 25, 1, 5}, {"major_positive", 5, 20, 0, 20}, {"trading", 1, 25, 2, 20},
}

// Gossips is a villager's opinion of each target by type; 0 is absent and a
// present value is at least 2. vanilla: GossipContainer
type Gossips map[uuid.UUID][len(gossipTypes)]int32

// add is GossipContainer.add.
func (g Gossips) add(target uuid.UUID, typ GossipType, amount int32) {
	vals := g[target]
	t := gossipTypes[typ]
	if old := vals[typ]; old == 0 {
		vals[typ] = amount
	} else if sum := old + amount; sum > t.max {
		vals[typ] = max(t.max, old)
	} else {
		vals[typ] = sum
	}
	vals[typ] = min(vals[typ], t.max)
	if vals[typ] < 2 {
		vals[typ] = 0
	}
	g.store(target, vals)
}

// store writes a target's values back, dropping it when none are left.
func (g Gossips) store(target uuid.UUID, vals [len(gossipTypes)]int32) {
	if vals == [len(gossipTypes)]int32{} {
		delete(g, target)
		return
	}
	g[target] = vals
}

// Reputation is the weighted sum of the target's values.
func (g Gossips) Reputation(target uuid.UUID) int32 {
	var sum int32
	for i, v := range g[target] {
		sum += v * gossipTypes[i].weight
	}
	return sum
}

// decay is GossipContainer.decay, one day's worth.
func (g Gossips) decay() {
	for target, vals := range g {
		for i, v := range vals {
			if v -= gossipTypes[i].decayPerDay; v < 2 {
				v = 0
			}
			vals[i] = v
		}
		g.store(target, vals)
	}
}

type gossipEntry struct {
	target uuid.UUID
	typ    GossipType
	value  int32
}

// entries lists the values ordered by UUID bytes, then type.
func (g Gossips) entries() []gossipEntry {
	var out []gossipEntry
	for target, vals := range g {
		for i, v := range vals {
			if v != 0 {
				out = append(out, gossipEntry{target, GossipType(i), v})
			}
		}
	}
	slices.SortFunc(out, func(a, b gossipEntry) int {
		if c := bytes.Compare(a.target[:], b.target[:]); c != 0 {
			return c
		}
		return int(a.typ) - int(b.typ)
	})
	return out
}

// transferFrom is GossipContainer.transferFrom: maxCount weighted draws from
// src, each drawn entry once, less its decay per transfer.
func (g Gossips) transferFrom(src Gossips, intn func(int) int, maxCount int) {
	list := src.entries()
	if len(list) == 0 {
		return
	}
	ranges := make([]int, len(list))
	total := 0
	for i, e := range list {
		total += int(abs32(e.value * gossipTypes[e.typ].weight))
		ranges[i] = total
	}
	if total == 0 {
		return
	}
	drawn := map[int]bool{}
	for range maxCount {
		choice := intn(total)
		drawn[sort.Search(len(ranges), func(i int) bool { return ranges[i] > choice })] = true
	}
	for i := range drawn {
		e := list[i]
		if v := e.value - gossipTypes[e.typ].decayPerTransfer; v >= 2 {
			vals := g[e.target]
			vals[e.typ] = max(vals[e.typ], v)
			g[e.target] = vals
		}
	}
}
