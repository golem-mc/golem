package world

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"sync"
)

// Index the archives without parsing their templates or initializing worldgen.
var builtinTemplateFiles = sync.OnceValue(func() map[string]*zip.File {
	files := map[string]*zip.File{}
	for _, archive := range [][]byte{
		endCityArchive, featureTemplateArchive, iglooArchive, jigsawArchive,
		mansionArchive, netherFossilArchive, oceanRuinArchive,
		ruinedPortalArchive, shipwreckArchive,
	} {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			panic(err)
		}
		for _, f := range zr.File {
			namespace, name, ok := strings.Cut(f.Name, "/structure/")
			if ok && strings.HasSuffix(name, ".nbt") {
				files[namespace+":"+strings.TrimSuffix(name, ".nbt")] = f
			}
		}
	}
	return files
})

// builtinTemplate reads only the requested template; each store owns its parsed copy.
func builtinTemplate(id string) (*Template, bool) {
	f, ok := builtinTemplateFiles()[id]
	if !ok {
		return nil, false
	}
	r, err := f.Open()
	if err != nil {
		panic(err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}
	t, err := ParseTemplate(data)
	if err != nil {
		panic(err)
	}
	return t, true
}
