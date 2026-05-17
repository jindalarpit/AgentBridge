"use client";

import { useState, useCallback, useRef, useEffect } from "react";
import { useSessionStore, useRuntimeStore, useConnectionStore, useMessageStore } from "@/lib/store";
import { getWsClient } from "@/lib/ws";

// ─── Constants ───────────────────────────────────────────────────────────────

/** Minimum message length (inclusive) */
export const MIN_MESSAGE_LENGTH = 1;

/** Maximum message length (inclusive) */
export const MAX_MESSAGE_LENGTH = 32000;

// ─── Validation ──────────────────────────────────────────────────────────────

/**
 * Validates a chat message content string.
 * Returns null if valid, or an error message string if invalid.
 */
export function validateMessageContent(content: string): string | null {
  const trimmed = content.trim();
  if (trimmed.length < MIN_MESSAGE_LENGTH) {
    return "Message cannot be empty";
  }
  if (trimmed.length > MAX_MESSAGE_LENGTH) {
    return `Message exceeds maximum length of ${MAX_MESSAGE_LENGTH} characters`;
  }
  return null;
}

// ─── Props ───────────────────────────────────────────────────────────────────

export interface ChatInputProps {
  /** Optional callback after message is sent (for testing/integration) */
  onSend?: (content: string) => void;
}

// ─── Component ───────────────────────────────────────────────────────────────

/**
 * ChatInput provides a textarea and send button for composing and sending
 * chat messages.
 *
 * - Validates message length (1-32000 chars) client-side
 * - Sends via WebSocket `chat:send` message
 * - Disables input while agent is unavailable (no bound runtime or runtime offline)
 *
 * Requirements: 6.1, 6.8, 5.5
 */
export function ChatInput({ onSend }: ChatInputProps = {}) {
  const [content, setContent] = useState("");
  const [error, setError] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Store state
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const sessions = useSessionStore((state) => state.sessions);
  const runtimes = useRuntimeStore((state) => state.runtimes);
  const connectionStatus = useConnectionStore((state) => state.status);
  const isStreaming = useMessageStore((state) => state.isStreaming);

  // Determine if agent is available
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const boundRuntime = activeSession?.runtime_id
    ? runtimes.find((r) => r.id === activeSession.runtime_id)
    : null;

  const isAgentAvailable =
    connectionStatus === "connected" &&
    !!boundRuntime &&
    boundRuntime.status === "available";

  const isDisabled = !isAgentAvailable || isStreaming || !activeSessionId;

  // Auto-resize textarea
  useEffect(() => {
    const textarea = textareaRef.current;
    if (textarea) {
      textarea.style.height = "auto";
      textarea.style.height = `${Math.min(textarea.scrollHeight, 200)}px`;
    }
  }, [content]);

  const handleSend = useCallback(() => {
    const trimmed = content.trim();
    const validationError = validateMessageContent(trimmed);

    if (validationError) {
      setError(validationError);
      return;
    }

    if (!activeSessionId) {
      setError("No active session");
      return;
    }

    // Send via WebSocket
    const wsClient = getWsClient();
    wsClient.send("chat:send", {
      session_id: activeSessionId,
      content: trimmed,
    });

    // Clear input and error
    setContent("");
    setError(null);

    // Notify parent if callback provided
    onSend?.(trimmed);

    // Reset textarea height
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
  }, [content, activeSessionId, onSend]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      // Send on Enter (without Shift for newline)
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        if (!isDisabled) {
          handleSend();
        }
      }
    },
    [handleSend, isDisabled]
  );

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLTextAreaElement>) => {
      setContent(e.target.value);
      // Clear error when user starts typing
      if (error) {
        setError(null);
      }
    },
    [error]
  );

  // Character count for feedback
  const charCount = content.trim().length;
  const isOverLimit = charCount > MAX_MESSAGE_LENGTH;

  return (
    <div className="border-t border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 px-4 py-3">
      {/* Unavailable agent notice */}
      {!isAgentAvailable && activeSessionId && !isStreaming && (
        <div className="mb-2 text-sm text-amber-600 dark:text-amber-400 flex items-center gap-1.5">
          <UnavailableIcon />
          <span>
            {!boundRuntime
              ? "No agent bound to this session. Select an agent to start chatting."
              : connectionStatus !== "connected"
              ? "Reconnecting to server..."
              : "Agent is currently unavailable."}
          </span>
        </div>
      )}

      {/* Error message */}
      {error && (
        <div className="mb-2 text-sm text-red-600 dark:text-red-400" role="alert">
          {error}
        </div>
      )}

      {/* Input area */}
      <div className="flex items-end gap-2">
        <div className="flex-1 relative">
          <textarea
            ref={textareaRef}
            value={content}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
            disabled={isDisabled}
            placeholder={
              isDisabled
                ? "Agent unavailable..."
                : "Type a message... (Shift+Enter for new line)"
            }
            rows={1}
            className={`w-full resize-none rounded-lg border px-3 py-2 text-sm
              focus:outline-none focus:ring-2 focus:ring-blue-500
              disabled:cursor-not-allowed disabled:opacity-50
              dark:bg-gray-800 dark:text-gray-100
              ${
                isOverLimit
                  ? "border-red-400 dark:border-red-600"
                  : "border-gray-300 dark:border-gray-600"
              }`}
            aria-label="Chat message input"
            aria-invalid={!!error || isOverLimit}
            aria-describedby={error ? "chat-input-error" : undefined}
          />
          {/* Character count (shown when approaching limit) */}
          {charCount > MAX_MESSAGE_LENGTH * 0.9 && (
            <span
              className={`absolute bottom-1 right-2 text-xs ${
                isOverLimit
                  ? "text-red-500 dark:text-red-400"
                  : "text-gray-400 dark:text-gray-500"
              }`}
            >
              {charCount}/{MAX_MESSAGE_LENGTH}
            </span>
          )}
        </div>

        <button
          onClick={handleSend}
          disabled={isDisabled || charCount === 0 || isOverLimit}
          className="flex-shrink-0 rounded-lg bg-blue-600 px-3 py-2 text-white
            hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500
            disabled:cursor-not-allowed disabled:opacity-50
            transition-colors"
          aria-label="Send message"
        >
          <SendIcon />
        </button>
      </div>
    </div>
  );
}

// ─── Icons ───────────────────────────────────────────────────────────────────

function SendIcon() {
  return (
    <svg
      className="w-5 h-5"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      strokeWidth={2}
      aria-hidden="true"
    >
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M12 19V5m0 0l-7 7m7-7l7 7"
      />
    </svg>
  );
}

function UnavailableIcon() {
  return (
    <svg
      className="w-4 h-4 flex-shrink-0"
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
