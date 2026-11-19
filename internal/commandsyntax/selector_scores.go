package commandsyntax

// expect reads the one character the option's syntax requires next.
func (p *selParser) expect(c byte) *Error {
	if !p.r.canRead() || p.r.peek() != c {
		return &Error{Key: keyExpectedSymbol, With: []string{string(c)}, Cursor: p.absolute()}
	}
	p.r.skip()
	return nil
}

// readBoolean reads true or false the way StringReader.readBoolean does.
func (p *selParser) readBoolean() (bool, *Error) {
	start := p.r.pos
	if !p.r.canRead() {
		return false, &Error{Key: keyBoolExpected, Cursor: p.absolute()}
	}
	switch value := p.r.readUnquoted(); value {
	case "true", "false":
		return value == "true", nil
	case "":
		return false, &Error{Key: keyBoolExpected, Cursor: p.inputStart + start}
	default:
		return false, &Error{Key: keyBoolInvalid, With: []string{value}, Cursor: p.inputStart + start}
	}
}

// skipListSeparator skips the comma after a list entry, where one stands.
func (p *selParser) skipListSeparator() {
	if p.r.canRead() && p.r.peek() == ',' {
		p.r.skip()
	}
}

// parseScores keeps the last range when an objective is repeated.
// vanilla: EntitySelectorOptions scores option
func (p *selParser) parseScores() *Error {
	if err := p.expect('{'); err != nil {
		return err
	}
	p.r.skipWhitespace()
	expected := map[string]*Bounds{}
	for p.r.canRead() && p.r.peek() != '}' {
		p.r.skipWhitespace()
		name := p.r.readUnquoted()
		p.r.skipWhitespace()
		if err := p.expect('='); err != nil {
			return err
		}
		p.r.skipWhitespace()
		value, err := p.r.parseRange(ParseIntEnd, "parsing.int.invalid", false)
		if err != nil {
			err.Cursor += p.inputStart // the range reads within the selector
			return err
		}
		expected[name] = value
		p.r.skipWhitespace()
		p.skipListSeparator()
	}
	if err := p.expect('}'); err != nil {
		return err
	}
	if len(expected) > 0 {
		p.sel.Scores = expected
	}
	p.scoresSet = true
	return nil
}

// AdvancementWant requires completion or named criteria states.
type AdvancementWant struct {
	Done     bool
	Criteria map[string]bool
}

// parseAdvancements narrows nonempty requirements to players.
// vanilla: EntitySelectorOptions advancements option
func (p *selParser) parseAdvancements() *Error {
	if err := p.expect('{'); err != nil {
		return err
	}
	p.r.skipWhitespace()
	expected := map[string]*AdvancementWant{}
	for p.r.canRead() && p.r.peek() != '}' {
		p.r.skipWhitespace()
		mark := p.r.pos
		id, err := p.r.readPredicateID()
		if err != nil {
			return &Error{Key: err.Key, Cursor: p.inputStart + mark}
		}
		p.r.skipWhitespace()
		if err := p.expect('='); err != nil {
			return err
		}
		p.r.skipWhitespace()
		want := &AdvancementWant{}
		if p.r.canRead() && p.r.peek() == '{' {
			if want.Criteria, err = p.parseCriteria(); err != nil {
				return err
			}
		} else if want.Done, err = p.readBoolean(); err != nil {
			return err
		}
		expected[qualify(id)] = want
		p.r.skipWhitespace()
		p.skipListSeparator()
	}
	if err := p.expect('}'); err != nil {
		return err
	}
	if len(expected) > 0 {
		p.addFilter(Filter{Kind: FilterAdvancements, Advancements: expected})
		p.sel.IncludesEntities = false
	}
	p.advanceSet = true
	return nil
}

func (p *selParser) parseCriteria() (map[string]bool, *Error) {
	p.r.skipWhitespace()
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	p.r.skipWhitespace()
	criteria := map[string]bool{}
	for p.r.canRead() && p.r.peek() != '}' {
		p.r.skipWhitespace()
		name := p.r.readUnquoted()
		p.r.skipWhitespace()
		if err := p.expect('='); err != nil {
			return nil, err
		}
		p.r.skipWhitespace()
		value, err := p.readBoolean()
		if err != nil {
			return nil, err
		}
		criteria[name] = value
		p.r.skipWhitespace()
		p.skipListSeparator()
	}
	p.r.skipWhitespace()
	if err := p.expect('}'); err != nil {
		return nil, err
	}
	p.r.skipWhitespace()
	return criteria, nil
}

// TeamWant names a team (empty for none), optionally inverted.
type TeamWant struct {
	Name   string
	Invert bool
}
