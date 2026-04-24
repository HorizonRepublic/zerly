import type { ICookieOptions } from '../types/cookie-options.interface';

/**
 * Fast-path test: ASCII letters, digits, dash, underscore, dot,
 * and tilde — the RFC 3986 unreserved set — need no percent-
 * encoding in a `Set-Cookie` value or name. Regex is compiled
 * once at module load.
 */
const TOKEN_SAFE = /^[A-Za-z0-9._~-]*$/;

/**
 * Maps the decorator option's lowercase sameSite values to the
 * canonical capitalized form expected in the serialized header.
 */
const SAME_SITE_LABELS = {
  strict: 'Strict',
  lax: 'Lax',
  none: 'None',
} as const;

/**
 * Serializes a single `Set-Cookie` header value per RFC 6265
 * §4.1.1. Pure function, no external dependencies.
 * @param name - Cookie name. Percent-encoded if it contains
 *               characters outside the RFC 3986 unreserved set.
 * @param value - Cookie value. Same encoding rule as the name.
 * @param options - Optional RFC 6265 attributes. Missing fields
 *                  omit the corresponding attribute.
 * @param defaults - Module-level cookie defaults from
 *                   `GatewayModule.forRoot({ defaults: { cookies } })`.
 *                   Per-cookie `options` fields take precedence over
 *                   `defaults` on a key-by-key basis.
 * @returns The serialized header value, ready to be stored in a
 *          reply envelope under `set-cookie`.
 * @example
 * ```ts
 * serializeCookie('sid', 'abc', { httpOnly: true, secure: true, maxAge: 3600 });
 * // → 'sid=abc; Max-Age=3600; HttpOnly; Secure'
 * ```
 */
export const serializeCookie = (
  name: string,
  value: string,
  options: ICookieOptions = {},
  defaults: Partial<ICookieOptions> = {},
): string => {
  const merged: ICookieOptions = { ...defaults, ...options };

  const encodedName = TOKEN_SAFE.test(name) ? name : encodeURIComponent(name);
  const encodedValue = TOKEN_SAFE.test(value) ? value : encodeURIComponent(value);

  let out = `${encodedName}=${encodedValue}`;

  if (merged.domain !== undefined) {
    out += `; Domain=${merged.domain}`;
  }

  if (merged.path !== undefined) {
    out += `; Path=${merged.path}`;
  }

  if (merged.expires !== undefined) {
    out += `; Expires=${merged.expires.toUTCString()}`;
  }

  if (merged.maxAge !== undefined) {
    out += `; Max-Age=${Math.floor(merged.maxAge)}`;
  }

  if (merged.httpOnly === true) {
    out += `; HttpOnly`;
  }

  if (merged.secure === true) {
    out += `; Secure`;
  }

  if (merged.sameSite !== undefined) {
    out += `; SameSite=${SAME_SITE_LABELS[merged.sameSite]}`;
  }

  if (merged.partitioned === true) {
    out += `; Partitioned`;
  }

  return out;
};
