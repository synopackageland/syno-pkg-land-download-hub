package main

import (
	"context"
	"fmt"
	"testing"
)

func TestCatalogIsIndependentOfThe64SearchSlots(t *testing.T) {
	definitions := map[string]string{}
	for i := 0; i < 65; i++ {
		definitions[fmt.Sprintf("site-%03d.yml", i)] = fixture
	}
	out := run(context.Background(), request{Op: "catalog", Definitions: definitions}, nil)
	if out.Code != "" || len(out.Sites) != len(definitions) {
		t.Fatalf("catalog truncated: %d %s", len(out.Sites), out.Code)
	}
	for _, site := range out.Sites {
		if !site.Supported {
			t.Fatal(site)
		}
	}
}
