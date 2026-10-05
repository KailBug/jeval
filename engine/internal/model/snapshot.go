package model

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// SnapshotID binds source identity, source bytes and normalization rules. Event
// IDs are only stable within this snapshot, never across a source replacement.
func SnapshotID(sourceID, sourceSHA256, adapterVersion string) string {
	data, _ := json.Marshal([]any{1, sourceID, sourceSHA256, adapterVersion})
	return fmt.Sprintf("snapshot-%x", sha256.Sum256(data))
}
