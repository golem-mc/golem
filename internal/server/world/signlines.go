package world

import "github.com/golem-mc/golem/nbt"

// Lines copies the unfiltered components. Call it inside the entity's Use.
func (t SignText) Lines() (lines [4]nbt.Tag) {
	for i, text := range t.Messages {
		if t.raw[i] != nil {
			lines[i] = nbt.Copy(t.raw[i])
		} else {
			lines[i] = nbt.String(text)
		}
	}
	return
}

// SetLines replaces both the unfiltered and filtered lines, preserving dye and glow.
// Call it inside the entity's Use.
func (t *SignText) SetLines(lines [4]nbt.Tag) {
	t.filtered = nil
	for i, line := range lines {
		t.raw[i] = nbt.Copy(line)
		t.Messages[i], _ = signLiteral(line)
	}
}
