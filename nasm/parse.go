package nasm

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Parser is used by token parsers to help parse Assembly lines.
type Parser struct {
	// Lines contains the lines that are already scanned.
	Lines Lines

	scan *bufio.Scanner
	curr string
	next *string
}

// NewParser returns a new Parser for the given reader.
func NewParser(r io.Reader) *Parser {
	return &Parser{
		scan: bufio.NewScanner(r),
	}
}

// PrevLine returns the previous line, if any.
func (p *Parser) PrevLine() *Line {
	if len(p.Lines) == 0 {
		return nil
	}
	return &p.Lines[len(p.Lines)-1]
}

// Scan moves the line iterator forward one line.
func (p *Parser) Scan() bool {
	if p.next != nil {
		p.curr = *p.next
		p.next = nil
		return true
	}

	scan := p.scan.Scan()
	p.curr = p.scan.Text()
	return scan
}

// Text returns the current line.
func (p *Parser) Text() string {
	return p.curr
}

// Peek allows the parser to peek to the next line. Peek can only peek to the
// next line; calling it multiple times will return that same line.
func (p *Parser) Peek() (string, bool) {
	if p.next != nil {
		return *p.next, true
	}

	if p.scan.Scan() {
		text := p.scan.Text()
		p.next = &text
		return text, true
	}

	return "", false
}

// Err returns any scanning error.
func (p *Parser) Err() error {
	if p.next != nil {
		// can't be non-nil if there's no error when peaking
		return nil
	}
	return p.scan.Err()
}

// Parse parses multiple lines (i.e. a whole file).
func Parse(r io.Reader) (Lines, error) {
	parser := NewParser(r)

	for lineIdx := 0; parser.Scan(); lineIdx++ {
		lines, err := parseLine(parser)
		if err != nil {
			return parser.Lines, fmt.Errorf("error at line %d: %w", lineIdx+1, err)
		}
		parser.Lines = append(parser.Lines, lines...)
	}

	return parser.Lines, parser.Err()
}

// parseLine parses one source line into one or more logical lines. A line of
// the form "label: <content>" (e.g. "start: cli") is split into a label line
// followed by the logical line that <content> parses to, so that the label and
// instruction may be written on a single line yet still get the standard
// two-line formatting.
func parseLine(scanner *Parser) (Lines, error) {
	line := scanner.Text()
	if line == "" {
		return nil, nil
	}

	if label, rest, ok := splitLeadingLabel(line); ok {
		restLine, err := parseSingleLine(scanner, rest)
		if err != nil {
			return nil, err
		}
		return Lines{
			{Token: LabelToken{Label: label}},
			restLine,
		}, nil
	}

	l, err := parseSingleLine(scanner, line)
	if err != nil {
		return nil, err
	}
	return Lines{l}, nil
}

// parseSingleLine parses a single logical line into one Line.
func parseSingleLine(scanner *Parser, text string) (Line, error) {
	line := text

	var token Token
	var comment CommentToken

	for _, parser := range TokenParsers {
		token, line = parser(scanner, line)

		if _, isComment := token.(CommentToken); isComment {
			comment = token.(CommentToken)
			continue
		}

		if token != nil || line == "" {
			break
		}
	}

	if line != "" {
		if token == nil {
			return Line{}, fmt.Errorf("unknown token %q", line)
		}
		return Line{}, fmt.Errorf("excess text %q", line)
	}

	return Line{
		Token:   token,
		Comment: comment,
	}, nil
}

// splitLeadingLabel reports whether line begins with a single-token label
// ("name:") followed by non-empty content, returning the label and the rest.
// The label must be a single identifier (no whitespace) so that far pointers
// such as "call 3000h:0" and memory operands such as "mov [cs:limit],0" do
// not match. The content after the label may be any valid line (instruction,
// pseudo, memory reference, even a trailing comment).
func splitLeadingLabel(line string) (label, rest string, ok bool) {
	noq := NoQuotes(line, "x")
	byteIdx := strings.IndexRune(noq, ':')
	if byteIdx == -1 {
		return "", "", false
	}

	// NoQuotes preserves the rune count of line, so map the byte index (in the
	// NoQuotes string) to a rune index and slice line by runes. This stays
	// correct even when line contains multi-byte or invalid UTF-8 sequences
	// (e.g. Shift-JIS comments), where the NoQuotes string and line can differ
	// in byte length.
	runeIdx := utf8.RuneCountInString(noq[:byteIdx])
	lr := []rune(line)
	if runeIdx >= len(lr) {
		return "", "", false
	}

	label = strings.TrimSpace(string(lr[:runeIdx]))
	if label == "" || strings.ContainsAny(label, " \t") {
		return "", "", false
	}

	rest = strings.TrimSpace(string(lr[runeIdx+1:]))
	if rest == "" {
		return "", "", false
	}

	return label, rest, true
}
