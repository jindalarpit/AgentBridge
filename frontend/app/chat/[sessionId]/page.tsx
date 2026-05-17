"use client";

import { useEffect, useRef, use } from "react";
import { useSessionStore, useMessageStore, ChatMessage as ChatMessageType } from "@/lib/store";
import { getMessages } from "@/lib/api";
import { ChatMessage } from "@/components/chat/ChatMessage";
import { ChatStream } from "@/components/chat/ChatStream";
import { ChatInput } from "@/components/chat/ChatInput";
import { AgentSelector } from "@/components/chat/AgentSelector";

export default function ChatSessionPage({
  params,
}: {
  params: Promise<{ sessionId: string }>;
}) {
  const { sessionId } = use(params);
  const setActiveSessionId = useSessionStore((s) => s.setActiveSessionId);
  const sessions = useSessionStore((s) => s.sessions);
  const messages = useMessageStore((s) => s.messages);
  const setMessages = useMessageStore((s) => s.setMessages);
  const clearMessages = useMessageStore((s) => s.clearMessages);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const activeSession = sessions.find((s) => s.id === sessionId);

  // Set active session on mount
  useEffect(() => {
    setActiveSessionId(sessionId);
    return () => setActiveSessionId(null);
  }, [sessionId, setActiveSessionId]);

  // Load messages for this session
  useEffect(() => {
    async function loadMessages() {
      try {
        const result = await getMessages(sessionId);
        // The API returns { messages: [...] }
        const msgs = Array.isArray(result) ? result : (result as unknown as { messages: ChatMessageType[] }).messages ?? [];
        setMessages(msgs);
      } catch (err) {
        console.error("Failed to load messages:", err);
        setMessages([]);
      }
    }
    clearMessages();
    loadMessages();
  }, [sessionId, setMessages, clearMessages]);

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  return (
    <div className="flex h-full flex-col">
      {/* Header with session title and agent selector */}
      <header className="flex items-center justify-between border-b border-gray-200 dark:border-gray-700 px-4 py-2">
        <h1 className="text-sm font-medium text-gray-700 dark:text-gray-200 truncate">
          {activeSession?.title ?? "Chat"}
        </h1>
        <AgentSelector />
      </header>

      {/* Messages area */}
      <div className="flex-1 overflow-y-auto px-4 py-4">
        {messages.length === 0 && (
          <div className="flex h-full items-center justify-center">
            <div className="text-center">
              <p className="text-sm text-gray-500 dark:text-gray-400">
                No messages yet. Select an agent above and start chatting.
              </p>
              <p className="mt-2 text-xs text-gray-400 dark:text-gray-500">
                If no agents appear, start the daemon on your machine:
              </p>
              <code className="mt-1 inline-block rounded bg-gray-100 dark:bg-gray-800 px-2 py-1 text-xs font-mono text-gray-700 dark:text-gray-300">
                agentbridge-daemon start
              </code>
            </div>
          </div>
        )}

        {messages.map((msg, index) => (
          <ChatMessage key={`${msg.id}-${index}`} message={msg} />
        ))}

        {/* Streaming response */}
        <ChatStream />

        <div ref={messagesEndRef} />
      </div>

      {/* Input area */}
      <ChatInput />
    </div>
  );
}
