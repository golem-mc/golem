package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"golang.org/x/term"

	"github.com/golem-mc/golem/launcher/ui"
)

type key int

const (
	keyNone key = iota
	keyUp
	keyDown
	keyToggle
	keyEnter
	keyCancel
)

// pickRow is one line of the picker: a plugin or one of its modules.
type pickRow struct {
	path, name, desc string
	module           bool
	requires         []string
}

// picker is the checkbox list's state; keys drive it and view draws it.
type picker struct {
	rows      []pickRow
	installed map[string]bool
	ticked    map[string]bool
	neededBy  map[string]string // path -> name of the plugin that pulled it in
	cursor    int
	msg       string // shown under the list: why the last key was refused
	done      bool
	cancelled bool
}

func newPicker(c catalog, installed []string) *picker {
	p := &picker{installed: map[string]bool{}, ticked: map[string]bool{}, neededBy: map[string]string{}}
	for _, i := range installed {
		p.installed[i] = true
	}
	for _, pl := range c.Plugins {
		p.rows = append(p.rows, pickRow{path: pl.Path, name: pl.Name, desc: pl.Description, requires: pl.Requires})
		for _, m := range pl.Modules {
			p.rows = append(p.rows, pickRow{path: m.Path, name: lastElement(m.Path), desc: m.Description, module: true})
		}
	}
	return p
}

func (p *picker) row(path string) *pickRow {
	if i := slices.IndexFunc(p.rows, func(r pickRow) bool { return r.path == path }); i >= 0 {
		return &p.rows[i]
	}
	return nil
}

// needs answers the name of a ticked entry that requires path, or "".
func (p *picker) needs(path string) string {
	for _, r := range p.rows {
		if p.ticked[r.path] && slices.Contains(r.requires, path) {
			return r.name
		}
	}
	return ""
}

func (p *picker) tick(r pickRow, by string) {
	if p.installed[r.path] || p.ticked[r.path] {
		return
	}
	p.ticked[r.path] = true
	if by != "" {
		p.neededBy[r.path] = by
	}
	for _, q := range r.requires {
		if dep := p.row(q); dep != nil {
			p.tick(*dep, r.name)
		}
	}
}

func (p *picker) untick(r pickRow) {
	delete(p.ticked, r.path)
	delete(p.neededBy, r.path)
	for _, q := range r.requires {
		dep := p.row(q)
		if dep != nil && p.ticked[q] && p.neededBy[q] == r.name {
			if other := p.needs(q); other != "" {
				p.neededBy[q] = other
			} else {
				p.untick(*dep)
			}
		}
	}
}

func (p *picker) toggle() {
	r := p.rows[p.cursor]
	switch {
	case p.installed[r.path]:
	case p.ticked[r.path]:
		if by := p.needs(r.path); by != "" {
			p.msg = r.name + " is needed by " + by
			return
		}
		p.untick(r)
	default:
		p.tick(r, "")
	}
}

// key applies one key.
func (p *picker) key(k key) {
	p.msg = ""
	switch k {
	case keyUp:
		p.cursor = max(p.cursor-1, 0)
	case keyDown:
		p.cursor = min(p.cursor+1, len(p.rows)-1)
	case keyToggle:
		p.toggle()
	case keyEnter:
		p.done = true
	case keyCancel:
		p.done, p.cancelled = true, true
	}
}

// selection answers the ticked paths, in list order.
func (p *picker) selection() []string {
	var r []string
	for _, row := range p.rows {
		if p.ticked[row.path] {
			r = append(r, row.path)
		}
	}
	return r
}

const pickerHeader = "Select plugins to install (space: toggle, enter: install, q: cancel)"

// view answers the lines of the list (without line ends).
func (p *picker) view(st ui.Style) []string {
	indentOf := func(r pickRow) string {
		if r.module {
			return "     "
		}
		return " "
	}
	nameW := 0
	for _, r := range p.rows {
		nameW = max(nameW, len(indentOf(r))+len(r.name))
	}
	lines := []string{st.Paint(ui.Bold, pickerHeader), ""}
	for i, r := range p.rows {
		cur := " "
		if i == p.cursor {
			cur = st.Paint(ui.Cyan, ">")
		}
		indent, name := indentOf(r), r.name
		box, note := "[ ]", ""
		switch {
		case p.installed[r.path]:
			box = st.Paint(ui.Green, "[✓]")
		case p.ticked[r.path]:
			box = st.Paint(ui.Green, "[x]")
			if by := p.neededBy[r.path]; by != "" {
				note = "  " + st.Paint(ui.Yellow, "(needed by "+by+")")
			}
		}
		pad := strings.Repeat(" ", nameW-len(indent)-len(r.name)+1)
		desc := r.desc
		if p.installed[r.path] {
			desc = st.Paint(ui.Dim, "installed")
		}
		lines = append(lines, cur+indent+box+" "+name+pad+desc+note)
	}
	if p.msg != "" {
		lines = append(lines, "", st.Paint(ui.Red, p.msg))
	}
	return lines
}

// parseKeys turns terminal input into keys. It answers the keys of buf and
// how many bytes it used; an escape sequence cut off at the end is left.
func parseKeys(buf []byte) (keys []key, n int) {
	for n < len(buf) {
		b := buf[n]
		switch b {
		case 0x1b:
			if n+1 == len(buf) {
				return append(keys, keyCancel), n + 1 // a lone Esc
			}
			if buf[n+1] != '[' && buf[n+1] != 'O' {
				keys = append(keys, keyCancel)
				n++
				continue
			}
			if n+2 >= len(buf) {
				return keys, n
			}
			switch buf[n+2] {
			case 'A':
				keys = append(keys, keyUp)
			case 'B':
				keys = append(keys, keyDown)
			}
			n += 3
		case 'k':
			keys, n = append(keys, keyUp), n+1
		case 'j':
			keys, n = append(keys, keyDown), n+1
		case ' ':
			keys, n = append(keys, keyToggle), n+1
		case '\r', '\n':
			keys, n = append(keys, keyEnter), n+1
		case 'q', 3, 4: // q, Ctrl-C, Ctrl-D
			keys, n = append(keys, keyCancel), n+1
		default:
			n++
		}
	}
	return keys, n
}

// canPick answers whether plugin add can open the list: stdin and stdout
// are terminals.
func (c *cli) canPick() bool {
	f, ok := c.out.(*os.File)
	return ok && ui.Terminal(f) && ui.Terminal(os.Stdin)
}

// pickPlugins runs the list and adds what is ticked.
func (c *cli) pickPlugins(ctx context.Context) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	var installed []string
	for _, s := range st.plugins {
		installed = append(installed, s.Path)
	}
	p := newPicker(loadCatalog(ctx), installed)
	if err := c.runPicker(p); err != nil {
		return err
	}
	sel := p.selection()
	if p.cancelled || len(sel) == 0 {
		fmt.Fprintln(c.out, "no plugins added")
		return nil
	}
	return c.pluginAdd(ctx, sel)
}

// runPicker shows p on the terminal until enter or cancel, and restores the
// terminal on every way out.
func (c *cli) runPicker(p *picker) error {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer func() { _ = term.Restore(fd, old) }()
	ui.EnableVTInput(os.Stdin)
	st := c.ui.Out()
	drawn := 0
	draw := func() {
		var b strings.Builder
		if drawn > 0 {
			fmt.Fprintf(&b, "\x1b[%dA", drawn-1)
		}
		b.WriteString("\r\x1b[J")
		lines := p.view(st)
		b.WriteString(strings.Join(lines, "\r\n"))
		drawn = len(lines)
		fmt.Fprint(c.out, "\x1b[?25l"+b.String())
	}
	defer fmt.Fprint(c.out, "\x1b[?25h\r\n")
	buf := make([]byte, 0, 16)
	in := make([]byte, 16)
	for draw(); !p.done; draw() {
		n, err := os.Stdin.Read(in)
		if err != nil {
			p.done, p.cancelled = true, true
			break
		}
		buf = append(buf, in[:n]...)
		keys, used := parseKeys(buf)
		buf = buf[:copy(buf, buf[used:])]
		for _, k := range keys {
			if p.key(k); p.done {
				break
			}
		}
	}
	return nil
}
