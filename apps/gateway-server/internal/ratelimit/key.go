package ratelimit

import (
	"fmt"
	"strings"
)

// ResolveKey walks the keyBy chain and returns the first resolved value.
// Falls back to clientIP if nothing resolves.
//
// headerFn and cookieFn are injected to decouple from any HTTP framework.
func ResolveKey(
	keyBy []string,
	clientIP string,
	headerFn func(name string) string,
	cookieFn func(name string) string,
	claims map[string]any,
) string {
	for _, key := range keyBy {
		switch {
		case key == "ip":
			return clientIP

		case strings.HasPrefix(key, "header:"):
			if v := headerFn(key[7:]); v != "" {
				return v
			}

		case strings.HasPrefix(key, "cookie:"):
			if v := cookieFn(key[7:]); v != "" {
				return v
			}

		case strings.HasPrefix(key, "user:"):
			if claims != nil {
				field := key[5:]
				if v, ok := claims[field]; ok {
					return fmt.Sprint(v)
				}
			}
		}
	}

	return clientIP
}
