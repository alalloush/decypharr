package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// envDocPath is the hand-maintained list of DECYPHARR_* overrides.
const envDocPath = "../../fork/spec/env.md"

// TestEnvDocMatchesCode fails when a key read through getEnv is missing from
// fork/spec/env.md, or when env.md lists a DECYPHARR_* key nothing reads.
// Array indexes are written as N in both places.
func TestEnvDocMatchesCode(t *testing.T) {
	code := getEnvKeys(t)
	doc := documentedEnvKeys(t)
	literals := decypharrLiterals(t)

	for _, key := range sortedKeys(code) {
		if !doc[key] {
			t.Errorf("%s (read at %s) is missing from fork/spec/env.md", key, code[key])
		}
	}
	for _, key := range sortedKeys(doc) {
		if _, ok := code[key]; !ok && !literals[key] {
			t.Errorf("fork/spec/env.md lists %s, but no code reads it", key)
		}
	}
}

// getEnvKeys returns every DECYPHARR_* key passed to getEnv in this package,
// mapped to the position of its first read.
func getEnvKeys(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			if fn, ok := call.Fun.(*ast.Ident); !ok || fn.Name != "getEnv" {
				return true
			}
			key, ok := resolveEnvKey(call.Args[0])
			if !ok {
				t.Errorf("%s: cannot resolve the getEnv key statically; extend resolveEnvKey", fset.Position(call.Pos()))
				return true
			}
			key = "DECYPHARR_" + key
			if _, seen := keys[key]; !seen {
				keys[key] = fset.Position(call.Pos()).String()
			}
			return true
		})
	}
	if len(keys) == 0 {
		t.Fatal("found no getEnv calls")
	}
	return keys
}

// resolveEnvKey evaluates the key expressions used in this package: string
// literals, fmt.Sprintf with a literal format (%d becomes N), concatenation,
// and local variables assigned once from one of those.
func resolveEnvKey(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, ok := resolveEnvKey(e.X)
		if !ok {
			return "", false
		}
		right, ok := resolveEnvKey(e.Y)
		return left + right, ok
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Sprintf" || len(e.Args) == 0 {
			return "", false
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "fmt" {
			return "", false
		}
		format, ok := resolveEnvKey(e.Args[0])
		return strings.ReplaceAll(format, "%d", "N"), ok
	case *ast.Ident:
		if e.Obj == nil {
			return "", false
		}
		assign, ok := e.Obj.Decl.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != len(assign.Rhs) {
			return "", false
		}
		for i, lhs := range assign.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Obj == e.Obj {
				return resolveEnvKey(assign.Rhs[i])
			}
		}
	}
	return "", false
}

var (
	envDocKeyPattern = regexp.MustCompile("`(DECYPHARR_[A-Z0-9_]+)`")
	envIndexPattern  = regexp.MustCompile(`__[0-9]+(__|$)`)
)

func normalizeEnvKey(key string) string {
	return envIndexPattern.ReplaceAllString(key, "__N$1")
}

func documentedEnvKeys(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(envDocPath)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, match := range envDocKeyPattern.FindAllStringSubmatch(string(data), -1) {
		keys[normalizeEnvKey(match[1])] = true
	}
	return keys
}

// decypharrLiterals collects "DECYPHARR_*" string literals from the module's
// non-test Go files, for variables read outside getEnv (os.Getenv elsewhere).
func decypharrLiterals(t *testing.T) map[string]bool {
	t.Helper()
	literal := regexp.MustCompile(`"(DECYPHARR_[A-Z0-9_]+)"`)
	keys := map[string]bool{}
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "fork", "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range literal.FindAllStringSubmatch(string(data), -1) {
			keys[normalizeEnvKey(match[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
