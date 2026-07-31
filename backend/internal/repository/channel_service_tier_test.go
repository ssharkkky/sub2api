//go:build unit

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelServiceTierConfigJSON(t *testing.T) {
	defaults := service.DefaultChannelServiceTierConfig()
	data, err := marshalServiceTierConfig(defaults)
	require.NoError(t, err)

	decoded, configError := unmarshalServiceTierConfig(data)
	require.Empty(t, configError)
	require.Equal(t, defaults, decoded)

	decoded, configError = unmarshalServiceTierConfig(nil)
	require.Empty(t, configError)
	require.Equal(t, defaults, decoded)

	_, configError = unmarshalServiceTierConfig([]byte(`{"standard":`))
	require.Contains(t, configError, "decode service_tier_config")

	_, configError = unmarshalServiceTierConfig([]byte(`{"standard":{"enabled":false,"multiplier":1},"priority":{"enabled":false,"multiplier":2},"flex":{"enabled":false,"multiplier":0.5}}`))
	require.Contains(t, configError, "at least one")
}
