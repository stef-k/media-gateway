package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestUnsupportedDelivery proves unsupported gates fail with targeted, sanitized guidance.
func TestUnsupportedDelivery(t *testing.T) {
	base, _ := fixture(t)
	for _, stale := range []string{"[delivery]\n", "[delivery]\nallow_original=false\nimage_variant='preview'", "['delivery']\nimage_variant='private-marker'", "delivery={allow_original=true}"} {
		// Put inline syntax at the document root, table syntax after the current config.
		text := base + "\n" + stale
		if strings.HasPrefix(stale, "delivery=") {
			text = stale + "\n" + base
		}
		c, key, err := loadText(t, text)
		if err == nil || !strings.Contains(err.Error(), "preview/original") || !strings.Contains(err.Error(), "remove the delivery section") {
			t.Fatalf("missing unsupported-field: %v", err)
		}
		if !reflect.DeepEqual(c, Config{}) || key != "" || strings.Contains(err.Error(), "private-marker") {
			t.Fatal("partial state or private values")
		}
	}
}
