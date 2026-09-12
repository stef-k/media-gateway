package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fixture adapts the committed operator example using a separate temporary secret.
func fixture(t *testing.T) (string, string) {
	t.Helper()
	data, err := os.ReadFile("../../deploy/config.toml.example")
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "provider.key")
	if err := os.WriteFile(key, []byte("test-secret-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "/etc/media-gateway/immich.key", key), key
}

// loadText exercises the real explicit-file startup boundary.
func loadText(t *testing.T, text string) (Config, string, error) {
	t.Helper()
	name := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(name, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return Load(name)
}

func TestLoadDocumentedConfiguration(t *testing.T) {
	text, keyPath := fixture(t)
	c, key, err := loadText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	if key != "test-secret-token" || c.Provider.APIKeyFile != keyPath || c.Provider.RequestTimeout != 15*time.Second || len(c.Policy.Rules) != 3 || c.Delivery.ImageVariant != "preview" {
		t.Fatalf("unexpected loaded fields")
	}
	again, againKey, err := loadText(t, text)
	if err != nil || !reflect.DeepEqual(c, again) || key != againKey {
		t.Fatal("load is not deterministic")
	}
	text = strings.ReplaceAll(text, "/media/archive", "/external/photos")
	text = strings.ReplaceAll(text, `segment = "public"`, `segment = "website"`)
	text = strings.ReplaceAll(text, `public_base_url = "https://media.example.com"`, "")
	text = strings.ReplaceAll(text, `127.0.0.1:2290`, `[::1]:2290`)
	c, _, err = loadText(t, text)
	if err != nil || c.Policy.AllowedRoots[0] != "/external/photos" || c.Policy.Rules[0].Segment != "website" || c.Server.PublicBaseURL != "" {
		t.Fatalf("installation-specific configuration failed: %v", err)
	}
}

func TestRejectInvalidConfiguration(t *testing.T) {
	base, _ := fixture(t)
	cases := []struct{ name, old, replacement string }{
		{"unknown field", "[server]", "[server]\nsecret_typo = 'sentinel-private'"},
		{"unknown table", "[server]", "[unexpected]\nvalue = 1\n[server]"},
		{"wrong type", `request_timeout = "15s"`, `request_timeout = 15`},
		{"syntax", "[server]", "[server"},
		{"relative root", `["/media/archive"]`, `["relative"]`},
		{"empty root", `["/media/archive"]`, `[""]`},
		{"no roots", `["/media/archive"]`, `[]`},
		{"duplicate root", `["/media/archive"]`, `["/media/archive", "/media/archive"]`},
		{"unclean root", `["/media/archive"]`, `["/media/../archive"]`},
		{"double slash root", `["/media/archive"]`, `["/media//archive"]`},
		{"root backslash", `["/media/archive"]`, `['/media\archive']`},
		{"root control", `["/media/archive"]`, `["/media/\narchive"]`},
		{"provider type", `type = "immich"`, `type = "other"`},
		{"url scheme", `http://127.0.0.1:2283`, `file:///private`},
		{"url missing host", `http://127.0.0.1:2283`, `http://`},
		{"url credentials", `http://127.0.0.1:2283`, `http://sentinel-private@host`},
		{"url query", `http://127.0.0.1:2283`, `http://host?secret=sentinel-private`},
		{"url fragment", `http://127.0.0.1:2283`, `http://host#`},
		{"url path", `http://127.0.0.1:2283`, `http://host/api`},
		{"url invalid port", `http://127.0.0.1:2283`, `http://host:65536`},
		{"public url", `https://media.example.com`, `not-a-url`},
		{"credential relative", `api_key_file = "`, `api_key_file = "relative`},
		{"empty segment", `segment = "public"`, `segment = ""`},
		{"duplicate segment", `segment = "public-images"`, `segment = "public"`},
		{"dot segment", `segment = "public"`, `segment = "."`},
		{"parent segment", `segment = "public"`, `segment = ".."`},
		{"slash segment", `segment = "public"`, `segment = "public/child"`},
		{"backslash segment", `segment = "public"`, `segment = 'public\child'`},
		{"glob segment", `segment = "public"`, `segment = "public*"`},
		{"regex segment", `segment = "public"`, `segment = "(public|website)"`},
		{"unknown media", `["image", "video"]`, `["audio"]`},
		{"duplicate media", `["image", "video"]`, `["image", "image"]`},
		{"empty media", `["image", "video"]`, `[]`},
		{"invalid duration", `request_timeout = "15s"`, `request_timeout = "forever"`},
		{"zero duration", `request_timeout = "15s"`, `request_timeout = "0s"`},
		{"negative duration", `request_timeout = "15s"`, `request_timeout = "-1s"`},
		{"missing duration", `request_timeout = "15s"`, ``},
		{"external listener", `127.0.0.1:2290`, `0.0.0.0:2290`},
		{"hostname listener", `127.0.0.1:2290`, `localhost:2290`},
		{"zero port", `127.0.0.1:2290`, `127.0.0.1:0`},
		{"originals", `allow_original = false`, `allow_original = true`},
		{"variant", `image_variant = "preview"`, `image_variant = "original"`},
		{"missing variant", `image_variant = "preview"`, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := strings.ReplaceAll(base, tc.old, tc.replacement)
			c, key, err := loadText(t, text)
			if err == nil {
				t.Fatal("accepted invalid configuration")
			}
			if !reflect.DeepEqual(c, Config{}) || key != "" {
				t.Fatal("returned partial state")
			}
			if strings.Contains(err.Error(), "sentinel-private") || strings.Contains(err.Error(), "test-secret-token") || strings.Contains(err.Error(), "/media/archive") {
				t.Fatal("error exposed input")
			}
		})
	}
}

func TestCredentialFailures(t *testing.T) {
	for _, scenario := range []string{"missing", "directory", "unreadable", "public", "empty", "multiline", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			text, keyPath := fixture(t)
			var err error
			switch scenario {
			case "missing":
				err = os.Remove(keyPath)
			case "directory":
				text = strings.ReplaceAll(text, keyPath, filepath.Dir(keyPath))
			case "unreadable":
				err = os.Chmod(keyPath, 0000)
			case "public":
				err = os.Chmod(keyPath, 0644)
			case "empty":
				err = os.WriteFile(keyPath, nil, 0600)
			case "multiline":
				err = os.WriteFile(keyPath, []byte("test-secret-token\nsecond"), 0600)
			case "oversized":
				err = os.WriteFile(keyPath, []byte(strings.Repeat("x", 4097)), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			c, key, err := loadText(t, text)
			if err == nil || key != "" || !reflect.DeepEqual(c, Config{}) {
				t.Fatal("credential failure did not fail closed")
			}
			if strings.Contains(err.Error(), keyPath) || strings.Contains(err.Error(), "test-secret-token") {
				t.Fatal("credential error leaked input")
			}
		})
	}
}

func TestMissingConfiguration(t *testing.T) {
	_, _, err := Load(filepath.Join(t.TempDir(), "private-config"))
	if err == nil || strings.Contains(err.Error(), "private-config") {
		t.Fatal("missing config error must be sanitized")
	}
}
