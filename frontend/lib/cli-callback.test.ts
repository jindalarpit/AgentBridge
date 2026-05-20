import { describe, it, expect } from "vitest";
import { validateCliCallback, buildCliRedirectUrl } from "./cli-callback";

describe("validateCliCallback", () => {
  it("accepts valid http://localhost: URL", () => {
    expect(validateCliCallback("http://localhost:12345/callback")).toBe(true);
  });

  it("accepts valid http://127.0.0.1: URL", () => {
    expect(validateCliCallback("http://127.0.0.1:54321/callback")).toBe(true);
  });

  it("rejects null", () => {
    expect(validateCliCallback(null)).toBe(false);
  });

  it("rejects undefined", () => {
    expect(validateCliCallback(undefined)).toBe(false);
  });

  it("rejects empty string", () => {
    expect(validateCliCallback("")).toBe(false);
  });

  it("rejects https://localhost URL", () => {
    expect(validateCliCallback("https://localhost:8080/callback")).toBe(false);
  });

  it("rejects non-localhost URL", () => {
    expect(validateCliCallback("http://evil.com:8080/callback")).toBe(false);
  });

  it("rejects http://localhost without port (no colon after localhost)", () => {
    expect(validateCliCallback("http://localhost/callback")).toBe(false);
  });

  it("rejects URL exceeding 2048 characters", () => {
    const longPath = "a".repeat(2049 - "http://localhost:8080/".length);
    const longUrl = `http://localhost:8080/${longPath}`;
    expect(longUrl.length).toBeGreaterThan(2048);
    expect(validateCliCallback(longUrl)).toBe(false);
  });

  it("accepts URL at exactly 2048 characters", () => {
    const prefix = "http://localhost:8080/";
    const padding = "a".repeat(2048 - prefix.length);
    const url = prefix + padding;
    expect(url.length).toBe(2048);
    expect(validateCliCallback(url)).toBe(true);
  });

  it("rejects syntactically invalid URL", () => {
    expect(validateCliCallback("http://localhost:not-a-valid-url")).toBe(false);
  });

  it("rejects URL with http://127.0.0.2:", () => {
    expect(validateCliCallback("http://127.0.0.2:8080/callback")).toBe(false);
  });
});

describe("buildCliRedirectUrl", () => {
  it("builds URL with token and state", () => {
    const result = buildCliRedirectUrl(
      "http://localhost:12345/callback",
      "jwt-token-123",
      "state-abc"
    );
    const url = new URL(result);
    expect(url.origin).toBe("http://localhost:12345");
    expect(url.pathname).toBe("/callback");
    expect(url.searchParams.get("token")).toBe("jwt-token-123");
    expect(url.searchParams.get("state")).toBe("state-abc");
  });

  it("builds URL with only token when state is null", () => {
    const result = buildCliRedirectUrl(
      "http://localhost:12345/callback",
      "jwt-token-123",
      null
    );
    const url = new URL(result);
    expect(url.searchParams.get("token")).toBe("jwt-token-123");
    expect(url.searchParams.has("state")).toBe(false);
  });

  it("builds URL with only token when state is empty string", () => {
    const result = buildCliRedirectUrl(
      "http://localhost:12345/callback",
      "jwt-token-123",
      ""
    );
    const url = new URL(result);
    expect(url.searchParams.get("token")).toBe("jwt-token-123");
    expect(url.searchParams.has("state")).toBe(false);
  });

  it("builds URL with only token when state is undefined", () => {
    const result = buildCliRedirectUrl(
      "http://localhost:12345/callback",
      "jwt-token-123",
      undefined
    );
    const url = new URL(result);
    expect(url.searchParams.get("token")).toBe("jwt-token-123");
    expect(url.searchParams.has("state")).toBe(false);
  });

  it("properly URL-encodes special characters in token", () => {
    const result = buildCliRedirectUrl(
      "http://localhost:12345/callback",
      "token+with=special&chars",
      "state"
    );
    const url = new URL(result);
    expect(url.searchParams.get("token")).toBe("token+with=special&chars");
  });
});
