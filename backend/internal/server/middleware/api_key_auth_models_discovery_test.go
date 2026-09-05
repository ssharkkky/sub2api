//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 模型发现端点（只读元数据）必须与主中间件的豁免保持一致：
// 零余额 Key 在全部发现路径上都能拿到渠道模型列表。
var modelsDiscoveryPaths = []string{
	"/v1/models",
	"/models",
	"/backend-api/codex/models",
	"/antigravity/models",
	"/antigravity/v1/models",
}

func TestAPIKeyAuthZeroBalanceModelsDiscovery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	user := &service.User{
		ID:          11,
		Role:        service.RoleUser,
		Status:      service.StatusActive,
		Balance:     0,
		Concurrency: 3,
	}
	apiKey := &service.APIKey{
		ID:     110,
		UserID: user.ID,
		Key:    "zero-balance-key",
		Status: service.StatusActive,
		User:   user,
	}
	repo := &stubApiKeyRepo{
		getByKey: func(ctx context.Context, key string) (*service.APIKey, error) {
			if key != apiKey.Key {
				return nil, service.ErrAPIKeyNotFound
			}
			clone := *apiKey
			userClone := *user
			clone.User = &userClone
			return &clone, nil
		},
	}

	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiKeyService := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)

	router := gin.New()
	router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg)))
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	for _, path := range modelsDiscoveryPaths {
		router.GET(path, ok)
	}
	router.GET("/v1/chat/completions", ok)

	// 所有发现端点：零余额也放行。
	for _, path := range modelsDiscoveryPaths {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("x-api-key", apiKey.Key)
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "GET %s", path)
	}

	// 非发现端点：零余额仍然 403。
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	requireAPIKeyAuthError(t, w, "INSUFFICIENT_BALANCE", "Insufficient account balance")
}

func TestGoogleAPIKeyAuthZeroBalanceModelsDiscovery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	apiKeyService := newTestAPIKeyService(fakeAPIKeyRepo{
		getByKey: func(ctx context.Context, key string) (*service.APIKey, error) {
			return &service.APIKey{
				ID:     1,
				Key:    key,
				Status: service.StatusActive,
				User: &service.User{
					ID:      123,
					Status:  service.StatusActive,
					Balance: 0,
				},
			}, nil
		},
	})
	r.Use(APIKeyAuthWithSubscriptionGoogle(apiKeyService, nil, &config.Config{}))

	ok := func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) }
	for _, path := range []string{"/v1beta/models", "/antigravity/v1beta/models"} {
		r.GET(path, ok)
	}
	r.GET("/v1beta/test", ok)

	// Google 平台的两个发现端点：零余额也放行。
	for _, path := range []string{"/v1beta/models", "/antigravity/v1beta/models"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer ok")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "GET %s", path)
	}

	// 非发现端点：零余额仍然 403。
	req := httptest.NewRequest(http.MethodGet, "/v1beta/test", nil)
	req.Header.Set("Authorization", "Bearer ok")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	var resp googleErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "Insufficient account balance", resp.Error.Message)
}
