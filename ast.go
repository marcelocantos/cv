// Copyright 2026 The mk Authors
// SPDX-License-Identifier: Apache-2.0

package mk

// Node is the interface for all AST nodes.
type Node interface {
	node()
}

// File represents a parsed mkfile.
type File struct {
	Stmts []Node
}

// VarAssign represents a variable assignment: name = value, name += value, lazy name = value.
type VarAssign struct {
	Name  string
	Op    AssignOp
	Value string
	Lazy  bool
	Line  int
}

type AssignOp int

const (
	OpSet     AssignOp = iota // =
	OpAppend                  // +=
	OpCondSet                 // ?=
)

// Rule represents a build rule: targets: prerequisites \n recipe.
type Rule struct {
	Targets          []string
	Prereqs          []string
	OrderOnlyPrereqs []string // after |
	Recipe           []string
	IsTask           bool   // ! prefix
	Keep             bool   // [keep] annotation
	Fingerprint      string // [fingerprint: command] for non-file artifacts
	DepsFormat       string // [deps: <format>] — gcc, makefile, etc.
	Line             int
}

// Include represents an include directive.
type Include struct {
	Path  string
	Alias string // "as foo" scoping
	Line  int
}

// Conditional represents if/elif/else/end blocks.
type Conditional struct {
	Branches []CondBranch
	Line     int
}

type CondBranch struct {
	Op    string // "if", "elif", "else"
	Left  string
	Cmp   string // "==", "!="
	Right string
	Body  []Node
}

// FuncDef represents a user-defined function: fn name(params): return expr.
type FuncDef struct {
	Name   string
	Params []string // parameter names
	Body   string   // the return expression
	Line   int
}

// ConfigDef represents a build config declaration: config name: ...
type ConfigDef struct {
	Name     string
	Excludes []string    // mutually exclusive configs
	Requires []string    // targets that must be built before any :config build
	Vars     []VarAssign // variable overrides
	Line     int
}

// Loop represents a for loop: for var in list: ... end
type Loop struct {
	Var  string // loop variable name
	List string // list expression (unexpanded)
	Body []Node // statements to repeat
	Line int
}

func (VarAssign) node()   {}
func (Rule) node()        {}
func (Include) node()     {}
func (Conditional) node() {}
func (FuncDef) node()     {}
func (ConfigDef) node()   {}
func (Loop) node()        {}
