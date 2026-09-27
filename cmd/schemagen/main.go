// Command schemagen writes the JSON Schema for decypharr's config.json from
// the internal/config structs. Run it through `go generate ./internal/config`.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/sirrobot01/decypharr/internal/config"
)

const (
	modulePath = "github.com/sirrobot01/decypharr"
	configDir  = "internal/config"
	schemaID   = "https://github.com/alalloush/decypharr/blob/dev/fork/spec/config.schema.json"
)

// deprecatedPattern matches Go "Deprecated" notes, which upstream writes as
// "Deprecated:" or "Deprecated.".
var deprecatedPattern = regexp.MustCompile(`(?m)^Deprecated[:.]`)

func main() {
	out := flag.String("o", "fork/spec/config.schema.json", "output file, relative to the module root")
	flag.Parse()

	root, err := moduleRoot()
	if err != nil {
		fail(err)
	}
	data, err := generate(root)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(root, *out), data, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "schemagen:", err)
	os.Exit(1)
}

// moduleRoot walks up from the working directory to the directory holding
// go.mod, so the command works from the repository root and from go generate.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found above the working directory")
		}
		dir = parent
	}
}

// generate returns the schema document for config.json, indented and
// newline-terminated.
func generate(root string) ([]byte, error) {
	comments, err := goComments(filepath.Join(root, configDir), modulePath+"/"+configDir)
	if err != nil {
		return nil, err
	}
	reflector := &jsonschema.Reflector{
		// config.json has no required keys, and unknown keys are ignored on
		// load (older configs carry removed fields), so the schema allows both.
		RequiredFromJSONSchemaTags: true,
		AllowAdditionalProperties:  true,
		ExpandedStruct:             true,
		Anonymous:                  true,
		CommentMap:                 comments,
	}
	schema := reflector.Reflect(&config.Config{})
	schema.ID = schemaID
	schema.Title = "Decypharr configuration"
	schema.Description = "config.json in the decypharr config directory. Generated from internal/config by cmd/schemagen; do not edit by hand."

	markDeprecated(schema)
	for _, def := range schema.Definitions {
		markDeprecated(def)
	}
	if err := markSecrets(schema); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schema); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// goComments maps "<import path>.<Type>" and "<import path>.<Type>.<Field>"
// to their Go comments. A field's description joins its doc comment and its
// trailing line comment, since upstream uses both.
func goComments(dir, importPath string) (map[string]string, error) {
	fset := token.NewFileSet()
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	comments := map[string]string{}
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				if !typeSpec.Name.IsExported() {
					continue
				}
				typeName := typeSpec.Name.Name
				doc := typeSpec.Doc
				if doc == nil && len(gen.Specs) == 1 {
					doc = gen.Doc
				}
				if text := joinComments(doc); text != "" {
					comments[importPath+"."+typeName] = text
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for i, field := range structType.Fields.List {
					doc := field.Doc
					if isSectionHeader(fset, structType.Fields.List, i) {
						doc = nil
					}
					text := joinComments(doc, field.Comment)
					if text == "" {
						continue
					}
					for _, name := range field.Names {
						if name.IsExported() {
							comments[importPath+"."+typeName+"."+name.Name] = text
						}
					}
				}
			}
		}
	}
	return comments, nil
}

// isSectionHeader reports whether the doc comment above fields[i] labels a
// group of fields ("// Manager settings") rather than describing that field.
// Go field docs start with the field name; a header is a single line followed
// directly by further undocumented fields. Longer group comments are kept.
func isSectionHeader(fset *token.FileSet, fields []*ast.Field, i int) bool {
	field := fields[i]
	if field.Doc == nil || len(field.Doc.List) != 1 || i+1 == len(fields) {
		return false
	}
	for _, name := range field.Names {
		if strings.HasPrefix(field.Doc.Text(), name.Name+" ") {
			return false
		}
	}
	next := fields[i+1]
	return next.Doc == nil && fset.Position(next.Pos()).Line == fset.Position(field.End()).Line+1
}

func joinComments(groups ...*ast.CommentGroup) string {
	var parts []string
	for _, group := range groups {
		if text := strings.TrimSpace(group.Text()); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func markDeprecated(schema *jsonschema.Schema) {
	if schema.Properties == nil {
		return
	}
	for _, property := range schema.Properties.FromOldest() {
		if deprecatedPattern.MatchString(property.Description) {
			property.Deprecated = true
		}
	}
}

// markSecrets marks config.SecretFields writeOnly, so generated clients never
// echo them back. Every entry must exist in the schema.
func markSecrets(root *jsonschema.Schema) error {
	for typeName, names := range config.SecretFields {
		schema := root
		if typeName != "Config" {
			schema = root.Definitions[typeName]
		}
		if schema == nil || schema.Properties == nil {
			return fmt.Errorf("secret type %s is not in the schema", typeName)
		}
		for _, name := range names {
			property, ok := schema.Properties.Get(name)
			if !ok {
				return fmt.Errorf("secret property %s.%s is not in the schema", typeName, name)
			}
			property.WriteOnly = true
			switch {
			case property.Type == "string":
				property.Format = "password"
			case property.Type == "array" && property.Items != nil && property.Items.Type == "string":
				property.Items.Format = "password"
			}
		}
	}
	return nil
}
