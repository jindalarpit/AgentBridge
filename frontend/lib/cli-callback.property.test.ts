import { describe, it, expect } from "vitest";
import * as fc from "fast-check";
import { validateCliCallback, buildCliRedirectUrl } from "./cli-callback";

/**
 * Property 12: Callback Redirect URL Construction
 *
 * For any valid localhost callback URL and state string, after successful
 * authentication the frontend SHALL redirect to
 * `{cli_callback}?token={jwt}&state={cli_state}` with properly URL-encoded parameters.
 *
 * **Validates: Requirements 10.2**
 */
describe("Property 12: Callback Redirect URL Construction", () => {
  // Generator for valid localhost ports (1024-65535)
  const validPort = fc.integer({ min: 1024, max: 65535 });

  // Generator for valid URL path segments (alphanumeric + hyphens + slashes)
  const validPath = fc.stringOf(
    fc.constantFrom(
      ..."abcdefghijklmnopqrstuvwxyz0123456789-_/".split("")
    ),
    { minLength: 0, maxLength: 50 }
  ).map((s) => s.startsWith("/") ? s : `/${s}`);

  // Generator for valid localhost callback URLs
  const validLocalhostUrl = fc.oneof(
    fc.tuple(validPort, validPath).map(
      ([port, path]) => `http://localhost:${port}${path}`
    ),
    fc.tuple(validPort, validPath).map(
      ([port, path]) => `http://127.0.0.1:${port}${path}`
    )
  );

  // Generator for JWT-like token strings (non-empty, no control chars)
  const tokenArb = fc.stringOf(
    fc.constantFrom(
      ..."abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.~+/=".split("")
    ),
    { minLength: 1, maxLength: 200 }
  );

  // Generator for state strings (hex-like, as used by the CLI)
  const stateArb = fc.hexaString({ minLength: 1, maxLength: 64 });

  it("redirect URL contains the token parameter with correct value", () => {
    fc.assert(
      fc.property(validLocalhostUrl, tokenArb, stateArb, (callbackUrl, token, state) => {
        const result = buildCliRedirectUrl(callbackUrl, token, state);
        const parsed = new URL(result);
        expect(parsed.searchParams.get("token")).toBe(token);
      }),
      { numRuns: 200 }
    );
  });

  it("redirect URL contains the state parameter with correct value when state is provided", () => {
    fc.assert(
      fc.property(validLocalhostUrl, tokenArb, stateArb, (callbackUrl, token, state) => {
        const result = buildCliRedirectUrl(callbackUrl, token, state);
        const parsed = new URL(result);
        expect(parsed.searchParams.get("state")).toBe(state);
      }),
      { numRuns: 200 }
    );
  });

  it("redirect URL omits state parameter when state is null or empty", () => {
    fc.assert(
      fc.property(
        validLocalhostUrl,
        tokenArb,
        fc.constantFrom(null, undefined, ""),
        (callbackUrl, token, state) => {
          const result = buildCliRedirectUrl(callbackUrl, token, state);
          const parsed = new URL(result);
          expect(parsed.searchParams.has("state")).toBe(false);
        }
      ),
      { numRuns: 200 }
    );
  });

  it("redirect URL preserves the callback URL origin and pathname", () => {
    fc.assert(
      fc.property(validLocalhostUrl, tokenArb, stateArb, (callbackUrl, token, state) => {
        const result = buildCliRedirectUrl(callbackUrl, token, state);
        const original = new URL(callbackUrl);
        const parsed = new URL(result);
        expect(parsed.origin).toBe(original.origin);
        expect(parsed.pathname).toBe(original.pathname);
      }),
      { numRuns: 200 }
    );
  });

  it("redirect URL has format {cli_callback}?token={jwt}&state={cli_state}", () => {
    fc.assert(
      fc.property(validLocalhostUrl, tokenArb, stateArb, (callbackUrl, token, state) => {
        const result = buildCliRedirectUrl(callbackUrl, token, state);
        // The result should start with the callback URL base
        expect(result.startsWith(callbackUrl.split("?")[0])).toBe(true);
        // Should contain both params
        const parsed = new URL(result);
        expect(parsed.searchParams.get("token")).toBe(token);
        expect(parsed.searchParams.get("state")).toBe(state);
      }),
      { numRuns: 200 }
    );
  });
});

/**
 * Property 13: Callback URL Security Validation
 *
 * For any URL that does not start with `http://localhost:` or `http://127.0.0.1:`,
 * or that is not a syntactically valid URL, or that exceeds 2048 characters,
 * the frontend SHALL ignore the `cli_callback` parameter.
 *
 * **Validates: Requirements 10.4, 10.5**
 */
describe("Property 13: Callback URL Security Validation", () => {
  // Generator for non-localhost schemes/hosts that should be rejected
  const nonLocalhostHost = fc.oneof(
    fc.webUrl().filter(
      (url) =>
        !url.startsWith("http://localhost:") &&
        !url.startsWith("http://127.0.0.1:")
    ),
    // Explicit non-localhost patterns
    fc.constantFrom(
      "https://localhost:8080/callback",
      "http://evil.com:8080/callback",
      "http://192.168.1.1:8080/callback",
      "http://127.0.0.2:8080/callback",
      "ftp://localhost:8080/callback",
      "http://localhost.evil.com:8080/callback",
      "http://0.0.0.0:8080/callback",
      "http://[::1]:8080/callback"
    ),
    // Random domain URLs
    fc.tuple(
      fc.constantFrom("http://", "https://", "ftp://", "ws://"),
      fc.domain(),
      fc.integer({ min: 1, max: 65535 }),
      fc.webPath()
    ).map(([scheme, domain, port, path]) => `${scheme}${domain}:${port}${path}`)
      .filter(
        (url) =>
          !url.startsWith("http://localhost:") &&
          !url.startsWith("http://127.0.0.1:")
      )
  );

  // Generator for URLs exceeding 2048 characters (but otherwise valid localhost)
  const tooLongUrl = fc.integer({ min: 1024, max: 65535 }).map((port) => {
    const prefix = `http://localhost:${port}/`;
    const padding = "a".repeat(2049 - prefix.length);
    return prefix + padding;
  });

  // Generator for syntactically invalid URLs that start with localhost prefix
  const invalidSyntaxUrl = fc.constantFrom(
    "http://localhost:not-a-port/callback",
    "http://localhost:-1/callback",
    "http://localhost:99999999/callback",
    "http://127.0.0.1:abc/callback"
  );

  it("rejects all non-localhost URLs", () => {
    fc.assert(
      fc.property(nonLocalhostHost, (url) => {
        expect(validateCliCallback(url)).toBe(false);
      }),
      { numRuns: 200 }
    );
  });

  it("rejects all URLs exceeding 2048 characters", () => {
    fc.assert(
      fc.property(tooLongUrl, (url) => {
        expect(url.length).toBeGreaterThan(2048);
        expect(validateCliCallback(url)).toBe(false);
      }),
      { numRuns: 100 }
    );
  });

  it("rejects syntactically invalid URLs with localhost prefix", () => {
    fc.assert(
      fc.property(invalidSyntaxUrl, (url) => {
        expect(validateCliCallback(url)).toBe(false);
      }),
      { numRuns: 50 }
    );
  });

  it("accepts valid localhost URLs within length limit", () => {
    const validLocalhostUrl = fc.oneof(
      fc.integer({ min: 1, max: 65535 }).map(
        (port) => `http://localhost:${port}/callback`
      ),
      fc.integer({ min: 1, max: 65535 }).map(
        (port) => `http://127.0.0.1:${port}/callback`
      )
    );

    fc.assert(
      fc.property(validLocalhostUrl, (url) => {
        expect(validateCliCallback(url)).toBe(true);
      }),
      { numRuns: 200 }
    );
  });

  it("rejects null and undefined values", () => {
    fc.assert(
      fc.property(
        fc.constantFrom(null, undefined, ""),
        (value) => {
          expect(validateCliCallback(value as string | null | undefined)).toBe(false);
        }
      ),
      { numRuns: 10 }
    );
  });

  it("rejects random strings that are not valid URLs", () => {
    const randomNonUrl = fc.string({ minLength: 1, maxLength: 100 }).filter(
      (s) =>
        !s.startsWith("http://localhost:") &&
        !s.startsWith("http://127.0.0.1:")
    );

    fc.assert(
      fc.property(randomNonUrl, (str) => {
        expect(validateCliCallback(str)).toBe(false);
      }),
      { numRuns: 200 }
    );
  });
});
