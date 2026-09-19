package immich

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
)

// TestDiscoveryStreams fixes effective rule union and declaration-independent order.
func TestDiscoveryStreams(t *testing.T) {
	policy := config.Policy{Roots: []config.Root{{Name: "z", Path: "/z"}, {Name: "a", Path: "/a"}}, Rules: []config.Rule{
		{Segment: "public", Media: []string{"video"}},
		{Segment: "public", Media: []string{"image"}, Roots: []string{"a"}},
		{Segment: "post", Media: []string{"image"}, Roots: []string{"a"}},
	}}
	want := []DiscoveryStream{{Root: policy.Roots[1], Segment: "post", Media: []string{"image"}}, {Root: policy.Roots[1], Segment: "public", Media: []string{"image", "video"}}, {Root: policy.Roots[0], Segment: "public", Media: []string{"video"}}}
	if got := DiscoveryStreams(policy); !reflect.DeepEqual(got, want) {
		t.Fatalf("streams: %+v", got)
	}
	policy.Roots[0], policy.Roots[1] = policy.Roots[1], policy.Roots[0]
	policy.Rules[0], policy.Rules[2] = policy.Rules[2], policy.Rules[0]
	if !reflect.DeepEqual(DiscoveryStreams(policy), want) {
		t.Fatal("declaration order changed discovery")
	}
}

// TestCatalogSearchModes checks fixed filters, literal SQL pattern escaping,
// image/video decoding, and discovery independence from unrelated malformed metadata.
func TestCatalogSearchModes(t *testing.T) {
	for _, discovery := range []bool{false, true} {
		t.Run(fmt.Sprint(discovery), func(t *testing.T) {
			query := CandidateQuery{IncludeSourceMetadata: true, Limit: 1, Collection: &CollectionSelector{Root: config.Root{Name: "images", Path: "/root_%"}, Path: "public_%"}}
			pathFilter := map[string]any{"startsWith": `/root\_\%/public\_\%/`}
			media := []any{"IMAGE", "VIDEO"}
			if discovery {
				query.Collection = nil
				query.Discovery = &DiscoveryStream{Root: config.Root{Name: "images", Path: "/root_%"}, Segment: "public_%", Media: []string{"video"}}
				pathFilter = map[string]any{"startsWith": `/root\_\%/`, "like": `/public\_\%/`}
				media = []any{"VIDEO"}
			}
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				var got map[string]any
				json.NewDecoder(r.Body).Decode(&got)
				wantFilter := map[string]any{"type": map[string]any{"in": media}, "originalPath": pathFilter, "isOffline": map[string]any{"eq": false}, "trashedAt": map[string]any{"eq": nil}}
				if !reflect.DeepEqual(got["filter"], wantFilter) || got["withExif"] != !discovery || got["withPeople"] != false || got["withStacked"] != false || r.URL.Path != "/api/search/metadata" {
					t.Errorf("wrong fixed request: %v", got)
				}
				raw := strings.Replace(candidateJSON, "IMAGE", "VIDEO", 1)
				if discovery {
					raw = strings.Replace(raw, `"width":1200`, `"width":"invalid"`, 1)
					raw = strings.Replace(raw, "2026-09-12T12:34:56.123Z", "invalid", 1)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, candidateResponse(raw, `null`, 1))
			}, time.Second)
			page, err := client.SearchCandidates(context.Background(), query)
			if err != nil || len(page.Items) != 1 || page.Items[0].Media != "video" {
				t.Fatalf("mode mapping: %+v %v", page, err)
			}
		})
	}
}

// TestDurationValidation requires explicit nullable nonnegative safe integers in milliseconds.
func TestDurationValidation(t *testing.T) {
	for _, value := range []string{"null", "0", "23800", "-1", "1.5", `"23"`, "9007199254740992"} {
		raw := strings.Replace(candidateJSON, `"duration":null`, `"duration":`+value, 1)
		item, err := decodeCandidate([]byte(raw), assetID, false, true)
		valid := value == "null" || value == "0" || value == "23800"
		if (err == nil) != valid {
			t.Errorf("duration %s: %v", value, err)
		}
		if value == "23800" && (item.DurationMS == nil || *item.DurationMS != 23800) {
			t.Fatal("duration units changed")
		}
	}
	raw := strings.Replace(candidateJSON, `"duration":null,`, "", 1)
	if _, err := decodeCandidate([]byte(raw), assetID, false, true); err != ErrMetadata {
		t.Fatal("missing duration accepted")
	}
}
