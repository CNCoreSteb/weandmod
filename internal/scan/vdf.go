package scan

import (
	"fmt"
	"strings"
)

// vdfValue is either a string or map[string]vdfValue.
type vdfValue = interface{}

// parseVDF parses Valve KeyValues format into nested maps.
// Duplicate keys keep the first occurrence.
func parseVDF(data []byte) (map[string]vdfValue, error) {
	p := &vdfParser{data: string(data)}
	root := map[string]vdfValue{}
	if err := p.parseBlock(root); err != nil {
		return nil, err
	}
	return root, nil
}

type vdfParser struct {
	data string
	pos  int
}

func (p *vdfParser) parseBlock(m map[string]vdfValue) error {
	for {
		key, eof, err := p.next()
		if err != nil {
			return err
		}
		if eof || key == "}" {
			return nil
		}
		val, eof, err := p.next()
		if err != nil {
			return err
		}
		switch {
		case eof || val == "}":
			return fmt.Errorf("vdf: unexpected end after key %q", key)
		case val == "{":
			sub := map[string]vdfValue{}
			if err := p.parseBlock(sub); err != nil {
				return err
			}
			if _, exists := m[key]; !exists {
				m[key] = sub
			}
		default:
			if _, exists := m[key]; !exists {
				m[key] = val
			}
		}
	}
}

// next returns the next token: "{" / "}" / quoted string. eof=true at end.
func (p *vdfParser) next() (string, bool, error) {
	p.skipSpaceAndComments()
	if p.pos >= len(p.data) {
		return "", true, nil
	}
	c := p.data[p.pos]
	if c == '{' || c == '}' {
		p.pos++
		return string(c), false, nil
	}
	if c == '"' {
		s, err := p.quoted()
		return s, false, err
	}
	return "", false, fmt.Errorf("vdf: unexpected character %q at %d", c, p.pos)
}

func (p *vdfParser) quoted() (string, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		p.pos++
		if c == '\\' && p.pos < len(p.data) {
			b.WriteByte(p.data[p.pos])
			p.pos++
			continue
		}
		if c == '"' {
			return b.String(), nil
		}
		b.WriteByte(c)
	}
	return "", fmt.Errorf("vdf: unterminated string")
}

func (p *vdfParser) skipSpaceAndComments() {
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			p.pos++
			continue
		}
		if c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/' {
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		break
	}
}
