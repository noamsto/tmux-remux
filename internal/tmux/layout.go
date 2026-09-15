package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

// NormalizeLayout removes floating panes from a valid window layout and
// recomputes its checksum. tmux includes floats both in the tiled tree and in
// a trailing <...> section, but select-layout rejects that representation.
func NormalizeLayout(s string) (string, error) {
	checksum, body, ok := strings.Cut(s, ",")
	if !ok {
		return "", fmt.Errorf("layout: no checksum separator in %q", s)
	}
	if len(checksum) != 4 {
		return "", fmt.Errorf("layout: checksum %q is not four hex digits", checksum)
	}
	want, err := strconv.ParseUint(checksum, 16, 16)
	if err != nil || uint16(want) != layoutChecksum(body) {
		return "", fmt.Errorf("layout: invalid checksum %q", checksum)
	}
	p := &layoutParser{s: body}
	root, err := p.cell()
	if err != nil {
		return "", err
	}
	var floats []layoutCell
	if p.pos < len(p.s) && p.s[p.pos] == '<' {
		floats, err = p.floatSection()
		if err != nil {
			return "", err
		}
	}
	if p.pos != len(p.s) {
		return "", fmt.Errorf("layout: trailing data %q", p.s[p.pos:])
	}
	if len(floats) == 0 {
		return s, nil
	}
	floatIDs := make(map[string]bool, len(floats))
	for _, f := range floats {
		floatIDs[f.id] = true
	}
	root = pruneFloatingCells(root, floatIDs)
	if root == nil {
		return "", fmt.Errorf("layout: no tiled panes in %q", s)
	}
	var out strings.Builder
	writeLayoutCell(root, &out)
	body = out.String()
	return fmt.Sprintf("%04x,%s", layoutChecksum(body), body), nil
}

type layoutCell struct {
	w, h, x, y int
	id         string
	kind       byte
	children   []*layoutCell
}

type layoutParser struct {
	s   string
	pos int
}

// cell := WxH,X,Y [ , id | { children } | [ children ] ]
func (p *layoutParser) cell() (*layoutCell, error) {
	n := &layoutCell{}
	var err error
	if n.w, err = p.intUntil('x'); err != nil {
		return nil, err
	}
	if n.h, err = p.intUntil(','); err != nil {
		return nil, err
	}
	if n.x, err = p.intUntil(','); err != nil {
		return nil, err
	}
	n.y, err = p.intUntilAny(",{[}]")
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.s) {
		return nil, fmt.Errorf("layout: unexpected end after cell")
	}
	switch p.s[p.pos] {
	case ',':
		p.pos++
		n.id = "%" + p.numRun()
	case '{':
		n.kind = '{'
		return p.split(n, '}')
	case '[':
		n.kind = '['
		return p.split(n, ']')
	}
	return n, nil
}

func (p *layoutParser) split(n *layoutCell, end byte) (*layoutCell, error) {
	p.pos++
	for {
		c, err := p.cell()
		if err != nil {
			return nil, err
		}
		n.children = append(n.children, c)
		if p.pos >= len(p.s) {
			return nil, fmt.Errorf("layout: unterminated split")
		}
		switch p.s[p.pos] {
		case ',':
			p.pos++
		case end:
			p.pos++
			return n, nil
		default:
			return nil, fmt.Errorf("layout: bad split delimiter %q", p.s[p.pos])
		}
	}
}

func (p *layoutParser) floatSection() ([]layoutCell, error) {
	p.pos++
	var floats []layoutCell
	for {
		f, err := p.floatCell()
		if err != nil {
			return nil, err
		}
		floats = append(floats, f)
		if p.pos >= len(p.s) {
			return nil, fmt.Errorf("layout: unterminated float section")
		}
		switch p.s[p.pos] {
		case ',':
			p.pos++
		case '>':
			p.pos++
			return floats, nil
		default:
			return nil, fmt.Errorf("layout: bad float delimiter %q", p.s[p.pos])
		}
	}
}

func (p *layoutParser) floatCell() (layoutCell, error) {
	var c layoutCell
	var err error
	if c.w, err = p.intUntil('x'); err != nil {
		return c, err
	}
	if c.h, err = p.intUntil(','); err != nil {
		return c, err
	}
	if c.x, err = p.intUntil(','); err != nil {
		return c, err
	}
	if c.y, err = p.intUntil(','); err != nil {
		return c, err
	}
	c.id = "%" + p.numRun()
	return c, nil
}

func (p *layoutParser) numRun() string {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	return p.s[start:p.pos]
}

func (p *layoutParser) intUntil(sep byte) (int, error) {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] != sep {
		p.pos++
	}
	if p.pos >= len(p.s) {
		return 0, fmt.Errorf("layout: expected %q", sep)
	}
	v, err := strconv.Atoi(p.s[start:p.pos])
	p.pos++
	return v, err
}

func (p *layoutParser) intUntilAny(seps string) (int, error) {
	start := p.pos
	for p.pos < len(p.s) && !strings.ContainsRune(seps, rune(p.s[p.pos])) {
		p.pos++
	}
	return strconv.Atoi(p.s[start:p.pos])
}

func pruneFloatingCells(n *layoutCell, floatIDs map[string]bool) *layoutCell {
	if len(n.children) == 0 {
		if floatIDs[n.id] {
			return nil
		}
		return n
	}
	kept := make([]*layoutCell, 0, len(n.children))
	for _, c := range n.children {
		if c = pruneFloatingCells(c, floatIDs); c != nil {
			kept = append(kept, c)
		}
	}
	switch len(kept) {
	case 0:
		return nil
	case 1:
		return kept[0]
	default:
		n.children = kept
		return n
	}
}

func writeLayoutCell(n *layoutCell, out *strings.Builder) {
	fmt.Fprintf(out, "%dx%d,%d,%d", n.w, n.h, n.x, n.y)
	if len(n.children) == 0 {
		fmt.Fprintf(out, ",%s", strings.TrimPrefix(n.id, "%"))
		return
	}
	out.WriteByte(n.kind)
	for i, c := range n.children {
		if i > 0 {
			out.WriteByte(',')
		}
		writeLayoutCell(c, out)
	}
	if n.kind == '{' {
		out.WriteByte('}')
	} else {
		out.WriteByte(']')
	}
}

func layoutChecksum(body string) uint16 {
	var sum uint16
	for i := range body {
		sum = (sum >> 1) + ((sum & 1) << 15)
		sum += uint16(body[i])
	}
	return sum
}
