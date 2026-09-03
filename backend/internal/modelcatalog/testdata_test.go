package modelcatalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// 目录数据不再编译进二进制（无 go:embed）：生产环境的目录来自启动期本地
// 加载（镜像种子/缓存）或远程合并文档。测试用同一份仓库权威数据
// （deploy/data/models.json 的 models 段）播种全局目录，
// 使所有依赖「真实在售目录」的测试与生产数据保持同源。
func TestMain(m *testing.M) {
	if err := seedTestCatalog(); err != nil {
		fmt.Fprintln(os.Stderr, "seed test catalog failed:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func seedTestCatalog() error {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "data", "models.json"))
	if err != nil {
		return fmt.Errorf("read repo model data document: %w", err)
	}
	var doc struct {
		Version int             `json:"version"`
		Models  json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("decode repo model data document: %w", err)
	}
	if len(doc.Models) == 0 || doc.Models[0] != '[' {
		return fmt.Errorf("repo model data document has no models section")
	}
	version := doc.Version
	if version <= 0 {
		version = 1
	}
	section, err := json.Marshal(struct {
		Version int             `json:"version"`
		Models  json.RawMessage `json:"models"`
	}{version, doc.Models})
	if err != nil {
		return err
	}
	cat, err := Load(section)
	if err != nil {
		return fmt.Errorf("load catalog section from repo document: %w", err)
	}
	Replace(cat)
	return nil
}
