package model

type SnapshotRef struct {
	RunID      string `json:"runId"`
	SnapshotID string `json:"snapshotId"`
}
type ComparisonSide struct {
	Record      Record       `json:"record"`
	Annotations []Annotation `json:"annotations"`
}
type MetricDifference struct {
	Left  *int64 `json:"left"`
	Right *int64 `json:"right"`
	Delta *int64 `json:"delta"`
}
type Comparison struct {
	Format              string                      `json:"format"`
	FormatVersion       int                         `json:"formatVersion"`
	ContentScope        string                      `json:"contentScope"`
	SourceFilesIncluded bool                        `json:"sourceFilesIncluded"`
	FullContentIncluded bool                        `json:"fullContentIncluded"`
	GeneratedAt         string                      `json:"generatedAt"`
	Left                ComparisonSide              `json:"left"`
	Right               ComparisonSide              `json:"right"`
	Metrics             map[string]MetricDifference `json:"metrics"`
}

func Difference(left, right *int64) MetricDifference {
	value := MetricDifference{Left: left, Right: right}
	if left != nil && right != nil {
		delta := *right - *left
		value.Delta = &delta
	}
	return value
}
