"use client";

import { useState, useCallback } from "react";
import { useConnectionStore, useAuthStore } from "@/lib/store";
import { login, setToken } from "@/lib/api";
import { getWsClient } from "@/lib/ws";

/**
 * ReAuthModal displays a modal dialog when the user's session token expires.
 * It prompts the user to re-authenticate without losing their chat session state.
 *
 * Requirements: 3.5
 */
export function ReAuthModal() {
  const tokenExpired = useConnectionStore((state) => state.tokenExpired);
  const user = useAuthStore((state) => state.user);
  const authLogin = useAuthStore((state) => state.login);
  const setTokenExpired = useConnectionStore((state) => state.setTokenExpired);

  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!user?.email || !password.trim()) return;

      setIsSubmitting(true);
      setError(null);

      try {
        const res = await login({ email: user.email, password });
        // Update auth state with new token
        authLogin(
          {
            id: res.user.id,
            email: res.user.email,
            display_name: res.user.display_name,
            created_at: res.user.created_at,
          },
          res.token
        );
        setToken(res.token);

        // Reconnect WebSocket with new token
        const wsClient = getWsClient();
        wsClient.setToken(res.token);
        wsClient.disconnect();
        wsClient.connect();

        // Clear modal state
        setTokenExpired(false);
        setPassword("");
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Authentication failed. Please try again."
        );
      } finally {
        setIsSubmitting(false);
      }
    },
    [user, password, authLogin, setTokenExpired]
  );

  if (!tokenExpired) {
    return null;
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      role="dialog"
      aria-modal="true"
      aria-labelledby="reauth-title"
    >
      <div className="w-full max-w-sm rounded-lg bg-white dark:bg-gray-800 p-6 shadow-xl">
        <h2
          id="reauth-title"
          className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-2"
        >
          Session Expired
        </h2>
        <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
          Your session has expired. Please sign in again to continue. Your chat
          history is preserved.
        </p>

        <form onSubmit={handleSubmit}>
          {/* Email (read-only) */}
          <div className="mb-3">
            <label
              htmlFor="reauth-email"
              className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
            >
              Email
            </label>
            <input
              id="reauth-email"
              type="email"
              value={user?.email ?? ""}
              readOnly
              className="w-full rounded-md border border-gray-300 dark:border-gray-600 bg-gray-100 dark:bg-gray-700 px-3 py-2 text-sm text-gray-700 dark:text-gray-300"
            />
          </div>

          {/* Password */}
          <div className="mb-4">
            <label
              htmlFor="reauth-password"
              className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
            >
              Password
            </label>
            <input
              id="reauth-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Enter your password"
              autoFocus
              className="w-full rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 px-3 py-2 text-sm text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>

          {/* Error */}
          {error && (
            <div className="mb-3 text-sm text-red-600 dark:text-red-400" role="alert">
              {error}
            </div>
          )}

          {/* Submit */}
          <button
            type="submit"
            disabled={isSubmitting || !password.trim()}
            className="w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:cursor-not-allowed disabled:opacity-50 transition-colors"
          >
            {isSubmitting ? "Signing in..." : "Sign In"}
          </button>
        </form>
      </div>
    </div>
  );
}
