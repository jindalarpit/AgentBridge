"use client";

import { type ChatMessage as ChatMessageType } from "@/lib/store";

// ─── Props ───────────────────────────────────────────────────────────────────

export interface ChatMessageProps {
  message: ChatMessageType;
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function formatTimestamp(iso: string): string {
  const date = new Date(iso);
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// ─── Component ───────────────────────────────────────────────────────────────

/**
 * ChatMessage renders a single chat message with role-based styling.
 *
 * - User messages: right-aligned, blue background
 * - Assistant messages: left-aligned, gray background
 * - Error messages: red border with error icon
 *
 * Requirements: 6.3, 6.4, 6.6, 6.7
 */
export function ChatMessage({ message }: ChatMessageProps) {
  const isUser = message.role === "user";
  const isError = message.status === "error";

  // Error state rendering
  if (isError) {
    return (
      <div className="flex justify-start w-full mb-3">
        <div className="max-w-[80%] rounded-lg px-4 py-3 border-2 border-red-400 bg-red-50 dark:bg-red-950/30 dark:border-red-600">
          <div className="flex items-start gap-2">
            <ErrorIcon />
            <div className="flex-1 min-w-0">
              <p className="text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap break-words">
                {message.failure_reason ?? message.content ?? "An error occurred"}
              </p>
              <span className="text-xs text-red-500 dark:text-red-400 mt-1 block">
                {formatTimestamp(message.created_at)}
              </span>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // Normal message rendering (user or assistant)
  return (
    <div
      className={`flex w-full mb-3 ${isUser ? "justify-end" : "justify-start"}`}
    >
      <div
        className={`max-w-[80%] rounded-lg px-4 py-3 ${
          isUser
            ? "bg-blue-600 text-white"
            : "bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-gray-100"
        }`}
      >
        <p className="text-sm whitespace-pre-wrap break-words">
          {message.content}
        </p>
        <span
          className={`text-xs mt-1 block ${
            isUser
              ? "text-blue-200"
              : "text-gray-500 dark:text-gray-400"
          }`}
        >
          {formatTimestamp(message.created_at)}
        </span>
      </div>
    </div>
  );
}

// ─── Icons ───────────────────────────────────────────────────────────────────

function ErrorIcon() {
  return (
    <svg
      className="w-5 h-5 text-red-500 dark:text-red-400 flex-shrink-0 mt-0.5"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      strokeWidth={2}
      aria-hidden="true"
    >
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M12 9v2m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
      />
    </svg>
  );
}
