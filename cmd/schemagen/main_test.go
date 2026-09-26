package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestSchemaUpToDate fails when internal/config changed without regenerating
// fork/spec/config.schema.json (go generate ./internal/config).
func TestSchemaUpToDate(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	want, err := generate(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "fork", "spec", "config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("fork/spec/config.schema.json is stale; run: go generate ./internal/config")
	}
}
