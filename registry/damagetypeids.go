package registry

import (
	"slices"
	"strings"
)

// DamageType is a damage registry ID plus one; zero means no damage source.
type DamageType int32

// Name returns the registry name, or "" for an absent or invalid source.
func (d DamageType) Name() string {
	s := damageTypesNow.Load()
	if d <= 0 || int(d) >= len(s.names) {
		return ""
	}
	return s.names[d]
}

// String returns Name.
func (d DamageType) String() string { return d.Name() }

// DamageTypeByName resolves a name at command and data loading boundaries.
func DamageTypeByName(name string) (DamageType, bool) {
	d, ok := damageTypesNow.Load().ids[name]
	return d, ok
}

// Info returns the type's data pack fields.
func (d DamageType) Info() DamageTypeInfo {
	s := damageTypesNow.Load()
	if d <= 0 || int(d) >= len(s.info) {
		return DamageTypeInfo{}
	}
	return s.info[d]
}

type damageTypeState struct {
	names []string
	ids   map[string]DamageType
	info  []DamageTypeInfo
}

func newDamageTypeState() *damageTypeState {
	names := append([]string(nil), damageTypeNames[:]...)
	if reg, ok := Get("minecraft:damage_type"); ok {
		names = append(names[:1], reg.Entries...)
	}
	packs := packDamageTypes.Load()
	if packs != nil {
		var extra []string
		for name := range *packs {
			if !slices.Contains(names, name) {
				extra = append(extra, name)
			}
		}
		slices.Sort(extra)
		names = append(names, extra...)
	}
	s := &damageTypeState{names: names, ids: make(map[string]DamageType, len(names)), info: make([]DamageTypeInfo, len(names))}
	for i, name := range names {
		if i == 0 {
			continue
		}
		s.ids[name] = DamageType(i)
		info, ok := DamageTypes[name]
		if !ok {
			_, id, _ := strings.Cut(name, ":")
			info.MessageID = id
		}
		if packs != nil {
			if override, ok := (*packs)[name]; ok {
				info = override
			}
		}
		s.info[i] = info
	}
	return s
}
