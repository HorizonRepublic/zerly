package trustedproxy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/trustedproxy"
)

func TestParseCIDRList_EmptyStringReturnsEmptySlice(t *testing.T) {
	out, err := trustedproxy.ParseCIDRList("")
	require.NoError(t, err)
	assert.Empty(t, out, "empty input means trust nothing")
}

func TestParseCIDRList_PrivateSentinelExpandsToSevenRanges(t *testing.T) {
	out, err := trustedproxy.ParseCIDRList("private")
	require.NoError(t, err)
	require.Len(t, out, 7, "private sentinel expands to exactly 7 CIDR blocks")

	// Collect textual forms so the test is order-independent and
	// reads like a security audit.
	got := make(map[string]bool, len(out))
	for _, n := range out {
		got[n.String()] = true
	}
	want := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"::1/128",
		"fd00::/8",
	}
	for _, cidr := range want {
		assert.True(t, got[cidr], "private sentinel must include %s", cidr)
	}
}

func TestParseCIDRList_LiteralSingleCIDR(t *testing.T) {
	out, err := trustedproxy.ParseCIDRList("10.0.0.0/8")
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "10.0.0.0/8", out[0].String())
}

func TestParseCIDRList_LiteralCommaSeparatedWithWhitespace(t *testing.T) {
	out, err := trustedproxy.ParseCIDRList("10.0.0.0/8,  172.16.0.0/12 , 192.168.0.0/16")
	require.NoError(t, err)
	require.Len(t, out, 3, "literal list must tolerate whitespace around commas")

	got := make([]string, 0, 3)
	for _, n := range out {
		got = append(got, n.String())
	}
	assert.ElementsMatch(t, []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}, got)
}

func TestParseCIDRList_InvalidCIDRReturnsError(t *testing.T) {
	_, err := trustedproxy.ParseCIDRList("not-a-cidr")
	require.Error(t, err, "invalid input must fail parse so Load() aborts startup")
	assert.Contains(t, err.Error(), "not-a-cidr",
		"error must name the offending entry so operators can fix it")
}

func TestParseCIDRList_BadMaskReturnsError(t *testing.T) {
	_, err := trustedproxy.ParseCIDRList("10.0.0.0/99")
	require.Error(t, err, "mask out of range must fail parse")
}

func TestParseCIDRList_OneValidOneInvalidReturnsError(t *testing.T) {
	_, err := trustedproxy.ParseCIDRList("10.0.0.0/8,garbage")
	require.Error(t, err,
		"any single invalid entry must fail the whole parse (fail-closed)")
}
