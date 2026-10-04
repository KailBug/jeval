package demo

import (
	_ "embed"
	"encoding/json"
	"jeval/engine/internal/model"
)

// Data is synthetic, has no credentials, and is embedded for offline packaged use.
//
//go:embed records.json
var data []byte

func Load() (model.Record, error) {
	var record model.Record
	err := json.Unmarshal(data, &record)
	return record, err
}
