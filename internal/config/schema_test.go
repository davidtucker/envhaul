package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestSchemaInSync keeps schema/envhaul.schema.json aligned with the keys the
// loader accepts.
func TestSchemaInSync(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "envhaul.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
		Defs       struct {
			Component struct {
				Properties map[string]any `json:"properties"`
			} `json:"component"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	check := func(what string, got map[string]any, want []string) {
		var keys []string
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if a, b := strings.Join(keys, ","), strings.Join(sorted(want), ","); a != b {
			t.Errorf("%s keys differ\nschema: %s\nloader: %s", what, a, b)
		}
	}
	check("top-level", schema.Properties, TopLevelKeys)
	check("component", schema.Defs.Component.Properties, ComponentKeys)
}
