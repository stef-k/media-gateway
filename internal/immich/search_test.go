package immich

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/publication"
)

// candidateJSON uses top-level dimensions/times; unrelated private EXIF is ignored.
const candidateJSON = `{"id":"a1234567-abcd-4abc-8abc-123456789abc","type":"IMAGE","isTrashed":false,"isOffline":false,"originalPath":"/outside/private/../photo.jpg","duration":null,"width":1200,"height":null,"fileCreatedAt":"2026-09-12T12:34:56.123Z","localDateTime":"2026-09-12T15:34:56.123Z","exifInfo":{"gps":"body-marker"}}`

// candidateResponse wraps synthetic assets in the reviewed search envelope.
func candidateResponse(items, cursor string, count int) string {
	return fmt.Sprintf(`{"assets":{"items":[%s],"nextCursor":%s,"count":%d}}`, items, cursor, count)
}

// TestSearchCandidatesContract checks the complete fixed request and narrow mapping.
func TestSearchCandidatesContract(t *testing.T) {
	for _, id := range []string{"", assetID} {
		t.Run("id="+id, func(t *testing.T) {
			cursor := `opaque/+?\"filter:VIDEO`
			query := CandidateQuery{Limit: 2, Cursor: cursor}
			filter := map[string]any{"type": map[string]any{"in": []any{"IMAGE", "VIDEO"}}, "isOffline": map[string]any{"eq": false}, "trashedAt": map[string]any{"eq": nil}}
			if id != "" {
				query = CandidateQuery{Limit: 1, ID: id}
				filter["id"] = map[string]any{"eq": id}
			}
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.RequestURI() != "/api/search/metadata" || r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != testKey || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
					t.Error("unexpected search request")
				}
				var got map[string]any
				if json.NewDecoder(r.Body).Decode(&got) != nil {
					t.Error("invalid request JSON")
				}
				want := map[string]any{"filter": filter, "orderBy": map[string]any{"field": "fileCreatedAt", "direction": "desc"}, "size": float64(query.Limit), "withExif": true, "withPeople": false, "withStacked": false}
				if query.Cursor != "" {
					want["cursor"] = query.Cursor
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("unexpected request shape: %v", got)
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				next := `"next-opaque"`
				if id != "" {
					next = `null`
				}
				fmt.Fprint(w, candidateResponse(candidateJSON, next, 1))
			}, time.Second)
			got, err := client.SearchCandidates(context.Background(), query)
			if err != nil || len(got.Items) != 1 {
				t.Fatalf("unexpected search: %v", err)
			}
			item := got.Items[0]
			if item.ID != assetID || item.Media != "image" || item.OriginalPath != "/outside/private/../photo.jpg" || item.Width == nil || *item.Width != 1200 || item.Height != nil || item.FileCreatedAt != "2026-09-12T12:34:56.123Z" || item.LocalDateTime != "2026-09-12T15:34:56.123Z" {
				t.Fatalf("unexpected mapping: %+v", item)
			}
			wantCursor := "next-opaque"
			if id != "" {
				wantCursor = ""
			}
			if got.NextCursor != wantCursor {
				t.Fatal("cursor changed")
			}
			policy := config.Policy{Roots: []config.Root{{Name: "images", Path: "/published"}}, Rules: []config.Rule{{Segment: "public", Media: []string{"image"}}}}
			if _, eligible := publication.Evaluate(policy, item.OriginalPath, item.Media); eligible {
				t.Fatal("private candidate authorized")
			}
		})
	}
}

// TestSearchInvalidInput proves only bounded pagination and exact IDs reach Immich.
func TestSearchInvalidInput(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) }, time.Second)
	for _, q := range []CandidateQuery{
		{Limit: 0}, {Limit: -1}, {Limit: 101}, {Limit: 1, Cursor: strings.Repeat("x", 1025)},
		{Limit: 1, Cursor: "bad\n"}, {Limit: 1, Cursor: "bad\u0085"}, {Limit: 1, Cursor: string([]byte{255})},
		{Limit: 1, ID: "https://example.com/" + assetID}, {Limit: 1, ID: "../" + assetID},
		{Limit: 1, ID: assetID + "?filter=VIDEO"}, {Limit: 1, ID: strings.Replace(assetID, "4abc", "7abc", 1)},
		{Limit: 1, ID: assetID, Cursor: "next"},
	} {
		got, err := client.SearchCandidates(context.Background(), q)
		if err == nil || got.Items != nil || got.NextCursor != "" {
			t.Fatal("invalid query accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid query contacted provider")
	}
}
