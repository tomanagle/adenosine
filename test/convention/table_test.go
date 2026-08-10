package convention

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGoTestsUseTableDrivenConvention(t *testing.T) {
	testCases := []struct {
		name string
	}{
		{name: "all Test functions declare testCases and range with t.Run"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, filename, _, ok := runtime.Caller(0)
			if !ok {
				t.Fatal("resolve convention test path")
			}
			root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
			var violations []string
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					switch entry.Name() {
					case ".git", ".air", "node_modules":
						return filepath.SkipDir
					}
					return nil
				}
				if !strings.HasSuffix(path, "_test.go") {
					return nil
				}
				file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
				if parseErr != nil {
					return parseErr
				}
				for _, declaration := range file.Decls {
					function, ok := declaration.(*ast.FuncDecl)
					if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") || function.Body == nil {
						continue
					}
					if !usesTestCasesTable(function.Body) {
						relative, relErr := filepath.Rel(root, path)
						if relErr != nil {
							return relErr
						}
						violations = append(violations, relative+":"+function.Name.Name)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("scan Go tests: %v", err)
			}
			if len(violations) != 0 {
				t.Fatalf("Test functions must use testCases and t.Run:\n%s", strings.Join(violations, "\n"))
			}
		})
	}
}

func usesTestCasesTable(body *ast.BlockStmt) bool {
	declaresTable := false
	rangesTable := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.AssignStmt:
			if value.Tok != token.DEFINE {
				return true
			}
			for _, expression := range value.Lhs {
				identifier, ok := expression.(*ast.Ident)
				if ok && identifier.Name == "testCases" {
					declaresTable = true
				}
			}
		case *ast.RangeStmt:
			identifier, ok := value.X.(*ast.Ident)
			if ok && identifier.Name == "testCases" && callsTRun(value.Body) {
				rangesTable = true
			}
		}
		return true
	})
	return declaresTable && rangesTable
}

func callsTRun(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Run" {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && identifier.Name == "t" {
			found = true
		}
		return true
	})
	return found
}
