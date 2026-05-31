// Copyright 2026 The mk Authors
// SPDX-License-Identifier: Apache-2.0

package mk

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Parse parses an mkfile from a reader.
func Parse(r io.Reader) (*File, error) {
	// Read all lines upfront so we can peek/backtrack.
	var rawLines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		rawLines = append(rawLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Join line continuations: lines ending with \ are merged with the next.
	var lines []string
	for i := 0; i < len(rawLines); i++ {
		line := rawLines[i]
		for strings.HasSuffix(line, "\\") && i+1 < len(rawLines) {
			line = line[:len(line)-1] + rawLines[i+1]
			i++
		}
		lines = append(lines, line)
	}

	p := &parser{lines: lines}
	stmts, err := p.parseBlock(false)
	if err != nil {
		return nil, err
	}
	return &File{Stmts: stmts}, nil
}

type parser struct {
	lines []string
	pos   int
}

func (p *parser) peek() (string, bool) {
	if p.pos >= len(p.lines) {
		return "", false
	}
	return p.lines[p.pos], true
}

func (p *parser) next() (string, int, bool) {
	if p.pos >= len(p.lines) {
		return "", 0, false
	}
	line := p.lines[p.pos]
	lineNum := p.pos + 1
	p.pos++
	return line, lineNum, true
}

func (p *parser) parseBlock(inConditional bool) ([]Node, error) {
	var stmts []Node
	for {
		line, ok := p.peek()
		if !ok {
			break
		}
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and full-line comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			p.pos++
			continue
		}

		// Strip inline comments
		if idx := strings.Index(trimmed, " #"); idx >= 0 {
			trimmed = strings.TrimRight(trimmed[:idx], " \t")
		}

		// End of conditional block
		if inConditional && (trimmed == "end" || trimmed == "else" || strings.HasPrefix(trimmed, "elif ")) {
			break
		}

		// Indented line outside a rule
		if line[0] == ' ' || line[0] == '\t' {
			if inConditional {
				// Inside a conditional, indented lines are the body
				trimmed = strings.TrimSpace(line)
			} else {
				return nil, fmt.Errorf("line %d: unexpected indented line outside a rule", p.pos+1)
			}
		}

		node, err := p.parseStatement(trimmed)
		if err != nil {
			return nil, err
		}
		if node != nil {
			stmts = append(stmts, node)
		}
	}
	return stmts, nil
}

func (p *parser) parseStatement(trimmed string) (Node, error) {
	_, lineNum, _ := p.next() // consume the line

	// Include
	if strings.HasPrefix(trimmed, "include ") {
		n, err := parseInclude(trimmed, lineNum)
		return n, err
	}

	// Conditional
	if strings.HasPrefix(trimmed, "if ") {
		return p.parseConditional(trimmed, lineNum)
	}

	// Function definition
	if strings.HasPrefix(trimmed, "fn ") {
		return p.parseFuncDef(trimmed, lineNum)
	}

	// Config definition
	if strings.HasPrefix(trimmed, "config ") && strings.HasSuffix(trimmed, ":") {
		return p.parseConfigDef(trimmed, lineNum)
	}

	// Loop
	if strings.HasPrefix(trimmed, "for ") && strings.HasSuffix(trimmed, ":") {
		return p.parseLoop(trimmed, lineNum)
	}

	// Lazy variable
	if rest, ok := strings.CutPrefix(trimmed, "lazy "); ok {
		if name, value, ok := parseAssign(rest); ok {
			if containsVarRef(value, name) {
				return nil, fmt.Errorf("line %d: recursive definition: %s references itself", lineNum, name)
			}
			return VarAssign{Name: name, Op: OpSet, Value: value, Lazy: true, Line: lineNum}, nil
		}
	}

	// Variable assignment
	if name, value, ok := parseAssign(trimmed); ok {
		if containsVarRef(value, name) {
			return nil, fmt.Errorf("line %d: recursive definition: %s references itself", lineNum, name)
		}
		return VarAssign{Name: name, Op: OpSet, Value: value, Line: lineNum}, nil
	}
	if name, value, ok := parseAppend(trimmed); ok {
		return VarAssign{Name: name, Op: OpAppend, Value: value, Line: lineNum}, nil
	}
	if name, value, ok := parseCondAssign(trimmed); ok {
		return VarAssign{Name: name, Op: OpCondSet, Value: value, Line: lineNum}, nil
	}

	// Rule or task
	if isTask, keep, fingerprint, depsFormat, scan, scanFormat, writes, reads, targets, prereqs, orderOnly, ok := parseRuleHeader(trimmed); ok {
		recipe := p.parseRecipe()
		return Rule{
			Targets:          targets,
			Prereqs:          prereqs,
			OrderOnlyPrereqs: orderOnly,
			Recipe:           recipe,
			IsTask:           isTask,
			Keep:             keep,
			Fingerprint:      fingerprint,
			DepsFormat:       depsFormat,
			Scan:             scan,
			ScanFormat:       scanFormat,
			Writes:           writes,
			Reads:            reads,
			Line:             lineNum,
		}, nil
	}

	return nil, fmt.Errorf("line %d: unrecognized syntax: %s", lineNum, trimmed)
}

func (p *parser) parseFuncDef(line string, lineNum int) (Node, error) {
	// fn name(param1, param2):
	rest := strings.TrimPrefix(line, "fn ")

	parenOpen := strings.IndexByte(rest, '(')
	parenClose := strings.IndexByte(rest, ')')
	if parenOpen < 0 || parenClose < 0 || parenClose < parenOpen {
		return nil, fmt.Errorf("line %d: invalid function definition: %s", lineNum, line)
	}

	name := strings.TrimSpace(rest[:parenOpen])
	paramStr := rest[parenOpen+1 : parenClose]

	var params []string
	for _, p := range strings.Split(paramStr, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			params = append(params, p)
		}
	}

	// Read indented body lines, looking for "return ..."
	var body string
	for {
		bodyLine, ok := p.peek()
		if !ok {
			break
		}
		if bodyLine == "" {
			p.pos++
			continue
		}
		if bodyLine[0] != ' ' && bodyLine[0] != '\t' {
			break
		}
		p.pos++
		trimmed := strings.TrimSpace(bodyLine)
		if after, ok := strings.CutPrefix(trimmed, "return "); ok {
			body = strings.TrimSpace(after)
		}
	}

	if body == "" {
		return nil, fmt.Errorf("line %d: function %q has no return statement", lineNum, name)
	}

	return FuncDef{Name: name, Params: params, Body: body, Line: lineNum}, nil
}

func (p *parser) parseConfigDef(line string, lineNum int) (Node, error) {
	// config name:
	name := strings.TrimSuffix(strings.TrimPrefix(line, "config "), ":")
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("line %d: config requires a name", lineNum)
	}

	cfg := ConfigDef{Name: name, Line: lineNum}

	// Read indented body lines
	for {
		bodyLine, ok := p.peek()
		if !ok {
			break
		}
		if bodyLine == "" {
			p.pos++
			continue
		}
		if bodyLine[0] != ' ' && bodyLine[0] != '\t' {
			break
		}
		p.pos++
		trimmed := strings.TrimSpace(bodyLine)
		if trimmed == "" {
			continue
		}

		if rest, ok := strings.CutPrefix(trimmed, "excludes "); ok {
			cfg.Excludes = append(cfg.Excludes, strings.Fields(rest)...)
		} else if rest, ok := strings.CutPrefix(trimmed, "requires "); ok {
			cfg.Requires = append(cfg.Requires, strings.Fields(rest)...)
		} else if vname, value, ok := parseAssign(trimmed); ok {
			cfg.Vars = append(cfg.Vars, VarAssign{Name: vname, Op: OpSet, Value: value})
		} else if vname, value, ok := parseAppend(trimmed); ok {
			cfg.Vars = append(cfg.Vars, VarAssign{Name: vname, Op: OpAppend, Value: value})
		} else if vname, value, ok := parseCondAssign(trimmed); ok {
			cfg.Vars = append(cfg.Vars, VarAssign{Name: vname, Op: OpCondSet, Value: value})
		} else {
			return nil, fmt.Errorf("line %d: unrecognized config property: %s", p.pos, trimmed)
		}
	}

	return cfg, nil
}

func (p *parser) parseLoop(line string, lineNum int) (Node, error) {
	// for var in list:
	inner := strings.TrimSuffix(strings.TrimPrefix(line, "for "), ":")
	varName, listExpr, ok := strings.Cut(inner, " in ")
	if !ok {
		return nil, fmt.Errorf("line %d: invalid for loop syntax: %s", lineNum, line)
	}
	varName = strings.TrimSpace(varName)
	listExpr = strings.TrimSpace(listExpr)
	if varName == "" || listExpr == "" {
		return nil, fmt.Errorf("line %d: for loop requires variable and list: %s", lineNum, line)
	}

	body, err := p.parseBlock(true)
	if err != nil {
		return nil, err
	}

	// Consume "end" terminator
	termLine, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("line %d: unexpected end of file in for loop", lineNum)
	}
	if strings.TrimSpace(termLine) != "end" {
		return nil, fmt.Errorf("line %d: expected 'end' to close for loop, got: %s", p.pos+1, strings.TrimSpace(termLine))
	}
	p.pos++

	return Loop{Var: varName, List: listExpr, Body: body, Line: lineNum}, nil
}

func (p *parser) parseRecipe() []string {
	var lines []string
	indent := ""
	for {
		line, ok := p.peek()
		if !ok {
			break
		}
		if line == "" {
			p.pos++
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			break
		}
		p.pos++
		if indent == "" {
			// First recipe line sets the base indentation.
			indent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		}
		lines = append(lines, strings.TrimPrefix(line, indent))
	}
	return lines
}

func (p *parser) parseConditional(line string, lineNum int) (Node, error) {
	cond := Conditional{Line: lineNum}
	branch, err := parseCondExpr(line)
	if err != nil {
		return nil, fmt.Errorf("line %d: %w", lineNum, err)
	}

	for {
		body, err := p.parseBlock(true)
		if err != nil {
			return nil, err
		}
		branch.Body = body
		cond.Branches = append(cond.Branches, branch)

		termLine, ok := p.peek()
		if !ok {
			return nil, fmt.Errorf("line %d: unexpected end of file in conditional", lineNum)
		}
		termTrimmed := strings.TrimSpace(termLine)
		p.pos++ // consume the terminator

		if termTrimmed == "end" {
			break
		}

		branch, err = parseCondExpr(termTrimmed)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", p.pos, err)
		}
	}

	return cond, nil
}

func parseAssign(line string) (string, string, bool) {
	for i := 0; i < len(line); i++ {
		if line[i] == '=' && (i == 0 || line[i-1] != '+' && line[i-1] != '!' && line[i-1] != '?') {
			prefix := line[:i]
			if strings.ContainsRune(prefix, ':') {
				return "", "", false
			}
			name := strings.TrimSpace(prefix)
			value := strings.TrimSpace(line[i+1:])
			if isValidVarName(name) {
				return name, value, true
			}
			return "", "", false
		}
	}
	return "", "", false
}

func parseCondAssign(line string) (string, string, bool) {
	idx := strings.Index(line, "?=")
	if idx < 0 {
		return "", "", false
	}
	prefix := line[:idx]
	if strings.ContainsRune(prefix, ':') {
		return "", "", false
	}
	name := strings.TrimSpace(prefix)
	value := strings.TrimSpace(line[idx+2:])
	if isValidVarName(name) {
		return name, value, true
	}
	return "", "", false
}

func parseAppend(line string) (string, string, bool) {
	idx := strings.Index(line, "+=")
	if idx < 0 {
		return "", "", false
	}
	prefix := line[:idx]
	if strings.ContainsRune(prefix, ':') {
		return "", "", false
	}
	name := strings.TrimSpace(prefix)
	value := strings.TrimSpace(line[idx+2:])
	if isValidVarName(name) {
		return name, value, true
	}
	return "", "", false
}

func parseRuleHeader(line string) (isTask, keep bool, fingerprint, depsFormat, scan, scanFormat, writes, reads string, targets, prereqs, orderOnlyPrereqs []string, ok bool) {
	if strings.HasPrefix(line, "!") {
		isTask = true
		line = line[1:]
	}

	// Find the rule-separating colon, skipping colons inside [...] brackets
	colonIdx := -1
	depth := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '[':
			depth++
		case ']':
			depth--
		case ':':
			if depth == 0 {
				colonIdx = i
				goto found
			}
		}
	}
found:
	if colonIdx < 0 {
		return false, false, "", "", "", "", "", "", nil, nil, nil, false
	}

	targetStr := strings.TrimSpace(line[:colonIdx])
	prereqStr := strings.TrimSpace(line[colonIdx+1:])

	if targetStr == "" {
		return false, false, "", "", "", "", "", "", nil, nil, nil, false
	}

	// Extract [fingerprint: ...] annotation
	if idx := strings.Index(targetStr, "[fingerprint:"); idx >= 0 {
		end := strings.Index(targetStr[idx:], "]")
		if end >= 0 {
			fingerprint = strings.TrimSpace(targetStr[idx+len("[fingerprint:") : idx+end])
			targetStr = strings.TrimSpace(targetStr[:idx] + targetStr[idx+end+1:])
		}
	}

	// Extract [deps: <format>] annotation
	if idx := strings.Index(targetStr, "[deps:"); idx >= 0 {
		end := strings.Index(targetStr[idx:], "]")
		if end >= 0 {
			depsFormat = strings.TrimSpace(targetStr[idx+len("[deps:") : idx+end])
			targetStr = strings.TrimSpace(targetStr[:idx] + targetStr[idx+end+1:])
		}
	}

	// Extract [scan-format: <format>] annotation (must be checked before
	// [scan: …] so the longer key wins).
	if idx := strings.Index(targetStr, "[scan-format:"); idx >= 0 {
		end := strings.Index(targetStr[idx:], "]")
		if end >= 0 {
			scanFormat = strings.TrimSpace(targetStr[idx+len("[scan-format:") : idx+end])
			targetStr = strings.TrimSpace(targetStr[:idx] + targetStr[idx+end+1:])
		}
	}

	// Extract [scan: <command>] annotation
	if idx := strings.Index(targetStr, "[scan:"); idx >= 0 {
		end := strings.Index(targetStr[idx:], "]")
		if end >= 0 {
			scan = strings.TrimSpace(targetStr[idx+len("[scan:") : idx+end])
			targetStr = strings.TrimSpace(targetStr[:idx] + targetStr[idx+end+1:])
		}
	}

	// Extract [writes: <spec>] annotation (e.g. "manifest path/to/manifest"
	// or "trace"). The recipe produces a set of outputs not known
	// statically; mk reads them post-run (DESIGN.md §11).
	if idx := strings.Index(targetStr, "[writes:"); idx >= 0 {
		end := strings.Index(targetStr[idx:], "]")
		if end >= 0 {
			writes = strings.TrimSpace(targetStr[idx+len("[writes:") : idx+end])
			targetStr = strings.TrimSpace(targetStr[:idx] + targetStr[idx+end+1:])
		}
	}

	// Extract [reads: <glob>...] — declared read envelope (DESIGN.md §11).
	if idx := strings.Index(targetStr, "[reads:"); idx >= 0 {
		end := strings.Index(targetStr[idx:], "]")
		if end >= 0 {
			reads = strings.TrimSpace(targetStr[idx+len("[reads:") : idx+end])
			targetStr = strings.TrimSpace(targetStr[:idx] + targetStr[idx+end+1:])
		}
	}

	// Check for [keep] annotation
	if idx := strings.Index(targetStr, "[keep]"); idx >= 0 {
		keep = true
		targetStr = strings.TrimSpace(targetStr[:idx] + targetStr[idx+len("[keep]"):])
	}

	targets = strings.Fields(targetStr)

	// Split prereqs on | for order-only prerequisites
	normalStr, orderOnlyStr, _ := strings.Cut(prereqStr, "|")
	if s := strings.TrimSpace(normalStr); s != "" {
		prereqs = strings.Fields(s)
	}
	if s := strings.TrimSpace(orderOnlyStr); s != "" {
		orderOnlyPrereqs = strings.Fields(s)
	}

	return isTask, keep, fingerprint, depsFormat, scan, scanFormat, writes, reads, targets, prereqs, orderOnlyPrereqs, true
}

func parseInclude(line string, lineNum int) (Node, error) {
	rest := strings.TrimPrefix(line, "include ")
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return nil, fmt.Errorf("line %d: include requires a path", lineNum)
	}

	inc := Include{Path: parts[0], Line: lineNum}
	if len(parts) >= 3 && parts[1] == "as" {
		inc.Alias = parts[2]
	}
	return inc, nil
}

func parseCondExpr(line string) (CondBranch, error) {
	if line == "else" {
		return CondBranch{Op: "else"}, nil
	}

	var rest, op string
	if after, ok := strings.CutPrefix(line, "if "); ok {
		rest, op = after, "if"
	} else if after, ok := strings.CutPrefix(line, "elif "); ok {
		rest, op = after, "elif"
	} else {
		return CondBranch{}, fmt.Errorf("expected if/elif/else, got: %s", line)
	}

	if parts := strings.SplitN(rest, " == ", 2); len(parts) == 2 {
		return CondBranch{Op: op, Left: strings.TrimSpace(parts[0]), Cmp: "==", Right: strings.TrimSpace(parts[1])}, nil
	}
	if parts := strings.SplitN(rest, " != ", 2); len(parts) == 2 {
		return CondBranch{Op: op, Left: strings.TrimSpace(parts[0]), Cmp: "!=", Right: strings.TrimSpace(parts[1])}, nil
	}

	return CondBranch{}, fmt.Errorf("expected comparison (== or !=), got: %s", rest)
}

func isValidVarName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
				return false
			}
		} else {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$' || c == '{' || c == '}') {
				return false
			}
		}
	}
	return true
}

// containsVarRef reports whether value contains a variable reference to name,
// i.e. $name (followed by non-identifier or end) or ${name}.
func containsVarRef(value, name string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] != '$' {
			continue
		}
		i++
		if i >= len(value) {
			break
		}
		switch {
		case value[i] == '{':
			end := strings.IndexByte(value[i:], '}')
			if end >= 0 && value[i+1:i+end] == name {
				return true
			}
		case isIdentStart(value[i]):
			start := i
			for i < len(value) && isIdentCont(value[i]) {
				i++
			}
			if value[start:i] == name {
				return true
			}
			i-- // loop will increment
		}
	}
	return false
}
