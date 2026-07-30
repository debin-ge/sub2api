package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandlerUsageBillingInputsCarryPayloadFingerprint(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, parseErr := parser.ParseFile(fset, name, nil, 0)
			require.NoError(t, parseErr)

			var missing []token.Position
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok || !isUsageBillingInputLiteral(literal.Type) {
					return true
				}
				if !compositeLiteralHasKey(literal, "RequestPayloadHash") {
					missing = append(missing, fset.Position(literal.Lbrace))
				}
				return true
			})

			require.Empty(t, missing, "every post-upstream billing input must carry a request payload hash")
		})
	}
}

func isUsageBillingInputLiteral(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "service" {
		return false
	}
	switch selector.Sel.Name {
	case "RecordUsageInput",
		"RecordUsageLongContextInput",
		"OpenAIRecordUsageInput",
		"CyberPolicyUsageInput":
		return true
	default:
		return false
	}
}
