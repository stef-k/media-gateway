package immich

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAssetLifecycle requires explicit booleans and returns no metadata on denial.
func TestAssetLifecycle(t *testing.T) {
	good := response(assetID, "/external/photos/website/photo.jpg", "IMAGE")
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"active", good, nil},
		{"trashed", strings.Replace(good, `"isTrashed":false`, `"isTrashed":true`, 1), ErrMissing},
		{"offline", strings.Replace(good, `"isOffline":false`, `"isOffline":true`, 1), ErrMissing},
		{"both", strings.ReplaceAll(good, `:false`, `:true`), ErrMissing},
		{"missing trashed", strings.Replace(good, `"isTrashed":false,`, "", 1), ErrMetadata},
		{"missing offline", strings.Replace(good, `"isOffline":false,`, "", 1), ErrMetadata},
		{"null trashed", strings.Replace(good, `"isTrashed":false`, `"isTrashed":null`, 1), ErrMetadata},
		{"null offline", strings.Replace(good, `"isOffline":false`, `"isOffline":null`, 1), ErrMetadata},
		{"string trashed", strings.Replace(good, `"isTrashed":false`, `"isTrashed":"false"`, 1), ErrMetadata},
		{"number offline", strings.Replace(good, `"isOffline":false`, `"isOffline":0`, 1), ErrMetadata},
		{"trashed with invalid offline", strings.ReplaceAll(strings.Replace(good, `"isOffline":false`, `"isOffline":null`, 1), `:false`, `:true`), ErrMetadata},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.body)
			}, time.Second)
			got, err := client.Asset(context.Background(), assetID)
			if !errors.Is(err, tc.want) || (err != nil && got != (Metadata{})) {
				t.Fatalf("unexpected lifecycle outcome: %v", err)
			}
			if err == nil && got.ID != assetID {
				t.Fatal("active asset lost")
			}
		})
	}
}
