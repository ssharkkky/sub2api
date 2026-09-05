package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPR166AuditCompactAccountPreservesKiroRuntime(t *testing.T) {
	reset := time.Now().Add(time.Hour)
	account := &Account{ID: 1, Platform: "kiro", KiroRuntimeState: "cooldown", KiroRuntimeReason: "upstream cooldown", KiroRuntimeResetAt: &reset, KiroQuotaState: "exhausted", KiroQuotaReason: "quota exhausted", KiroQuotaResetAt: &reset}
	body, err := json.Marshal(AccountListItemFromAccount(account))
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, "cooldown", payload["kiro_runtime_state"])
	require.Equal(t, "exhausted", payload["kiro_quota_state"])
}
