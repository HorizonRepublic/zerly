/**
 * HTTP methods supported by the Zerly gateway.
 * @remarks
 * This union is the canonical list of verbs that `@ApiGateway` accepts and
 * that `zerly-gateway-server` will dispatch. Any extension (e.g., custom
 * verbs) requires a breaking change and a synchronized release of both
 * `@zerly/gateway-sdk` and `zerly-gateway-server`.
 */
export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE' | 'HEAD' | 'OPTIONS';
