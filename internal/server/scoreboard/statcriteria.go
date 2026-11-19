package scoreboard

import (
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/golem-mc/golem/registry"
)

// statKeys is the keys of one statistic type's registry, which the type
// counts over.
func statKeys(statType string) []string {
	switch statType {
	case registry.StatMined:
		return slices.Sorted(maps.Keys(registry.BlockIDs))
	case registry.StatKilled, registry.StatKilledBy:
		return registry.EntityTypes
	case registry.StatCustom:
		return registry.CustomStats
	}
	return registry.Items
}

// dotted writes a resource location the way a criteria name spells it.
// vanilla: Stat.locationToKey
func dotted(location string) string { return strings.ReplaceAll(location, ":", ".") }

// undotted reads a criteria name part as a resource location split at the
// first dot; no dot, or an empty namespace, means minecraft.
// vanilla: Identifier.bySeparator
func undotted(part string) string {
	namespace, path, ok := strings.Cut(part, ".")
	if !ok {
		return "minecraft:" + part
	}
	if namespace == "" {
		namespace = "minecraft"
	}
	return namespace + ":" + path
}

// StatCriteriaName is the name of the criteria that counts one statistic.
// vanilla: Stat.buildName
func StatCriteriaName(statType, key string) string {
	return dotted(statType) + ":" + dotted(key)
}

// lookupStat reads minecraft.<stat_type>:minecraft.<key>.
// vanilla: ObjectiveCriteria.byName, getStat
func lookupStat(name string) (Criteria, bool) {
	typePart, keyPart, ok := strings.Cut(name, ":")
	if !ok {
		return Criteria{}, false
	}
	statType, key := undotted(typePart), undotted(keyPart)
	if _, _, known := registry.StatID(statType, key); !known {
		return Criteria{}, false
	}
	return Criteria{Name: StatCriteriaName(statType, key), StatType: statType, StatKey: key}, true
}

// allNames is every criteria name, the custom ones first.
// vanilla: ObjectiveCriteriaArgument.listSuggestions
var allNames = sync.OnceValue(func() []string {
	names := make([]string, 0, len(customCriteria)+len(registry.Items)*5)
	for _, c := range customCriteria {
		names = append(names, c.Name)
	}
	for _, statType := range registry.StatTypes {
		for _, key := range statKeys(statType) {
			names = append(names, StatCriteriaName(statType, key))
		}
	}
	return names
})
