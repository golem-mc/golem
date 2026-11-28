package server

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/golem-mc/golem/internal/server/datapack"
)

// packErrors collects the elements of the reloadable registries that vanilla's
// codecs refuse, so the load fails once with all of them.
// vanilla: RegistryDataLoader.load, RegistryDataLoader.createReportWithBriefInfo
type packErrors struct {
	elems []string
}

// add records a refused element, or logs it when the server cannot model the
// element, which vanilla would accept.
func (p *packErrors) add(kind, id string, err error) {
	if errors.Is(err, datapack.ErrUnsupported) {
		slog.Warn("Skipping data pack file " + kind + "/" + id + ": " + err.Error())
		return
	}
	slog.Error(fmt.Sprintf(">> Errors in element %s: %s", id, err))
	p.elems = append(p.elems, kind+" "+id+": "+err.Error())
}

// err is nil when no element was refused.
func (p *packErrors) err() error {
	if len(p.elems) == 0 {
		return nil
	}
	return errors.New("Failed to load registries due to errors in elements: " + strings.Join(p.elems, "; "))
}
