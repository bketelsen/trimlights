package schedule

import (
	"encoding/json"
	"os"
	"testing"
)

// TestSharedNameVectors applies the shared pattern name vectors to rule names.
func TestSharedNameVectors(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/name-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name  string
		Value string
		Valid bool
	}
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			rule := weekly()
			rule.Name = v.Value
			plan := Default()
			plan.Rules = []Rule{rule}
			if err := plan.Validate(); (err == nil) != v.Valid {
				t.Fatalf("%q valid=%v: %v", v.Value, v.Valid, err)
			}
		})
	}
}
