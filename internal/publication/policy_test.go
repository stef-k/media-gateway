package publication

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stef-k/media-gateway/internal/config"
)

// examplePolicy mirrors the validated policy vocabulary without loading credentials.
func examplePolicy() config.Policy {
	return config.Policy{AllowedRoots: []string{"/photos"}, Rules: []config.Rule{
		{Segment: "public", Media: []string{"image", "video"}},
		{Segment: "public-images", Media: []string{"image"}},
		{Segment: "public-videos", Media: []string{"video"}},
	}}
}

// TestEligible proves directory, root and media boundaries with provider metadata.
func TestEligible(t *testing.T) {
	tests := []struct {
		name, path, media string
		want              bool
	}{
		{"public image", "/photos/public/a.jpg", "image", true},
		{"public video", "/photos/public/a.mp4", "video", true},
		{"nested directory", "/photos/trip/public/day/a.jpg", "image", true},
		{"private asset", "/photos/private/a.jpg", "image", false},
		{"image rule", "/photos/public-images/a.jpg", "image", true},
		{"image rule denies video", "/photos/public-images/a.jpg", "video", false},
		{"video rule", "/photos/public-videos/a.mp4", "video", true},
		{"video rule denies image", "/photos/public-videos/a.jpg", "image", false},
		{"any permitting rule", "/photos/public-videos/public-images/a", "image", true},
		{"any permitting rule reversed", "/photos/public-images/public-videos/a", "image", true},
		{"suffix", "/photos/public-old/a.jpg", "image", false},
		{"prefix", "/photos/my-public/a.jpg", "image", false},
		{"near miss", "/photos/publicity/a.jpg", "image", false},
		{"case sensitive", "/photos/Public/a.jpg", "image", false},
		{"outside root", "/elsewhere/public/a.jpg", "image", false},
		{"above root", "/public/photos/a.jpg", "image", false},
		{"basename only", "/photos/public", "image", false},
		{"basename text", "/photos/public.jpg", "image", false},
		{"root prefix", "/photos-private/public/a.jpg", "image", false},
		{"root itself", "/photos", "image", false},
		{"unknown media", "/photos/public/a.jpg", "audio", false},
		{"empty media", "/photos/public/a.jpg", "", false},
		{"unnormalized media", "/photos/public/a.jpg", "IMAGE", false},
		{"mime type", "/photos/public/a.jpg", "image/jpeg", false},
		{"literal percent segment", "/photos/%70ublic/a.jpg", "image", false},
		{"unicode filename", "/photos/public/εικόνα.jpg", "image", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Eligible(examplePolicy(), tt.path, tt.media); got != tt.want {
				t.Fatalf("Eligible(%q, %q) = %v, want %v", tt.path, tt.media, got, tt.want)
			}
		})
	}
}

// TestMalformedPaths ensures normalization never repairs metadata into permission.
func TestMalformedPaths(t *testing.T) {
	for _, value := range []string{
		"", "/", "photos/public/a.jpg", " /photos/public/a.jpg", "/photos/public/a.jpg ",
		"/photos/./public/a.jpg", "/photos/private/../public/a.jpg", "/photos/public/.",
		"/photos/public/..", "/photos//public/a.jpg", "//photos/public/a.jpg",
		"/photos/public/a.jpg/", "/photos/public/", "/photos/public/a\\b.jpg",
		"/photos/public/a\x00.jpg", "/photos/public/a\n.jpg", "/photos/public/a\x7f.jpg",
		"/photos/public/a\u0085.jpg", "/photos/public/a\xff.jpg",
	} {
		t.Run(value, func(t *testing.T) {
			if Eligible(examplePolicy(), value, "image") {
				t.Fatalf("malformed path allowed: %q", value)
			}
		})
	}
}

// TestConfiguredRoots proves shared-rule union semantics independent of root order.
func TestConfiguredRoots(t *testing.T) {
	tests := []struct {
		name          string
		roots         []string
		segment, path string
		want          bool
	}{
		{"custom installation", []string{"/external/photos"}, "website", "/external/photos/website/a", true},
		{"custom literal", []string{"/external/photos"}, "website", "/external/photos/website-old/a", false},
		{"segment inside root", []string{"/photos/public"}, "public", "/photos/public/a", false},
		{"segment above root", []string{"/public/photos"}, "public", "/public/photos/a", false},
		{"segment below named root", []string{"/photos/public"}, "public", "/photos/public/public/a", true},
		{"nested union", []string{"/photos/public", "/photos"}, "public", "/photos/public/a", true},
		{"nested private", []string{"/photos/trip", "/photos"}, "public", "/photos/trip/private/a", false},
		{"slash root", []string{"/"}, "public", "/public/a", true},
		{"slash basename", []string{"/"}, "public", "/public", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := config.Policy{AllowedRoots: tt.roots, Rules: []config.Rule{{Segment: tt.segment, Media: []string{"image"}}}}
			if got := Eligible(p, tt.path, "image"); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			slices.Reverse(p.AllowedRoots)
			if got := Eligible(p, tt.path, "image"); got != tt.want {
				t.Fatalf("reversed roots: got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestInputUnchanged checks the caller-owned policy slices as well as path/media values.
func TestInputUnchanged(t *testing.T) {
	p, before := examplePolicy(), examplePolicy()
	value, media := "/photos/private/../public/a.jpg", "image"
	if Eligible(p, value, media) {
		t.Fatal("ambiguous path allowed")
	}
	if !Eligible(p, "/photos/public/a.jpg", media) {
		t.Fatal("eligible path denied")
	}
	if !reflect.DeepEqual(p, before) || value != "/photos/private/../public/a.jpg" || media != "image" {
		t.Fatal("evaluation mutated caller input")
	}
	if Eligible(config.Policy{}, "/photos/public/a.jpg", "image") {
		t.Fatal("empty policy allowed")
	}
}

// FuzzEligible compares arbitrary metadata with an independent component-based oracle.
func FuzzEligible(f *testing.F) {
	for _, seed := range []string{"/photos/public/a", "/photos/private/a", "/photos/../public/a", "/photos/public/", "/photos//public/a", "/photos/public/a\x00"} {
		f.Add(seed, "image")
	}
	f.Add("/photos/public/a", "video")
	f.Add("/photos/public/a", "unknown")
	f.Fuzz(func(t *testing.T, value, media string) {
		parts := strings.Split(value, "/")
		valid := utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\\") && !strings.ContainsFunc(value, unicode.IsControl)
		valid = valid && len(parts) >= 4 && parts[0] == "" && parts[1] == "photos"
		for _, component := range parts[1:] {
			if component == "" || component == "." || component == ".." {
				valid = false
			}
		}
		want := false
		if valid {
			for _, dir := range parts[2 : len(parts)-1] {
				want = want || (dir == "public" && (media == "image" || media == "video")) || (dir == "public-images" && media == "image") || (dir == "public-videos" && media == "video")
			}
		}
		if got := Eligible(examplePolicy(), value, media); got != want {
			t.Fatalf("Eligible(%q, %q) = %v, want %v", value, media, got, want)
		}
	})
}
