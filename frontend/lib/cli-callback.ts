/**
 * Validates a CLI callback URL for the daemon browser login flow.
 *
 * A valid callback URL must:
 * - Start with "http://localhost:" or "http://127.0.0.1:"
 * - Be a syntactically valid URL
 * - Be ≤ 2048 characters in length
 *
 * @param url - The callback URL to validate
 * @returns true if the URL is valid for CLI callback use, false otherwise
 */
export function validateCliCallback(url: string | null | undefined): boolean {
  if (!url) return false;
  if (url.length > 2048) return false;

  // Must start with localhost or 127.0.0.1
  if (
    !url.startsWith("http://localhost:") &&
    !url.startsWith("http://127.0.0.1:")
  ) {
    return false;
  }

  // Must be a syntactically valid URL
  try {
    new URL(url);
  } catch {
    return false;
  }

  return true;
}

/**
 * Builds the redirect URL for the CLI callback after successful authentication.
 *
 * @param cliCallback - The validated callback URL
 * @param token - The JWT token from authentication
 * @param cliState - The optional state parameter
 * @returns The full redirect URL with token and optional state params
 */
export function buildCliRedirectUrl(
  cliCallback: string,
  token: string,
  cliState: string | null | undefined
): string {
  const url = new URL(cliCallback);
  url.searchParams.set("token", token);
  if (cliState) {
    url.searchParams.set("state", cliState);
  }
  return url.toString();
}
