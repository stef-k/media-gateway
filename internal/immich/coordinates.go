package immich

import (
	"encoding/json"
	"math"
)

// decodeCoordinates discards unrelated EXIF and accepts only a nullable pair.
// JSON decoding rejects nonnumbers and overflowing numbers without exposing values.
func decodeCoordinates(raw json.RawMessage) (*float64, *float64, error) {
	var exif struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if len(raw) != 0 && json.Unmarshal(raw, &exif) != nil {
		return nil, nil, ErrMetadata
	}
	lat, lon := exif.Latitude, exif.Longitude
	if (lat == nil) != (lon == nil) {
		return nil, nil, ErrMetadata
	}
	if lat != nil && (math.IsNaN(*lat) || math.IsInf(*lat, 0) || *lat < -90 || *lat > 90 ||
		math.IsNaN(*lon) || math.IsInf(*lon, 0) || *lon < -180 || *lon > 180) {
		return nil, nil, ErrMetadata
	}
	return lat, lon, nil
}
