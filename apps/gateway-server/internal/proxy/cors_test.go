package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/registry"
)

func TestMatchOrigin_ExactMatch(t *testing.T) {
	cors := &registry.CORSMeta{Origins: []string{"https://app.example.com", "https://admin.example.com"}}

	assert.Equal(t, "https://app.example.com", MatchOrigin(cors, "https://app.example.com"))
}

func TestMatchOrigin_NoMatch(t *testing.T) {
	cors := &registry.CORSMeta{Origins: []string{"https://app.example.com"}}

	assert.Equal(t, "", MatchOrigin(cors, "https://evil.com"))
}

func TestMatchOrigin_Wildcard(t *testing.T) {
	cors := &registry.CORSMeta{Origins: []string{"*"}}

	assert.Equal(t, "*", MatchOrigin(cors, "https://anything.com"))
}

func TestMatchOrigin_EmptyOriginHeader(t *testing.T) {
	cors := &registry.CORSMeta{Origins: []string{"https://app.example.com"}}

	assert.Equal(t, "", MatchOrigin(cors, ""))
}

func TestBuildPreflightHeaders_FullConfig(t *testing.T) {
	cors := &registry.CORSMeta{
		Origins:     []string{"https://app.example.com"},
		Methods:     []string{"POST", "PUT"},
		Headers:     []string{"Content-Type", "Authorization"},
		Credentials: true,
		MaxAge:      3600,
	}

	h := BuildPreflightHeaders(cors, "https://app.example.com")

	assert.Equal(t, "https://app.example.com", h["Access-Control-Allow-Origin"])
	assert.Equal(t, "POST, PUT", h["Access-Control-Allow-Methods"])
	assert.Equal(t, "Content-Type, Authorization", h["Access-Control-Allow-Headers"])
	assert.Equal(t, "true", h["Access-Control-Allow-Credentials"])
	assert.Equal(t, "3600", h["Access-Control-Max-Age"])
	assert.Equal(t, "Origin", h["Vary"])
}

func TestBuildPreflightHeaders_WildcardNoVary(t *testing.T) {
	cors := &registry.CORSMeta{Origins: []string{"*"}}

	h := BuildPreflightHeaders(cors, "*")

	assert.Equal(t, "*", h["Access-Control-Allow-Origin"])
	_, hasVary := h["Vary"]
	assert.False(t, hasVary, "Vary should not be set for wildcard origin")
}

func TestBuildPreflightHeaders_MinimalConfig(t *testing.T) {
	cors := &registry.CORSMeta{Origins: []string{"https://app.example.com"}}

	h := BuildPreflightHeaders(cors, "https://app.example.com")

	assert.Equal(t, "https://app.example.com", h["Access-Control-Allow-Origin"])
	_, hasMethods := h["Access-Control-Allow-Methods"]
	assert.False(t, hasMethods)
	_, hasHeaders := h["Access-Control-Allow-Headers"]
	assert.False(t, hasHeaders)
	_, hasCreds := h["Access-Control-Allow-Credentials"]
	assert.False(t, hasCreds)
}

func TestBuildResponseCORSHeaders_IncludesOnlyResponseFields(t *testing.T) {
	cors := &registry.CORSMeta{
		Origins:     []string{"https://app.example.com"},
		Methods:     []string{"POST"},
		Headers:     []string{"Authorization"},
		Credentials: true,
		MaxAge:      3600,
	}

	h := BuildResponseCORSHeaders(cors, "https://app.example.com")

	assert.Equal(t, "https://app.example.com", h["Access-Control-Allow-Origin"])
	assert.Equal(t, "true", h["Access-Control-Allow-Credentials"])
	assert.Equal(t, "Origin", h["Vary"])

	_, hasMethods := h["Access-Control-Allow-Methods"]
	assert.False(t, hasMethods, "response should not include Allow-Methods")
	_, hasHeaders := h["Access-Control-Allow-Headers"]
	assert.False(t, hasHeaders, "response should not include Allow-Headers")
	_, hasMaxAge := h["Access-Control-Max-Age"]
	assert.False(t, hasMaxAge, "response should not include Max-Age")
}
