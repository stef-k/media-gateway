package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestPrivacyConfig verifies conservative defaults and strict, atomic configuration loading.
func TestPrivacyConfig(t *testing.T) {
	base, _ := fixture(t)
	for _, tc := range []struct {
		section          string
		enabled, invalid bool
	}{
		{"", false, false}, {"[privacy]", false, false},
		{"[privacy]\nexpose_source_metadata=false", false, false},
		{"[privacy]\nexpose_source_metadata=true", true, false},
		{"[privacy]\nexpose_source_metadata='private-marker'", false, true},
		{"[privacy]\nunknown=true", false, true},
		{"[consumer]\nexpose_coordinates=false", false, true},
		{"[consumer]\nexpose_coordinates=true", false, true},
		{"[consumer]", false, true},
	} {
		t.Run(tc.section, func(t *testing.T) {
			c, key, err := loadText(t, base+"\n"+tc.section)
			if tc.invalid {
				if err == nil || key != "" || !reflect.DeepEqual(c, Config{}) || strings.Contains(err.Error(), "private-marker") {
					t.Fatalf("invalid config leaked state: %v", err)
				}
			} else if err != nil || c.Privacy.ExposeSourceMetadata != tc.enabled || key == "" {
				t.Fatalf("privacy config: %v", err)
			}
		})
	}
}
