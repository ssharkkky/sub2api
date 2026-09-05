//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestPR166AuditEasyPayEmergencySwitchStopsProviderQueries(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	var queryCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		queryCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"trade_status":"WAIT_BUYER_PAY","money":"1.00","out_trade_no":"sub2_pr166","pid":"pid-1"}`))
	}))
	t.Cleanup(server.Close)
	instance, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeEasyPay).
		SetName("audit").SetConfig(encryptWebhookProviderConfig(t, map[string]string{
			"pid": "pid-1", "pkey": "pkey-1", "apiBase": server.URL,
			"notifyUrl": "https://example.com/notify", "returnUrl": "https://example.com/return",
		})).SetSupportedTypes("alipay,wxpay").SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().SetEmail("pr166@example.com").SetPasswordHash("hash").SetUsername("pr166").Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).
		SetAmount(1).SetPayAmount(1).SetFeeRate(0).SetRechargeCode("PR166").
		SetOutTradeNo("sub2_pr166").SetPaymentType(payment.TypeAlipay).SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").SetProviderKey(payment.TypeEasyPay).
		SetProviderInstanceID(strconv.FormatInt(instance.ID, 10)).Save(ctx)
	require.NoError(t, err)
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.Set(ctx, SettingEasyPayAutoReconcileEnabled, "false"))
	svc := &PaymentService{entClient: client, loadBalancer: newWebhookProviderTestLoadBalancer(client), providersLoaded: true, configService: NewPaymentConfigService(client, repo, nil)}
	_, err = svc.ReconcilePendingPaymentOrders(ctx)
	require.NoError(t, err)
	require.Zero(t, queryCalls.Load(), "disabled EasyPay reconciliation must not query the provider")
}

func TestPR166AuditCustomReloadPreservesExplicitCatalog(t *testing.T) {
	original := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(original) })
	dir := t.TempDir()
	explicit := filepath.Join(dir, "explicit.json")
	override := filepath.Join(dir, "override.json")
	require.NoError(t, os.WriteFile(explicit, []byte(`{"version":1,"models":[{"id":"explicit-model","lock_price":true,"price":{"input_per_mtok":9}}]}`), 0600))
	require.NoError(t, os.WriteFile(override, []byte(`{}`), 0600))
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{DataDir: dir, CatalogFile: explicit, OverrideFile: override}}, nil)
	body := []byte(`{"version":1,"models":[{"id":"cached-model"}],"prices":{"cached-model":{"input_cost_per_token":0.000001}}}`)
	doc, err := decodeModelData(body)
	require.NoError(t, err)
	require.NoError(t, svc.applyModelData(doc, "remote", "", body))
	svc.loadRuntimeCatalogFile()
	require.True(t, svc.catalogSourceIsExplicit())
	require.NotNil(t, modelcatalog.Current().Lookup("explicit-model"))
	require.NoError(t, os.WriteFile(override, []byte(`{"cached-model":{"input_cost_per_token":0.000002}}`), 0600))
	require.NoError(t, svc.reloadCustomPricingLayers())
	require.NotNil(t, modelcatalog.Current().Lookup("explicit-model"), "price override reload must preserve the active explicit catalog")
	require.True(t, svc.catalogSourceIsExplicit())
}

func TestPR166AuditLegacyCustomReloadPreservesCatalogLockedPrice(t *testing.T) {
	original := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(original) })
	cat, err := modelcatalog.Load([]byte(`{"version":1,"models":[{"id":"remote-model","lock_price":true,"price":{"input_per_mtok":9}}]}`))
	require.NoError(t, err)
	modelcatalog.Replace(cat)
	svc := newHotReloadPricingService(t, "", `{"remote-model":{"input_cost_per_token":0.000002}}`)
	require.InDelta(t, 9e-6, svc.pricingData["remote-model"].InputCostPerToken, 1e-12)
	require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, []byte(`{"remote-model":{"input_cost_per_token":0.000003}}`), 0600))
	require.NoError(t, svc.reloadCustomPricingLayers())
	require.InDelta(t, 9e-6, svc.pricingData["remote-model"].InputCostPerToken, 1e-12, "catalog lock_price must survive a custom price reload")
}
