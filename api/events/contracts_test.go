package eventscontract

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventSchemasAreStrictDraft202012Objects(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read schema directory: %v", err)
	}
	validated := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		validated++
		t.Run(strings.TrimSuffix(entry.Name(), ".json"), func(t *testing.T) {
			validateSchema(t, entry.Name())
		})
	}
	if validated == 0 {
		t.Fatal("no event schemas found")
	}
}

func validateSchema(t *testing.T, path string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close schema: %v", closeErr)
		}
	}()
	var schema map[string]any
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("schema contains trailing JSON: %v", err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("unexpected JSON Schema dialect: %v", schema["$schema"])
	}
	if id, ok := schema["$id"].(string); !ok || id == "" {
		t.Fatalf("schema has no $id: %v", schema["$id"])
	}
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatal("event schema must be a strict object")
	}
}
