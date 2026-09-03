package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestClassifyModelDataShapes 固定形态识别契约：合法三种形态正确归类，
// 畸形 section 整份拒收（P2-1 回归：models 是数组而 prices 非对象时，
// 不得静默降级为 catalog-only 丢弃 prices 段）。
func TestClassifyModelDataShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want modelDataShape
	}{
		// 合法形态
		{"merged", `{"version":1,"models":[],"prices":{"m":{}}}`, shapeMerged},
		{"catalog-only", `{"version":1,"models":[{"id":"m"}]}`, shapeCatalogOnly},
		{"flat price map", `{"gpt-5.6":{"input_cost_per_token":1e-06}}`, shapePricesOnly},
		{"flat map with colliding entry names",
			`{"models":{"input_cost_per_token":1},"prices":{"output_cost_per_token":2}}`, shapePricesOnly},
		// 畸形 section：整份拒收
		{"models array + prices array", `{"models":[],"prices":[]}`, shapeUnknown},
		{"models array + prices null", `{"models":[],"prices":null}`, shapeUnknown},
		{"models array + prices scalar", `{"models":[],"prices":42}`, shapeUnknown},
		{"prices array without models", `{"prices":[1,2]}`, shapeUnknown},
		// 顶层非对象 / 非法 JSON：拒收
		{"top-level array", `[1,2]`, shapeUnknown},
		{"top-level string", `"hello"`, shapeUnknown},
		{"malformed json", `{"models":`, shapeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, classifyModelData([]byte(tc.body)), tc.body)
		})
	}
}

// TestDecodeModelDataRejectsMalformedMerged 端到端拒收契约：畸形合并文档在
// decode 层就报错（applyModelData 不会被触达），调用方保留 last-good。
func TestDecodeModelDataRejectsMalformedMerged(t *testing.T) {
	_, err := decodeModelData([]byte(`{"version":1,"models":[{"id":"m"}],"prices":[1]}`))
	require.Error(t, err, "prices 段为数组的合并文档必须整份拒收")

	for _, body := range []string{
		`{"version":1,"models":[],"prices":{"m":{}}}`,
		`{"version":1,"models":[{"id":"m"}]}`,
		`{"m":{"input_cost_per_token":1}}`,
	} {
		doc, err := decodeModelData([]byte(body))
		require.NoError(t, err, body)
		require.Equal(t, 64, len(doc.hash), body)
	}
}
