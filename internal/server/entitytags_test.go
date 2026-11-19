package server

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
)

func TestRejectsEntityTagsAllocations(t *testing.T) {
	var empty nbt.StringSet
	tagged := nbt.NewStringSet([]string{"", "BB", "Aa", "written"})
	cleared := nbt.NewStringSet([]string{"BB", "Aa"})
	cleared.Reset(nil)
	for _, tc := range []struct {
		name string
		q    *command.EntityQuery
		tags nbt.StringSet
		want bool
	}{
		{"nil query", nil, tagged, false},
		{"unfiltered", &command.EntityQuery{}, tagged, false},
		{"present", &command.EntityQuery{Tags: []command.TagFilter{{Name: "Aa"}}}, tagged, false},
		{"absent", &command.EntityQuery{Tags: []command.TagFilter{{Name: "missing"}}}, tagged, true},
		{"inverted present", &command.EntityQuery{Tags: []command.TagFilter{{Name: "BB", Invert: true}}}, tagged, true},
		{"inverted absent", &command.EntityQuery{Tags: []command.TagFilter{{Name: "missing", Invert: true}}}, tagged, false},
		{"empty", &command.EntityQuery{Tags: []command.TagFilter{{}}}, empty, false},
		{"cleared", &command.EntityQuery{Tags: []command.TagFilter{{}}}, cleared, false},
		{"nonempty", &command.EntityQuery{Tags: []command.TagFilter{{}}}, tagged, true},
		{"inverted empty", &command.EntityQuery{Tags: []command.TagFilter{{Invert: true}}}, cleared, true},
		{"inverted nonempty", &command.EntityQuery{Tags: []command.TagFilter{{Invert: true}}}, tagged, false},
		{"all pass", &command.EntityQuery{Tags: []command.TagFilter{{Name: "Aa"}, {Name: "written"}}}, tagged, false},
		{"one fails", &command.EntityQuery{Tags: []command.TagFilter{{Name: "Aa"}, {Name: "missing"}}}, tagged, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(100, func() {
				if got := rejectsEntityTags(tc.q, tc.tags); got != tc.want {
					t.Fatalf("rejectsEntityTags = %v, want %v", got, tc.want)
				}
			})
			if allocs != 0 {
				t.Fatalf("tag rejection allocated %g times, want 0", allocs)
			}
		})
	}
}

func TestWithoutTeamRetainsCompoundOrder(t *testing.T) {
	t.Parallel()
	saved := &nbt.OrderedCompound{}
	for _, key := range []string{"BB", "Aa", "Team"} {
		saved.Set(key, nbt.String(key))
	}
	rider := nbt.Snapshot(saved).(*nbt.OrderedCompound)
	saved.Set("Passengers", nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{rider}})
	withoutTeam(saved)
	if _, exists := saved.Entries()["Team"]; exists {
		t.Fatal("carried entity kept its team")
	}
	if _, exists := rider.Entries()["Team"]; exists {
		t.Fatal("carried rider kept its team")
	}
	if got := nbt.HashOrderedKeys(rider); !slices.Equal(got, []string{"BB", "Aa"}) {
		t.Fatalf("carried rider's key order = %v, want BB then Aa", got)
	}
	// Copies iterate the ordered key history, which must omit deleted entries.
	if copy := nbt.Copy(saved).(nbt.CompoundTag); len(copy.Entries()) != 3 {
		t.Fatalf("copied carried entity has %d fields, want 3", len(copy.Entries()))
	}
}

// TestTagsOfTagReadsTheVanillaSet checks the read of a saved Tags list:
// duplicates collapse to one tag and a list longer than addTag takes reads as
// none, the sizeLimitedListOf the vanilla read caps it with.
// vanilla: Entity's read of Tags, Entity.addTag
func TestTagsOfTagReadsTheVanillaSet(t *testing.T) {
	t.Parallel()
	dup := nbt.Compound{"Tags": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{
		nbt.String("a"), nbt.String("a"), nbt.String("b"),
	}}}
	if got := tagsOfTag(dup); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("tagsOfTag keeps %v, want a and b", got)
	}
	over := make([]nbt.Tag, player.TagCap+1)
	for i := range over {
		over[i] = nbt.String("tag")
	}
	long := nbt.Compound{"Tags": nbt.List{Elem: nbt.TypeString, Items: over}}
	if got := tagsOfTag(long); got != nil {
		t.Errorf("tagsOfTag of an over-long list keeps %v, want none", got)
	}
}
