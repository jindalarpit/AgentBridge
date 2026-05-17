"use client";

import { SessionList } from "@/components/chat/SessionList";
import { ConnectionBanner } from "@/components/chat/ConnectionBanner";
import { ReAuthModal } from "@/components/chat/ReAuthModal";
import { useWebSocket } from "@/hooks/useWebSocket";

// Chat layout with sidebar listing sessions + main content area
// Validates: Requirements 4.1, 4.2, 4.4, 4.5, 10.3, 10.5, 3.5

export default function ChatLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  useWebSocket();
  return (
    <div className="flex h-screen">
      {/* Sidebar */}
      <aside className="flex w-64 flex-col border-r border-gray-200 bg-gray-50">
        <div className="flex h-12 items-center border-b border-gray-200 px-4">
          <h2 className="text-sm font-semibold text-gray-700">AgentBridge</h2>
        </div>
        <SessionList />
      </aside>

      {/* Main content */}
      <main className="flex flex-1 flex-col overflow-hidden">
        <ConnectionBanner />
        {children}
      </main>

      {/* Re-auth modal (shown on token expiry) */}
      <ReAuthModal />
    </div>
  );
}
