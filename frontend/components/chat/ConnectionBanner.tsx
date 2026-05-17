"use client";

import { useConnectionStore } from "@/lib/store";

/**
 * ConnectionBanner displays a "Reconnecting..." banner when the WebSocket
 * connection drops. It appears at the top of the chat area and auto-hides
 * when the connection is restored.
 *
 * Requirements: 10.3, 10.5
 */
export function ConnectionBanner() {
  const status = useConnectionStore((state) => state.status);

  if (status === "connected") {
    return null;
  }

  return (
    <div
      role="alert"
      aria-live="polite"
      className="flex items-center justify-center gap-2 bg-amber-50 dark:bg-amber-900/30 border-b border-amber-200 dark:border-amber-700 px-4 py-2 text-sm text-amber-700 dark:text-amber-300"
    >
      <ReconnectingSpinner />
      <span>
        {status === "reconnecting"
          ? "Reconnecting..."
          : "Disconnected from server. Attempting to reconnect..."}
      </span>
    </div>
  );
}

function ReconnectingSpinner() {
  return (
    <svg
      className="h-4 w-4 animate-spin flex-shrink-0"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle
        className="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        strokeWidth="4"
      />
      <path
        className="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
      />
    </svg>
  );
}
