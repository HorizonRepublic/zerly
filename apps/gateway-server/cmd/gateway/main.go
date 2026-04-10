// Package main is the entry point for zerly-gateway-server.
//
// zerly-gateway-server is an HTTP edge server that watches the
// handler_registry NATS KV bucket for routing metadata and proxies HTTP
// requests to Nest microservice handlers via Core NATS request/reply.
//
// The binary produced by this package is a scaffolding placeholder — real
// bootstrap wiring lands in milestone M21. See the design specification at
// docs/superpowers/specs/2026-04-10-zerly-gateway-design.md for architecture
// details.
package main

import "fmt"

func main() {
	fmt.Println("zerly-gateway-server — scaffolding placeholder (M11)")
}
