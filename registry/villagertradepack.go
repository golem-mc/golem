package registry

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
)

// tradeSetDef is a trade set before its tag is resolved to trades.
type tradeSetDef struct {
	Amount         int
	Duplicates     bool
	Tag            string // the villager_trade tag the set draws from
	RandomSequence string
}

// PackTradeSet is a trade_set file: the trades holder set is a tag or a list
// of trade ids.
// vanilla: TradeSet
type PackTradeSet struct {
	Amount         int
	Duplicates     bool
	Trades         []PackTagEntry
	RandomSequence string
}

// tradeSets is the built-in trade sets, resolved from the generated tables.
var tradeSets = func() map[string]TradeSet {
	sets, _ := BuildTradeSets(nil, nil, nil)
	return sets
}()

// packTradeSets are the sets the data packs define or change; they win over
// tradeSets.
var packTradeSets atomic.Pointer[map[string]TradeSet]

// ApplyPackTradeSets makes sets, by name, replace the built-in trade sets of
// the same name for FindTradeSet. nil restores the built-in ones.
func ApplyPackTradeSets(sets map[string]TradeSet) {
	if sets == nil {
		packTradeSets.Store(nil)
		return
	}
	packTradeSets.Store(&sets)
}

// tradeSetName names a trade set the way FindTradeSet does: without the
// minecraft namespace.
func tradeSetName(id string) string { return strings.TrimPrefix(id, "minecraft:") }

// BuildTradeSets resolves every trade set from the built-in trades, tags and
// sets with the packs' villager_trade files (by id), villager_trade tags and
// trade_set files (by id) layered over them. A set whose trades do not all
// load is left out and described in the returned messages.
// vanilla: TagLoader.build, TradeSet
func BuildTradeSets(trades map[string]VillagerTrade, tags map[string]PackTag, sets map[string]PackTradeSet) (map[string]TradeSet, []string) {
	exists := func(id string) bool {
		if _, ok := trades[id]; ok {
			return true
		}
		_, ok := builtinTrades[id]
		return ok
	}
	base := make(map[string][]PackTagEntry, len(builtinTradeTags))
	order := make([]string, 0, len(builtinTradeTags))
	for name, values := range builtinTradeTags {
		entries := make([]PackTagEntry, len(values))
		for i, v := range values {
			id, tag := strings.CutPrefix(v, "#")
			entries[i] = PackTagEntry{ID: id, Tag: tag, Required: true}
		}
		base[name] = entries
		order = append(order, name)
	}
	sort.Strings(order)
	res, problems := resolveTags(base, order, tags, exists)

	defs := make(map[string]PackTradeSet, len(builtinTradeSets)+len(sets))
	for name, d := range builtinTradeSets {
		defs[name] = PackTradeSet{Amount: d.Amount, Duplicates: d.Duplicates, RandomSequence: d.RandomSequence,
			Trades: []PackTagEntry{{ID: d.Tag, Tag: true, Required: true}}}
	}
	for id, d := range sets {
		defs[tradeSetName(id)] = d
	}
	out := make(map[string]TradeSet, len(defs))
	for name, d := range defs {
		var ids []string
		ok := true
		for _, e := range d.Trades {
			members := []string{e.ID}
			if e.Tag {
				var found bool
				if members, found = res.tags[e.ID]; !found {
					ok = false
					problems = append(problems, fmt.Sprintf("trade set %s: tag #%s did not load", name, e.ID))
					break
				}
			} else if !exists(e.ID) {
				ok = false
				problems = append(problems, fmt.Sprintf("trade set %s: no trade %s", name, e.ID))
				break
			}
			for _, m := range members {
				if !slices.Contains(ids, m) {
					ids = append(ids, m)
				}
			}
		}
		if !ok {
			continue
		}
		set := TradeSet{Amount: d.Amount, Duplicates: d.Duplicates, RandomSequence: d.RandomSequence, Trades: make([]VillagerTrade, len(ids))}
		for i, id := range ids {
			if t, ok := trades[id]; ok {
				set.Trades[i] = t
			} else {
				set.Trades[i] = builtinTrades[id]
			}
		}
		out[name] = set
	}
	return out, problems
}
