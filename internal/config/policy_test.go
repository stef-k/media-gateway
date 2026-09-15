package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestNamedRoots exercises syntax, canonical paths and component-aware overlap at Load.
func TestNamedRoots(t *testing.T) {
	base, _ := fixture(t)
	cases := []struct {
		name, roots string
		valid       bool
	}{
		{"single character", "name='a'\npath='/photos'", true},
		{"digit", "name='0'\npath='/photos'", true},
		{"64 characters", "name='" + strings.Repeat("a", 64) + "'\npath='/photos'", true},
		{"hyphens", "name='family-photos2'\npath='/photos'", true},
		{"near prefix", "name='images'\npath='/Images'\n[[policy.roots]]\nname='old'\npath='/Images-old'", true},
		{"empty name", "name=''\npath='/photos'", false},
		{"uppercase", "name='Images'\npath='/photos'", false},
		{"underscore", "name='images_1'\npath='/photos'", false},
		{"slash name", "name='images/art'\npath='/photos'", false},
		{"leading hyphen", "name='-images'\npath='/photos'", false},
		{"trailing hyphen", "name='images-'\npath='/photos'", false},
		{"65 characters", "name='" + strings.Repeat("a", 65) + "'\npath='/photos'", false},
		{"unicode", "name='εικόνες'\npath='/photos'", false},
		{"whitespace", "name=' images'\npath='/photos'", false},
		{"duplicate names", "name='images'\npath='/photos'\n[[policy.roots]]\nname='images'\npath='/art'", false},
		{"duplicate paths", "name='images'\npath='/photos'\n[[policy.roots]]\nname='art'\npath='/photos'", false},
		{"ancestor first", "name='images'\npath='/photos'\n[[policy.roots]]\nname='art'\npath='/photos/art'", false},
		{"descendant first", "name='art'\npath='/photos/art'\n[[policy.roots]]\nname='images'\npath='/photos'", false},
	}
	for _, path := range []string{"", "/", "relative", "/photos/", "/photos//art", "//photos", "/photos/./art", "/photos/../art", `/photos\art`, " /photos", "/photos "} {
		cases = append(cases, struct {
			name, roots string
			valid       bool
		}{"invalid path " + path, "name='images'\npath='" + path + "'", false})
	}
	cases = append(cases, struct {
		name, roots string
		valid       bool
	}{"control path", "name='images'\npath=\"/photos/\\nart\"", false})
	start := strings.Index(base, "[[policy.roots]]")
	end := strings.Index(base, "# Publication rules")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := base[:start] + "[[policy.roots]]\n" + tc.roots + "\n" + base[end:]
			c, key, err := loadText(t, text)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if err != nil && (!reflect.DeepEqual(c, Config{}) || key != "") {
				t.Fatal("returned partial state")
			}
		})
	}
	c, key, err := loadText(t, base[:start]+base[end:])
	if err == nil || !reflect.DeepEqual(c, Config{}) || key != "" {
		t.Fatal("missing roots did not fail closed")
	}
}

// TestRuleScopes proves nil/empty decoder semantics and unordered scope identity.
func TestRuleScopes(t *testing.T) {
	base, _ := fixture(t)
	start, end := strings.Index(base, "[[policy.rules]]"), strings.Index(base, "[delivery]")
	base = base[:start] + "[[policy.roots]]\nname='art'\npath='/art'\n" + base[start:]
	start, end = strings.Index(base, "[[policy.rules]]"), strings.Index(base, "[delivery]")
	rule := "[[policy.rules]]\nsegment='post'\nmedia=['image']\n"
	cases := []struct {
		name, rules string
		valid       bool
	}{
		{"global", rule, true},
		{"scoped", rule + "roots=['images']\n", true},
		{"explicit empty", rule + "roots=[]\n", false},
		{"unknown", rule + "roots=['missing']\n", false},
		{"exact reference", rule + "roots=['Images']\n", false},
		{"duplicate reference", rule + "roots=['images','images']\n", false},
		{"duplicate global", rule + rule, false},
		{"duplicate scope different media", rule + "roots=['images','art']\n" + strings.ReplaceAll(rule, "'image'", "'video'") + "roots=['art','images']\n", false},
		{"global and all roots scoped", rule + rule + "roots=['images','art']\n", true},
		{"distinct scopes", rule + "roots=['images']\n" + rule + "roots=['art']\n", true},
		{"overlapping scopes", rule + "roots=['images']\n" + rule + "roots=['images','art']\n", true},
		{"no rules", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, key, err := loadText(t, base[:start]+tc.rules+base[end:])
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if err != nil && (!reflect.DeepEqual(c, Config{}) || key != "") {
				t.Fatal("returned partial state")
			}
			if tc.name == "global" && c.Policy.Rules[0].Roots != nil {
				t.Fatal("omitted scope is not nil")
			}
		})
	}
}

// TestObsoletePolicy rejects V0 fields without returning topology or credentials.
func TestObsoletePolicy(t *testing.T) {
	base, _ := fixture(t)
	for _, field := range []string{"allowed_roots", "'allowed_roots'"} {
		// A stale V0 config has no named-root table at all.
		start, end := strings.Index(base, "[[policy.roots]]"), strings.Index(base, "# Publication rules")
		stale := base[:start] + base[end:]
		text := strings.Replace(stale, "[policy]", "[policy]\n"+field+"=['/sentinel-private']", 1)
		c, key, err := loadText(t, text)
		if err == nil || !strings.Contains(err.Error(), "policy.allowed_roots") || !strings.Contains(err.Error(), "Policy v2") {
			t.Fatalf("missing migration diagnostic: %v", err)
		}
		if strings.Contains(err.Error(), "sentinel-private") || !reflect.DeepEqual(c, Config{}) || key != "" {
			t.Fatal("migration exposed private or partial state")
		}
	}
}
