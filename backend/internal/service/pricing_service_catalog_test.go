package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/stretchr/testify/require"
)

const runtimeCatalogWithTestModel = `{
  "version": 1,
  "models": [
    {"id": "catalog-file-model", "platforms": ["openai"], "kind": "chat", "billing_mode": "token",
     "price": {"input_per_mtok": 1, "output_per_mtok": 2}}
  ]
}`

const runtimeCatalogWithOtherModel = `{
  "version": 1,
  "models": [
    {"id": "catalog-file-model-v2", "platforms": ["openai"], "kind": "chat", "billing_mode": "token",
     "price": {"input_per_mtok": 3, "output_per_mtok": 4}}
  ]
}`

// restoreEmbeddedCatalog 保证测试替换全局目录后恢复内嵌基线，
// 避免影响同包其它依赖内嵌目录的测试。
func restoreEmbeddedCatalog(t *testing.T) {
	t.Helper()
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })
}

func writeCatalogFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestResolveCatalogFilePathExplicitWins(t *testing.T) {
	dir := t.TempDir()
	explicit := writeCatalogFile(t, dir, "custom.json", runtimeCatalogWithTestModel)

	svc := NewPricingService(&config.Config{}, nil)
	svc.cfg.Pricing.CatalogFile = explicit
	require.Equal(t, explicit, svc.resolveCatalogFilePath())
}

func TestResolveCatalogFilePathExplicitMissingStillTracked(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "not-deployed-yet.json")
	svc := NewPricingService(&config.Config{}, nil)
	svc.cfg.Pricing.CatalogFile = missing
	// 显式配置但文件尚未就位（部署稍后放进容器）：继续跟踪该路径，
	// watcher 会在文件出现后自动换入。
	require.Equal(t, missing, svc.resolveCatalogFilePath())
}

func TestResolveCatalogFilePathAutoDiscover(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	svc := NewPricingService(&config.Config{}, nil)
	require.Equal(t, "", svc.resolveCatalogFilePath(), "没有任何文件时回落内嵌目录")

	writeCatalogFile(t, "data", "catalog.json", runtimeCatalogWithTestModel)
	require.FileExists(t, filepath.Join(dir, "data/catalog.json"))
	require.Equal(t, "./data/catalog.json", svc.resolveCatalogFilePath())
}

func TestLoadRuntimeCatalogFileFileWinsOverEmbedded(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	t.Chdir(dir)
	writeCatalogFile(t, "data", "catalog.json", runtimeCatalogWithTestModel)

	svc := NewPricingService(&config.Config{}, nil)
	svc.loadRuntimeCatalogFile()

	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"), "运行时文件里的模型必须可见")
	require.Nil(t, modelcatalog.Lookup("gemini-3.6-flash"), "文件整体替换内嵌目录，而不是合并")

	status := svc.catalogStatus()
	require.Equal(t, "./data/catalog.json", status["path"])
	require.Equal(t, "./data/catalog.json", status["source"])
	require.NotEmpty(t, status["models"])
}

func TestLoadRuntimeCatalogFileInvalidKeepsEmbedded(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	t.Chdir(dir)
	writeCatalogFile(t, "data", "catalog.json", `{"version": 77, "models": []}`)

	svc := NewPricingService(&config.Config{}, nil)
	svc.loadRuntimeCatalogFile()

	require.NotNil(t, modelcatalog.Lookup("gemini-3.6-flash"), "坏文件必须回落内嵌目录")
	require.Nil(t, modelcatalog.Lookup("catalog-file-model"))

	status := svc.catalogStatus()
	require.Equal(t, "embedded", status["source"])
	require.NotEmpty(t, status["last_error"])
}

func TestCatalogPollHotReloadAndInvalidRetry(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	t.Chdir(dir)
	writeCatalogFile(t, "data", "catalog.json", runtimeCatalogWithTestModel)

	svc := NewPricingService(&config.Config{}, nil)
	svc.loadRuntimeCatalogFile()
	path := svc.catalogPathLocked()
	require.Equal(t, "./data/catalog.json", path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"))

	// 未变化：poll 不得重复换入（无副作用即可断言通过）
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"))

	// 文件更新：换入新目录，旧模型消失
	require.NoError(t, os.WriteFile(path, []byte(runtimeCatalogWithOtherModel), 0o644))
	require.NoError(t, os.Chtimes(path, time.Now().Add(time.Second), time.Now().Add(time.Second)))
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model-v2"))
	require.Nil(t, modelcatalog.Lookup("catalog-file-model"))
	require.Equal(t, "./data/catalog.json", svc.catalogStatus()["source"])

	// 坏版本：保留上一份有效目录，不崩溃不清空
	require.NoError(t, os.WriteFile(path, []byte(`{"version": 2, "models": []}`), 0o644))
	require.NoError(t, os.Chtimes(path, time.Now().Add(2*time.Second), time.Now().Add(2*time.Second)))
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model-v2"), "坏版本不得覆盖上一份有效目录")
	require.NotEmpty(t, svc.catalogStatus()["last_error"])

	// 修复后自动恢复
	require.NoError(t, os.WriteFile(path, []byte(runtimeCatalogWithOtherModel), 0o644))
	require.NoError(t, os.Chtimes(path, time.Now().Add(3*time.Second), time.Now().Add(3*time.Second)))
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model-v2"))
	require.Empty(t, svc.catalogStatus()["last_error"])
}

func TestCatalogPollFileMissingKeepsLastGood(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	t.Chdir(dir)
	writeCatalogFile(t, "data", "catalog.json", runtimeCatalogWithTestModel)

	svc := NewPricingService(&config.Config{}, nil)
	svc.loadRuntimeCatalogFile()
	path := svc.catalogPathLocked()
	require.NotEmpty(t, path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"))

	require.NoError(t, os.Remove(path))
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"), "文件消失时保留上一份有效目录")
	require.NotEmpty(t, svc.catalogStatus()["last_error"])
}

func TestPricingServiceGetStatusIncludesCatalog(t *testing.T) {
	svc := NewPricingService(&config.Config{}, nil)
	svc.loadRuntimeCatalogFile()

	status := svc.GetStatus()
	catalog, ok := status["catalog"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "embedded", catalog["source"])
}
