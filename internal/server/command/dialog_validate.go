package command

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func dialogCheck(c nbt.Compound) string { return registry.DialogMalformed(c) }
func dialogRefCheck(tag nbt.Tag) string { return registry.DialogRefMalformed(tag) }
