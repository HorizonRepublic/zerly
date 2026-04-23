package ratelimit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashKey_FixedLengthAndDeterministic(t *testing.T) {
	a1 := hashKey("GET:/users/:id")
	a2 := hashKey("GET:/users/:id")
	b := hashKey("POST:/users")

	assert.Len(t, a1, 13)
	assert.Equal(t, a1, a2)
	assert.NotEqual(t, a1, b)
	assert.Regexp(t, `^[a-z2-7]{13}$`, a1)
}
