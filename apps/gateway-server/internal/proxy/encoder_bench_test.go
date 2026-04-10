package proxy

import (
	"testing"

	"github.com/HorizonRepublic/zerly/apps/gateway-server/internal/routing"
)

// BenchmarkEncoder_Encode_SmallJSON measures the hot path for a
// typical POST envelope: three headers, one query value, a small
// JSON body, and the usual metadata. The input is constructed once
// and reused — b.N iterations share the same EncodeInput pointer so
// the benchmark reflects steady-state cost, not construction cost.
func BenchmarkEncoder_Encode_SmallJSON(b *testing.B) {
	encoder := NewDefaultEncoder()
	input := &EncodeInput{
		Method: "POST",
		Path:   "/users",
		Body:   []byte(`{"email":"a@b.c","name":"Alice"}`),
		Query: map[string]QueryValue{
			"include": NewQueryValueString("profile"),
		},
		Headers: map[string]string{
			"authorization": "Bearer xxxx",
			"x-tenant-id":   "42",
			"content-type":  "application/json",
		},
		Route: routing.Route{
			Subject:      "users-svc__microservice.cmd.users.create",
			Method:       "POST",
			PathTemplate: "/users",
		},
		PathParams: map[string]string{},
		RequestID:  "01HXY0000000000000000000",
		RemoteAddr: "127.0.0.1",
		ReceivedAt: 1000,
		TimeoutMs:  30000,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := encoder.Encode(input); err != nil {
			b.Fatal(err)
		}
	}
}
