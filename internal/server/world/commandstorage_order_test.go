package world

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestCommandStorageRetainsCollisionOrderAfterSave(t *testing.T) {
	root := t.TempDir()
	storage := NewCommandStorages(root)
	tag, _, pe := nbt.ParseOrderedValueTag(`{big:1,s:1,t:1,v:1,f:1,w:1,em:1,k:1,l:1,m:1,bo:1,n:1,nested:{v:2,f:3}}`)
	if pe != nil {
		t.Fatal(pe)
	}
	storage.Set("test:order", tag.(nbt.CompoundTag))
	storage.Save()
	for _, source := range []*CommandStorages{storage, NewCommandStorages(root)} {
		source.Read("test:order", func(c nbt.CompoundTag) {
			if got := nbt.HashOrderedKeys(c); !slices.Equal(got, strings.Fields("big f em k l m bo nested n s t v w")) {
				t.Errorf("keys = %v", got)
			}
			nested := c.Entries()["nested"].(nbt.CompoundTag)
			if got := nbt.HashOrderedKeys(nested); !slices.Equal(got, []string{"v", "f"}) {
				t.Errorf("nested keys = %v", got)
			}
		})
	}
}
