package service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
)

// 目录数据不再编译进二进制（无 go:embed）：生产环境的目录来自启动期本地
// 加载（镜像种子/缓存）或远程合并文档。测试用同一份仓库权威数据
// （deploy/data/models.json）经生产解析路径播种全局目录，
// 使所有依赖「真实在售目录」的测试（storefront/定价/默认模型映射）
// 与生产数据保持同源。
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
	doc, err := decodeModelData(body)
	if err != nil {
		return fmt.Errorf("decode repo model data document: %w", err)
	}
	if !doc.hasCatalog {
		return fmt.Errorf("repo model data document has no models section")
	}
	cat, err := modelcatalog.Load(doc.catalogBody)
	if err != nil {
		return fmt.Errorf("load catalog section from repo document: %w", err)
	}
	modelcatalog.Replace(cat)
	return nil
}
