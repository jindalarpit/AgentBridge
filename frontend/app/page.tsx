"use client";

import { useState, FormEvent, useMemo } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { login, register, setToken, ApiError } from "@/lib/api";
import { useAuthStore } from "@/lib/store";
import { validateCliCallback, buildCliRedirectUrl } from "@/lib/cli-callback";

type Mode = "login" | "register";

export default function AuthPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const authLogin = useAuthStore((s) => s.login);

  // Read CLI callback params from URL
  const cliCallback = searchParams.get("cli_callback");
  const cliState = searchParams.get("cli_state");

  // Validate the callback URL once
  const validCliCallback = useMemo(
    () => (validateCliCallback(cliCallback) ? cliCallback : null),
    [cliCallback]
  );

  const [cliSuccess, setCliSuccess] = useState(false);

  const [mode, setMode] = useState<Mode>("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  function resetErrors() {
    setError(null);
    setFieldErrors({});
  }

  function toggleMode() {
    setMode(mode === "login" ? "register" : "login");
    resetErrors();
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    resetErrors();
    setLoading(true);

    try {
      const res =
        mode === "login"
          ? await login({ email, password })
          : await register({ email, password, display_name: displayName || undefined });

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

      // If valid CLI callback is present, redirect to callback URL
      if (validCliCallback) {
        const redirectUrl = buildCliRedirectUrl(
          validCliCallback,
          res.token,
          cliState
        );
        setCliSuccess(true);
        window.location.href = redirectUrl;
        return;
      }

      router.push("/chat");
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.fields && Object.keys(err.fields).length > 0) {
          setFieldErrors(err.fields);
        }
        setError(err.message);
      } else {
        setError("An unexpected error occurred. Please try again.");
      }
    } finally {
      setLoading(false);
    }
  }

  // Show success message for CLI callback flow
  if (cliSuccess) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-gray-50 px-4">
        <div className="w-full max-w-sm text-center">
          <h1 className="text-2xl font-bold text-gray-900">AgentBridge</h1>
          <div className="mt-6 rounded-md bg-green-50 p-4">
            <p className="text-sm font-medium text-green-800">
              Authentication successful — you can close this tab
            </p>
          </div>
        </div>
      </main>
    );
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <h1 className="text-2xl font-bold text-gray-900">AgentBridge</h1>
          <p className="mt-1 text-sm text-gray-500">
            {mode === "login"
              ? "Sign in to chat with your local AI agents"
              : "Create an account to get started"}
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          {error && !Object.keys(fieldErrors).length && (
            <div className="rounded-md bg-red-50 p-3 text-sm text-red-700">
              {error}
            </div>
          )}

          {mode === "register" && (
            <div>
              <label
                htmlFor="displayName"
                className="block text-sm font-medium text-gray-700"
              >
                Display Name
              </label>
              <input
                id="displayName"
                type="text"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
                placeholder="Your name (optional)"
              />
              {fieldErrors.display_name && (
                <p className="mt-1 text-xs text-red-600">
                  {fieldErrors.display_name}
                </p>
              )}
            </div>
          )}

          <div>
            <label
              htmlFor="email"
              className="block text-sm font-medium text-gray-700"
            >
              Email
            </label>
            <input
              id="email"
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              placeholder="you@example.com"
            />
            {fieldErrors.email && (
              <p className="mt-1 text-xs text-red-600">{fieldErrors.email}</p>
            )}
          </div>

          <div>
            <label
              htmlFor="password"
              className="block text-sm font-medium text-gray-700"
            >
              Password
            </label>
            <input
              id="password"
              type="password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
              placeholder="••••••••"
            />
            {fieldErrors.password && (
              <p className="mt-1 text-xs text-red-600">
                {fieldErrors.password}
              </p>
            )}
          </div>

          <button
            type="submit"
            disabled={loading}
            className="w-full rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loading
              ? mode === "login"
                ? "Signing in..."
                : "Creating account..."
              : mode === "login"
                ? "Sign In"
                : "Create Account"}
          </button>
        </form>

        <p className="mt-4 text-center text-sm text-gray-500">
          {mode === "login" ? "Don't have an account?" : "Already have an account?"}{" "}
          <button
            type="button"
            onClick={toggleMode}
            className="font-medium text-blue-600 hover:text-blue-500"
          >
            {mode === "login" ? "Sign up" : "Sign in"}
          </button>
        </p>
      </div>
    </main>
  );
}
