package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformDefaultModelIDs_IncludesCatalogOnlyModels(t *testing.T) {
	ids := PlatformDefaultModelIDs(PlatformAntigravity)
	require.Contains(t, ids, "gemini-3.7-flash")
	require.Contains(t, ids, "gemini-3.7-flash-high")

	gemini := PlatformDefaultModelIDs(PlatformGemini)
	require.Contains(t, gemini, "gemini-3.7-flash")
	require.Contains(t, gemini, "gemini-2.5-flash")
}

