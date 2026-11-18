package main

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// Parsing builds syntax nodes without expanding aliases. Count the expanded
// graph before either generic Unmarshal or the upstream schema/engine loader.
// A shared budget bounds exponential aliases, cycles, depth and scalar copies.
type yamlBudget struct {
	anchors map[string]ast.Node
	nodes   int
	bytes   int
	failed  bool
}
type yamlVisitor struct {
	budget *yamlBudget
	depth  int
}

func (v yamlVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil || v.budget.failed {
		return nil
	}
	b := v.budget
	b.nodes++
	if token := node.GetToken(); token != nil {
		b.bytes += len(token.Value)
	}
	if b.nodes > 65536 || b.bytes > 1<<20 || v.depth > 64 {
		b.failed = true
		return nil
	}
	v.depth++
	switch n := node.(type) {
	case *ast.AnchorNode:
		b.anchors[n.Name.GetToken().Value] = n.Value
	case *ast.AliasNode:
		target := b.anchors[n.Value.GetToken().Value]
		if target == nil {
			b.failed = true
			return nil
		}
		ast.Walk(v, target)
		return nil
	}
	return v
}

func boundedYAML(raw string) bool {
	// The engine accepts a BOM and retries quoted literal tabs. Spaces have
	// the same syntax structure for budgeting and avoid that parser defect.
	raw = strings.TrimPrefix(strings.ReplaceAll(raw, "\t", " "), "\ufeff")
	file, err := parser.ParseBytes([]byte(raw), 0)
	if err != nil {
		return false
	}
	for _, doc := range file.Docs {
		budget := &yamlBudget{anchors: map[string]ast.Node{}}
		if doc.Body != nil {
			ast.Walk(yamlVisitor{budget: budget}, doc.Body)
		}
		if budget.failed {
			return false
		}
	}
	return true
}
