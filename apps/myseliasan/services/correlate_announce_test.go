package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// The rule-change announcement must actually be CALLED from the edit paths.
//
// This is a source-level check rather than a behavioural one because of how the bug it
// guards arrived: `announceRulesChanged` was written, wired all the way to the event bus in
// app.go, and never called. Go does not complain about an unused method, so everything
// compiled, every test passed, and the feature was silently absent — an operator's rule
// edit reloaded only the instance that served it, while the LEADER (the instance that
// actually fires rules) kept the old set until something restarted.
//
// Asserting on the call sites catches exactly that: a refactor that drops the call cannot
// come back green.
func TestRuleEditsAnnounceTheChange(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "correlate_crud.go", nil, 0)
	if err != nil {
		t.Fatalf("parse correlate_crud.go: %v", err)
	}

	// Which methods contain a call to announceRulesChanged?
	announced := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			return true
		}
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "announceRulesChanged" {
				announced[fn.Name.Name] = true
			}
			return true
		})
		return true
	})

	for _, method := range []string{"Save", "Delete"} {
		if !announced[method] {
			t.Errorf("Correlator.%s does not call announceRulesChanged — a rule edit there will not reach the other instances", method)
		}
	}
}
