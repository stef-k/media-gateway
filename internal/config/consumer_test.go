package config

import (
	"strings"
	"testing"
)

// TestConsumerConfiguration proves default-off behavior and strict typed decoding.
func TestConsumerConfiguration(t *testing.T) {
	base, _ := fixture(t)
	// Remove the documented section to independently exercise its omission.
	base = strings.Split(base, "[consumer]")[0]
	for _, tc := range []struct {
		name, section    string
		enabled, invalid bool
	}{
		{"omitted", "", false, false},
		{"empty", "[consumer]", false, false},
		{"disabled", "[consumer]\nexpose_coordinates = false", false, false},
		{"enabled", "[consumer]\nexpose_coordinates = true", true, false},
		{"unknown", "[consumer]\nexpose_exif = true", false, true},
		{"string", "[consumer]\nexpose_coordinates = 'private-marker'", false, true},
		{"number", "[consumer]\nexpose_coordinates = 1", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, key, err := loadText(t, base+"\n"+tc.section)
			if tc.invalid {
				if err == nil || key != "" || strings.Contains(err.Error(), "private-marker") {
					t.Fatal("invalid configuration was not rejected safely")
				}
			} else if err != nil || c.Consumer.ExposeCoordinates != tc.enabled {
				t.Fatalf("consumer configuration: %v", err)
			}
		})
	}
}
