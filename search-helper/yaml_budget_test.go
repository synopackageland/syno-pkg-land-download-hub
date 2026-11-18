package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestDefinitionRejectsAliasExpansionBeforeDecoding(t *testing.T) {
	raw := "a0: &a0 [x,x,x,x,x,x,x,x,x,x]\n"
	for i := 1; i < 5; i++ {
		raw += fmt.Sprintf("a%d: &a%d [%s]\n", i, i, strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*a%d,", i-1), 10), ","))
	}
	for _, input := range []string{raw, "x: &x [*x]\n"} {
		_, meta := inspect(input)
		if meta.Supported || !slices.Contains(meta.Reasons, "definition_too_large") {
			t.Fatalf("unbounded YAML reached the decoder: %+v", meta)
		}
	}
}

func TestDefinitionKeepsOrdinaryAnchorsAndQuotedAliasText(t *testing.T) {
	input := strings.Replace(fixture, "size: {text: 1}", "size: &field {text: 1}", 1)
	input = strings.Replace(input, "seeders: {text: 1}", "seeders: *field", 1)
	if !boundedYAML(input) {
		t.Fatal("flat anchor exceeded the expansion budget")
	}
	if _, meta := inspect(strings.Replace(fixture, "description: offline", "description: '*not-an-alias &not-an-anchor'", 1)); !meta.Supported {
		t.Fatalf("quoted text refused: %+v", meta)
	}
}

func TestYAMLBudgetBoundsExpandedScalarBytes(t *testing.T) {
	raw := "value: &large '" + strings.Repeat("x", 32768) + "'\nvalues: [" + strings.TrimSuffix(strings.Repeat("*large,", 64), ",") + "]\n"
	if boundedYAML(raw) {
		t.Fatal("expanded scalar bytes exceeded the budget")
	}
}

func TestYAMLBudgetPreservesBundledSyntax(t *testing.T) {
	raw, err := os.ReadFile("../backend/internal/search/baseline/bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct{ Definitions map[string]string }
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	for name, input := range bundle.Definitions {
		if !boundedYAML(input) {
			t.Errorf("bundled syntax refused: %s", name)
		}
	}
	for _, input := range []string{"", "# comment\n", "---\n", "\ufeff" + fixture} {
		boundedYAML(input) // Empty or invalid input must not panic.
	}
}
