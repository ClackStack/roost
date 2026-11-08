// Package roost holds repository-wide checks.
package roost

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// outboundAllowed lists the only directory permitted to open connections.
const outboundAllowed = "internal/embed"

// forbidden lists selectors that start outbound traffic. Serving HTTP needs
// net/http, so importing it cannot be banned; these calls are what matter.
var forbidden = map[string]map[string]bool{
	"http": {"Client": true, "DefaultClient": true, "DefaultTransport": true, "Transport": true,
		"Get": true, "Post": true, "PostForm": true, "Head": true, "NewRequest": true, "NewRequestWithContext": true},
	"net": {"Dial": true, "DialTimeout": true, "DialTCP": true, "DialUDP": true, "DialIP": true, "DialUnix": true,
		"Dialer": true, "Resolver": true, "DefaultResolver": true, "LookupHost": true, "LookupIP": true, "LookupAddr": true},
	"tls":  {"Dial": true, "DialWithDialer": true, "Dialer": true},
	"exec": {"LookPath": true}, // the only child process is this binary, at a known path
}

// TestNoOutboundCode fails the build if outbound networking appears anywhere
// but the embeddings package. Test files are exempt: they run local servers.
func TestNoOutboundCode(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && path != "." {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasPrefix(filepath.ToSlash(path), outboundAllowed+"/") {
			return nil
		}
		checked++
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && forbidden[pkg.Name][sel.Sel.Name] {
				t.Errorf("%s: %s.%s is outbound networking, which is only allowed in %s",
					fset.Position(sel.Pos()), pkg.Name, sel.Sel.Name, outboundAllowed)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 10 {
		t.Fatalf("only checked %d files; the walk is broken", checked)
	}
}
