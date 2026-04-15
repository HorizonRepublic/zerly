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
): string => {
  const encodedName = TOKEN_SAFE.test(name) ? name : encodeURIComponent(name);
  const encodedValue = TOKEN_SAFE.test(value) ? value : encodeURIComponent(value);

  let out = `${encodedName}=${encodedValue}`;

  if (options.domain !== undefined) {
    out += `; Domain=${options.domain}`;
  }

  if (options.path !== undefined) {
    out += `; Path=${options.path}`;
  }

  if (options.expires !== undefined) {
    out += `; Expires=${options.expires.toUTCString()}`;
  }

  if (options.maxAge !== undefined) {
    out += `; Max-Age=${Math.floor(options.maxAge)}`;
  }

  if (options.httpOnly === true) {
    out += `; HttpOnly`;
  }

  if (options.secure === true) {
    out += `; Secure`;
  }

  if (options.sameSite !== undefined) {
    out += `; SameSite=${SAME_SITE_LABELS[options.sameSite]}`;
  }

  return out;
};
