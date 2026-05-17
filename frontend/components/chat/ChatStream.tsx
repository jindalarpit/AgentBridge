"use client";

import { useEffect, useRef } from "react";
import { useMessageStore } from "@/lib/store";

// ─── Component ───────────────────────────────────────────────────────────────

/**
 * ChatStream renders the currently streaming assistant response.
 *
 * Reads from `useMessageStore.streamingContent` and displays accumulated
 * tokens in real-time. Auto-scrolls to bottom as new tokens arrive.
 * Shows a typing indicator while streaming is active.
 *
 * Requirements: 6.3, 6.4, 6.6, 6.7
 */
export function ChatStream() {
  const isStreaming = useMessageStore((state) => state.isStreaming);
  const streamingContent = useMessageStore((state) => state.streamingContent);
  const containerRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom as new tokens arrive
  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollIntoView({ behavior: "smooth", block: "end" });
    }
  }, [streamingContent]);

  if (!isStreaming) {
    return null;
  }

  return (
    <div ref={containerRef} className="flex justify-start w-full mb-3">
      <div className="max-w-[80%] rounded-lg px-4 py-3 bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-gray-100">
        {streamingContent ? (
          <p className="text-sm whitespace-pre-wrap break-words">
            {streamingContent}
            <StreamingCursor />
          </p>
        ) : (
          <TypingIndicator />
        )}
      </div>
    </div>
  );
}

// ─── Sub-components ──────────────────────────────────────────────────────────

/** Blinking cursor shown at the end of streaming content */
function StreamingCursor() {
  return (
    <span className="inline-block w-2 h-4 ml-0.5 bg-gray-500 dark:bg-gray-400 animate-pulse align-middle" />
  );
}

/** Animated dots shown when streaming starts but no content has arrived yet */
function TypingIndicator() {
  return (
    <div className="flex items-center gap-1 py-1" aria-label="Agent is typing">
      <span className="w-2 h-2 rounded-full bg-gray-400 dark:bg-gray-500 animate-bounce [animation-delay:0ms]" />
      <span className="w-2 h-2 rounded-full bg-gray-400 dark:bg-gray-500 animate-bounce [animation-delay:150ms]" />
      <span className="w-2 h-2 rounded-full bg-gray-400 dark:bg-gray-500 animate-bounce [animation-delay:300ms]" />
    </div>
  );
}
