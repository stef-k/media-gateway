package publication

import (
	"reflect"
	"testing"

	"github.com/stef-k/media-gateway/internal/config"
)

// scopedPolicy supplies global and overlapping additive scopes without I/O.
func scopedPolicy() config.Policy {
	return config.Policy{
		Roots: []config.Root{{Name: "images", Path: "/media/archive/Images"}, {Name: "art", Path: "/media/archive/ART"}},
		Rules: []config.Rule{
			{Segment: "public", Media: []string{"image", "video"}},
			{Segment: "post", Media: []string{"image"}, Roots: []string{"images"}},
			{Segment: "post", Media: []string{"video"}, Roots: []string{"art"}},
			{Segment: "post", Media: []string{"video"}, Roots: []string{"images", "art"}},
			{Segment: "public", Media: []string{"image"}, Roots: []string{"images"}},
		},
	}
}

// TestScopedContext checks exact collection context and zero context on denial.
func TestScopedContext(t *testing.T) {
	cases := []struct{ path, media, root, collection string }{
		{"/media/archive/Images/2019/Romania/post/DSC1.JPG", "image", "images", "2019/Romania/post"},
		{"/media/archive/Images/2023/TAIWAN/public/day-1/IMG1.JPG", "image", "images", "2023/TAIWAN/public/day-1"},
		{"/media/archive/ART/public/a", "image", "art", "public"},
		{"/media/archive/ART/post/a", "image", "", ""},
		{"/media/archive/ART/post/a", "video", "art", "post"},
		{"/media/archive/Images/post/a", "video", "images", "post"},
		{"/media/archive/Images/post process/a", "image", "", ""},
		{"/media/archive/Images/post-old/a", "image", "", ""},
		{"/media/archive/Images/Post/a", "image", "", ""},
		{"/media/archive/Images/private/post", "image", "", ""},
		{"/media/archive/Images-old/post/a", "image", "", ""},
		{"/media/archive/Images/private/../post/a", "image", "", ""},
	}
	p, before := scopedPolicy(), scopedPolicy()
	for _, tc := range cases {
		got, ok := Evaluate(p, tc.path, tc.media)
		want := Match{RootName: tc.root, CollectionPath: tc.collection}
		if ok != (tc.root != "") || got != want {
			t.Fatalf("Evaluate(%q,%q)=(%#v,%v), want %#v", tc.path, tc.media, got, ok, want)
		}
	}
	if !reflect.DeepEqual(p, before) {
		t.Fatal("evaluation mutated scoped policy")
	}
}

// TestAmbiguousContext denies before rule matching even if only one root permits.
func TestAmbiguousContext(t *testing.T) {
	for _, roots := range [][]config.Root{
		nil,
		{{Name: "images", Path: "/media/archive/Images"}, {Name: "nested", Path: "/media/archive/Images/post"}},
		{{Name: "nested", Path: "/media/archive/Images/post"}, {Name: "images", Path: "/media/archive/Images"}},
		{{Name: "images", Path: "/media/archive/Images"}, {Name: "duplicate", Path: "/media/archive/Images"}},
	} {
		p := scopedPolicy()
		p.Roots = roots
		if got, ok := Evaluate(p, "/media/archive/Images/post/a", "image"); ok || got != (Match{}) {
			t.Fatalf("ambiguous policy returned %#v, %v", got, ok)
		}
	}
}
