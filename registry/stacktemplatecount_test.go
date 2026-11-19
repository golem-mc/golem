package registry

import (
	"github.com/golem-mc/golem/nbt"
)

func countlessEntry(id string) nbt.Compound { return nbt.Compound{"id": nbt.String(id)} }
