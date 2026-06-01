// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Vars is a variable store. All variables are also environment variables.
type Vars struct {
	vals  map[string]string
	lazy  map[string]string   // unevaluated lazy expressions
	funcs map[string]*FuncDef // user-defined functions
}

func NewVars() *Vars {
	v := &Vars{
		vals:  make(map[string]string),
		lazy:  make(map[string]string),
		funcs: make(map[string]*FuncDef),
	}
	// Import environment
	for _, env := range os.Environ() {
		k, val, ok := strings.Cut(env, "=")
		if ok {
			v.vals[k] = val
		}
	}
	return v
}

// Set sets a variable immediately.
func (v *Vars) Set(name, value string) {
	v.vals[name] = value
	delete(v.lazy, name)
}

// SetFunc registers a user-defined function.
func (v *Vars) SetFunc(def *FuncDef) {
	v.funcs[def.Name] = def
}

// SetLazy sets a variable for deferred evaluation.
func (v *Vars) SetLazy(name, expr string) {
	v.lazy[name] = expr
	delete(v.vals, name)
}

// Append appends to a variable.
func (v *Vars) Append(name, value string) {
	existing := v.Get(name)
	if existing != "" {
		v.Set(name, existing+" "+value)
	} else {
		v.Set(name, value)
	}
}

// Get retrieves a variable's value, evaluating lazy variables on demand.
func (v *Vars) Get(name string) string {
	if expr, ok := v.lazy[name]; ok {
		val := v.Expand(expr)
		v.vals[name] = val
		delete(v.lazy, name)
		return val
	}
	return v.vals[name]
}

// Expand expands variable references in a string.
// $name expands to the value of name.
// ${name} also works for delimiting.
// $name.dir / $name.file — path property access.
// $[func args] — built-in cv functions.
// $$ expands to a literal $.
func (v *Vars) Expand(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		i++ // skip $
		if i >= len(s) {
			b.WriteByte('$')
			break
		}

		switch {
		case s[i] == '$':
			// $$ → literal $
			b.WriteByte('$')
			i++

		case s[i] == '{':
			// ${name}
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				b.WriteByte('$')
				b.WriteByte('{')
				i++
			} else {
				name := s[i+1 : i+end]
				b.WriteString(v.Get(name))
				i += end + 1
			}

		case s[i] == '[':
			// $[func args] — cv built-in functions
			end := findMatchingBracket(s[i:])
			if end < 0 {
				b.WriteByte('$')
				b.WriteByte('[')
				i++
			} else {
				inner := s[i+1 : i+end]
				b.WriteString(v.evalFunc(inner))
				i += end + 1
			}

		case isIdentStart(s[i]):
			// $name, $name.scope, $name.prop, or $name:old=new (substitution reference)
			start := i
			for i < len(s) && isIdentCont(s[i]) {
				i++
			}
			name := s[start:i]
			val := v.Get(name)

			// Check for dot: could be scoped variable ($lib.src) or property ($target.dir)
			if i < len(s) && s[i] == '.' {
				propStart := i + 1
				for i+1 < len(s) && isIdentCont(s[i+1]) {
					i++
				}
				if propStart <= len(s) {
					member := s[propStart : i+1]
					// Try scoped variable first (e.g., lib.src)
					scopedName := name + "." + member
					if scopedVal := v.Get(scopedName); scopedVal != "" {
						i++ // consume past member
						val = scopedVal
						// Check for further property access ($lib.src.dir)
						if i < len(s) && s[i] == '.' {
							pStart := i + 1
							for i+1 < len(s) && isIdentCont(s[i+1]) {
								i++
							}
							if pStart <= len(s) {
								prop := s[pStart : i+1]
								i++
								val = varProperty(val, prop)
							}
						}
						b.WriteString(val)
						continue
					}
					// Fall back to property access (e.g., target.dir)
					i++ // consume past property
					val = varProperty(val, member)
					b.WriteString(val)
					continue
				}
			}

			// Check for substitution reference: $name:old=new
			if i < len(s) && s[i] == ':' {
				rest := s[i+1:]
				if eqIdx := strings.IndexByte(rest, '='); eqIdx >= 0 {
					// Find end of replacement (next space or end)
					endIdx := strings.IndexByte(rest[eqIdx+1:], ' ')
					var old, repl string
					if endIdx < 0 {
						old = rest[:eqIdx]
						repl = rest[eqIdx+1:]
						i = len(s)
					} else {
						old = rest[:eqIdx]
						repl = rest[eqIdx+1 : eqIdx+1+endIdx]
						i += 1 + eqIdx + 1 + endIdx
					}
					// Apply substitution: .c=.o means replace suffix .c with .o
					// Convert to % patterns for patsubstWord
					oldPat := "%" + old
					replPat := "%" + repl
					words := strings.Fields(val)
					for j, w := range words {
						words[j] = patsubstWord(oldPat, replPat, w)
					}
					val = strings.Join(words, " ")
				}
			}

			b.WriteString(val)

		default:
			b.WriteByte('$')
		}
	}
	return b.String()
}

// varProperty returns a property of a variable value.
func varProperty(val, prop string) string {
	switch prop {
	case "dir":
		return filepath.Dir(val)
	case "file":
		return filepath.Base(val)
	default:
		return ""
	}
}

// Environ returns the variables as environment strings for exec.
func (v *Vars) Environ() []string {
	var env []string
	for k, val := range v.vals {
		env = append(env, k+"="+val)
	}
	return env
}

// Snapshot returns a copy of all current variable values (resolving lazy ones).
func (v *Vars) Snapshot() map[string]string {
	snap := make(map[string]string, len(v.vals)+len(v.lazy))
	for k, val := range v.vals {
		snap[k] = val
	}
	for k := range v.lazy {
		snap[k] = v.Get(k)
	}
	return snap
}

// Clone creates a copy of the variable store.
func (v *Vars) Clone() *Vars {
	c := &Vars{
		vals:  make(map[string]string, len(v.vals)),
		lazy:  make(map[string]string, len(v.lazy)),
		funcs: make(map[string]*FuncDef, len(v.funcs)),
	}
	for k, val := range v.vals {
		c.vals[k] = val
	}
	for k, val := range v.lazy {
		c.lazy[k] = val
	}
	for k, val := range v.funcs {
		c.funcs[k] = val
	}
	return c
}

func (v *Vars) evalFunc(inner string) string {
	name, args, _ := strings.Cut(inner, " ")
	switch name {
	case "wildcard":
		return v.funcWildcard(strings.TrimSpace(args))
	case "shell":
		return v.funcShell(strings.TrimSpace(args))
	case "patsubst":
		return v.funcPatsubst(strings.TrimSpace(args))
	case "subst":
		return v.funcSubst(strings.TrimSpace(args))
	case "filter":
		return v.funcFilter(strings.TrimSpace(args))
	case "filter-out":
		return v.funcFilterOut(strings.TrimSpace(args))
	case "dir":
		return v.funcDir(strings.TrimSpace(args))
	case "notdir":
		return v.funcNotdir(strings.TrimSpace(args))
	case "basename":
		return v.funcBasename(strings.TrimSpace(args))
	case "suffix":
		return v.funcSuffix(strings.TrimSpace(args))
	case "addprefix":
		return v.funcAddprefix(strings.TrimSpace(args))
	case "addsuffix":
		return v.funcAddsuffix(strings.TrimSpace(args))
	case "sort":
		return v.funcSort(strings.TrimSpace(args))
	case "word":
		return v.funcWord(strings.TrimSpace(args))
	case "words":
		return v.funcWords(strings.TrimSpace(args))
	case "strip":
		return v.funcStrip(strings.TrimSpace(args))
	case "findstring":
		return v.funcFindstring(strings.TrimSpace(args))
	case "if":
		return v.funcIf(strings.TrimSpace(args))
	default:
		// Check user-defined functions
		if fn, ok := v.funcs[name]; ok {
			return v.callUserFunc(fn, strings.TrimSpace(args))
		}
		return ""
	}
}

func (v *Vars) callUserFunc(fn *FuncDef, args string) string {
	// Expand arguments before binding to parameters
	expanded := v.Expand(args)

	// Split expanded args into words, one per parameter
	words := strings.Fields(expanded)

	// Create a child scope with parameters bound
	child := v.Clone()
	for i, param := range fn.Params {
		if i < len(words) {
			child.Set(param, words[i])
		} else {
			child.Set(param, "")
		}
	}
	// If more words than params, join remaining into last param
	if len(fn.Params) > 0 && len(words) > len(fn.Params) {
		last := len(fn.Params) - 1
		child.Set(fn.Params[last], strings.Join(words[last:], " "))
	}

	return child.Expand(fn.Body)
}

func (v *Vars) funcWildcard(pattern string) string {
	pattern = v.Expand(pattern)
	matches, err := wildcardGlob(pattern)
	if err != nil {
		return ""
	}
	return strings.Join(matches, " ")
}

func (v *Vars) funcShell(cmd string) string {
	cmd = v.Expand(cmd)
	out, err := runShellCapture(cmd)
	if err != nil {
		return ""
	}
	// Replace newlines with spaces, trim
	out = strings.ReplaceAll(strings.TrimSpace(out), "\n", " ")
	return out
}

func (v *Vars) funcPatsubst(args string) string {
	// $[patsubst pattern,replacement,text]
	parts := strings.SplitN(args, ",", 3)
	if len(parts) != 3 {
		return ""
	}
	pattern := strings.TrimSpace(parts[0])
	replacement := strings.TrimSpace(parts[1])
	text := strings.TrimSpace(v.Expand(parts[2]))

	words := strings.Fields(text)
	var result []string
	for _, w := range words {
		result = append(result, patsubstWord(pattern, replacement, w))
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcSubst(args string) string {
	// $[subst from,to,text]
	parts := strings.SplitN(args, ",", 3)
	if len(parts) != 3 {
		return ""
	}
	from := strings.TrimSpace(parts[0])
	to := strings.TrimSpace(parts[1])
	text := strings.TrimSpace(v.Expand(parts[2]))
	return strings.ReplaceAll(text, from, to)
}

func (v *Vars) funcFilter(args string) string {
	// $[filter pattern,text]
	parts := strings.SplitN(args, ",", 2)
	if len(parts) != 2 {
		return ""
	}
	pattern := strings.TrimSpace(parts[0])
	text := strings.TrimSpace(v.Expand(parts[1]))
	var result []string
	for _, w := range strings.Fields(text) {
		if patsubstMatch(pattern, w) {
			result = append(result, w)
		}
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcFilterOut(args string) string {
	// $[filter-out pattern,text]
	parts := strings.SplitN(args, ",", 2)
	if len(parts) != 2 {
		return ""
	}
	pattern := strings.TrimSpace(parts[0])
	text := strings.TrimSpace(v.Expand(parts[1]))
	var result []string
	for _, w := range strings.Fields(text) {
		if !patsubstMatch(pattern, w) {
			result = append(result, w)
		}
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcDir(args string) string {
	// $[dir names...]
	text := v.Expand(args)
	words := strings.Fields(text)
	var result []string
	for _, w := range words {
		d := filepath.Dir(w)
		if d == "." {
			result = append(result, "./")
		} else {
			result = append(result, d+"/")
		}
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcNotdir(args string) string {
	// $[notdir names...]
	text := v.Expand(args)
	words := strings.Fields(text)
	var result []string
	for _, w := range words {
		result = append(result, filepath.Base(w))
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcBasename(args string) string {
	// $[basename names...]
	text := v.Expand(args)
	words := strings.Fields(text)
	var result []string
	for _, w := range words {
		ext := filepath.Ext(w)
		result = append(result, w[:len(w)-len(ext)])
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcSuffix(args string) string {
	// $[suffix names...]
	text := v.Expand(args)
	words := strings.Fields(text)
	var result []string
	for _, w := range words {
		ext := filepath.Ext(w)
		if ext != "" {
			result = append(result, ext)
		}
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcAddprefix(args string) string {
	// $[addprefix prefix,names]
	parts := strings.SplitN(args, ",", 2)
	if len(parts) != 2 {
		return ""
	}
	prefix := strings.TrimSpace(parts[0])
	text := strings.TrimSpace(v.Expand(parts[1]))
	words := strings.Fields(text)
	var result []string
	for _, w := range words {
		result = append(result, prefix+w)
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcAddsuffix(args string) string {
	// $[addsuffix suffix,names]
	parts := strings.SplitN(args, ",", 2)
	if len(parts) != 2 {
		return ""
	}
	suffix := strings.TrimSpace(parts[0])
	text := strings.TrimSpace(v.Expand(parts[1]))
	words := strings.Fields(text)
	var result []string
	for _, w := range words {
		result = append(result, w+suffix)
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcSort(args string) string {
	// $[sort list] — sort and deduplicate
	text := v.Expand(args)
	words := strings.Fields(text)
	sort.Strings(words)
	// Deduplicate
	var result []string
	for i, w := range words {
		if i == 0 || w != words[i-1] {
			result = append(result, w)
		}
	}
	return strings.Join(result, " ")
}

func (v *Vars) funcWord(args string) string {
	// $[word n,text] — 1-indexed
	parts := strings.SplitN(args, ",", 2)
	if len(parts) != 2 {
		return ""
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || n < 1 {
		return ""
	}
	text := strings.TrimSpace(v.Expand(parts[1]))
	words := strings.Fields(text)
	if n > len(words) {
		return ""
	}
	return words[n-1]
}

func (v *Vars) funcWords(args string) string {
	// $[words text] — count of words
	text := v.Expand(args)
	return strconv.Itoa(len(strings.Fields(text)))
}

func (v *Vars) funcStrip(args string) string {
	// $[strip text] — normalize whitespace
	text := v.Expand(args)
	return strings.Join(strings.Fields(text), " ")
}

func (v *Vars) funcFindstring(args string) string {
	// $[findstring find,in]
	parts := strings.SplitN(args, ",", 2)
	if len(parts) != 2 {
		return ""
	}
	find := strings.TrimSpace(parts[0])
	text := strings.TrimSpace(v.Expand(parts[1]))
	if strings.Contains(text, find) {
		return find
	}
	return ""
}

func (v *Vars) funcIf(args string) string {
	// $[if condition,then-val,else-val]
	parts := strings.SplitN(args, ",", 3)
	if len(parts) < 2 {
		return ""
	}
	cond := strings.TrimSpace(v.Expand(parts[0]))
	if cond != "" {
		return strings.TrimSpace(v.Expand(parts[1]))
	}
	if len(parts) == 3 {
		return strings.TrimSpace(v.Expand(parts[2]))
	}
	return ""
}

func patsubstWord(pattern, replacement, word string) string {
	// Simple % substitution
	if !strings.Contains(pattern, "%") {
		if word == pattern {
			return replacement
		}
		return word
	}
	prefix, suffix, _ := strings.Cut(pattern, "%")
	if strings.HasPrefix(word, prefix) && strings.HasSuffix(word, suffix) {
		stem := word[len(prefix) : len(word)-len(suffix)]
		return strings.ReplaceAll(replacement, "%", stem)
	}
	return word
}

// patsubstMatch tests whether a word matches a % pattern.
func patsubstMatch(pattern, word string) bool {
	if !strings.Contains(pattern, "%") {
		return word == pattern
	}
	prefix, suffix, _ := strings.Cut(pattern, "%")
	return strings.HasPrefix(word, prefix) && strings.HasSuffix(word, suffix)
}

func findMatchingBracket(s string) int {
	depth := 0
	for i, c := range s {
		switch c {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}
