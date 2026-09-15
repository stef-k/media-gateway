package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestConsumerMigration rejects stale settings without returning private partial state.
func TestConsumerMigration(t *testing.T) {
	base, _ := fixture(t)
	for _, section := range []string{"[consumer]\nexpose_coordinates = false", "[consumer]\nexpose_coordinates = 'private-marker'", "consumer.expose_coordinates = true"} {
		c, key, err := loadText(t, section+"\n"+base)
		if err == nil || !strings.Contains(err.Error(), "coordinates are now always included") || strings.Contains(err.Error(), "private-marker") || key != "" || !reflect.DeepEqual(c, Config{}) {
			t.Fatalf("migration: %v", err)
		}
	}
	for _, section := range []string{"[consumer]", "[consumer]\nexpose_exif=true", "[unknown]\nvalue=true"} {
		_, key, err := loadText(t, base+"\n"+section)
		if err == nil || key != "" {
			t.Fatal("unknown config accepted")
		}
	}
	if _, _, err := loadText(t, base); err != nil {
		t.Fatal(err)
	}
}
