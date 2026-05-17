"use client";

import { useEffect, useState, useRef } from "react";
import { useRouter, usePathname } from "next/navigation";
import { useSessionStore, ChatSession } from "@/lib/store";
import {
  listSessions,
  createSession,
  deleteSession,
  updateSession,
} from "@/lib/api";

// ─── SessionList ─────────────────────────────────────────────────────────────
// Sidebar component showing chat sessions ordered by recent activity.
// Supports create, rename, and delete actions.
// Validates: Requirements 4.1, 4.2, 4.4, 4.5

export function SessionList() {
  const router = useRouter();
  const pathname = usePathname();
  const {
    sessions,
    setSessions,
    addSession,
    removeSession,
    updateSession: updateStoreSession,
    activeSessionId,
    setActiveSessionId,
  } = useSessionStore();

  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editTitle, setEditTitle] = useState("");
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  // Load sessions on mount
  useEffect(() => {
    async function loadSessions() {
      try {
        setIsLoading(true);
        setError(null);
        const result = await listSessions();
        setSessions(result.sessions as unknown as ChatSession[]);
      } catch (err) {
        setError("Failed to load sessions");
        console.error("Failed to load sessions:", err);
      } finally {
        setIsLoading(false);
      }
    }
    loadSessions();
  }, [setSessions]);

  // Sync active session from URL
  useEffect(() => {
    const match = pathname.match(/\/chat\/([^/]+)/);
    if (match) {
      setActiveSessionId(match[1]);
    }
  }, [pathname, setActiveSessionId]);

  // Close menu on outside click
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpenId(null);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  // ─── Actions ─────────────────────────────────────────────────────────────

  async function handleCreateSession() {
    try {
      const session = await createSession();
      addSession(session as unknown as ChatSession);
      setActiveSessionId(session.id);
      router.push(`/chat/${session.id}`);
    } catch (err) {
      console.error("Failed to create session:", err);
    }
  }

  async function handleDeleteSession(id: string) {
    try {
      await deleteSession(id);
      removeSession(id);
      setMenuOpenId(null);
      // Navigate away if the deleted session was active
      if (activeSessionId === id) {
        const remaining = sessions.filter((s) => s.id !== id);
        if (remaining.length > 0) {
          router.push(`/chat/${remaining[0].id}`);
        } else {
          router.push("/chat");
        }
      }
    } catch (err) {
      console.error("Failed to delete session:", err);
    }
  }

  function handleStartRename(session: ChatSession) {
    setEditingId(session.id);
    setEditTitle(session.title);
    setMenuOpenId(null);
  }

  async function handleRenameSubmit(id: string) {
    const trimmed = editTitle.trim();
    if (trimmed.length < 1 || trimmed.length > 100) {
      // Invalid title — revert
      setEditingId(null);
      return;
    }
    try {
      await updateSession(id, { title: trimmed });
      updateStoreSession(id, { title: trimmed });
    } catch (err) {
      console.error("Failed to rename session:", err);
    } finally {
      setEditingId(null);
    }
  }

  function handleRenameKeyDown(e: React.KeyboardEvent, id: string) {
    if (e.key === "Enter") {
      e.preventDefault();
      handleRenameSubmit(id);
    } else if (e.key === "Escape") {
      setEditingId(null);
    }
  }

  function handleSelectSession(id: string) {
    setActiveSessionId(id);
    router.push(`/chat/${id}`);
  }

  // ─── Sorted sessions (most recent activity first) ────────────────────────

  const sortedSessions = [...sessions].sort((a, b) => {
    const aTime = new Date(a.updated_at).getTime();
    const bTime = new Date(b.updated_at).getTime();
    return bTime - aTime;
  });

  // ─── Render ──────────────────────────────────────────────────────────────

  return (
    <div className="flex h-full flex-col">
      {/* New Chat button */}
      <div className="p-3">
        <button
          onClick={handleCreateSession}
          className="flex w-full items-center justify-center gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm transition-colors hover:bg-gray-50"
          aria-label="New Chat"
        >
          <PlusIcon />
          <span>New Chat</span>
        </button>
      </div>

      {/* Session list */}
      <nav className="flex-1 overflow-y-auto px-2" aria-label="Chat sessions">
        {isLoading && (
          <p className="px-3 py-2 text-sm text-gray-400">Loading...</p>
        )}
        {error && (
          <p className="px-3 py-2 text-sm text-red-500">{error}</p>
        )}
        {!isLoading && !error && sortedSessions.length === 0 && (
          <p className="px-3 py-2 text-sm text-gray-400">
            No conversations yet
          </p>
        )}
        <ul className="space-y-0.5">
          {sortedSessions.map((session) => (
            <li key={session.id} className="relative">
              {editingId === session.id ? (
                <div className="px-2 py-1">
                  <input
                    type="text"
                    value={editTitle}
                    onChange={(e) => setEditTitle(e.target.value)}
                    onKeyDown={(e) => handleRenameKeyDown(e, session.id)}
                    onBlur={() => handleRenameSubmit(session.id)}
                    className="w-full rounded border border-blue-400 px-2 py-1 text-sm outline-none focus:ring-1 focus:ring-blue-400"
                    maxLength={100}
                    autoFocus
                    aria-label="Rename session"
                  />
                </div>
              ) : (
                <button
                  onClick={() => handleSelectSession(session.id)}
                  className={`group flex w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm transition-colors ${
                    activeSessionId === session.id
                      ? "bg-blue-50 text-blue-700"
                      : "text-gray-700 hover:bg-gray-100"
                  }`}
                  aria-current={
                    activeSessionId === session.id ? "page" : undefined
                  }
                >
                  <span className="truncate">{session.title}</span>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      setMenuOpenId(
                        menuOpenId === session.id ? null : session.id
                      );
                    }}
                    className="ml-1 rounded p-0.5 opacity-0 transition-opacity hover:bg-gray-200 group-hover:opacity-100"
                    aria-label={`Actions for ${session.title}`}
                    aria-haspopup="menu"
                  >
                    <EllipsisIcon />
                  </button>
                </button>
              )}

              {/* Context menu */}
              {menuOpenId === session.id && (
                <div
                  ref={menuRef}
                  className="absolute right-2 top-8 z-10 w-36 rounded-md border border-gray-200 bg-white py-1 shadow-lg"
                  role="menu"
                >
                  <button
                    onClick={() => handleStartRename(session)}
                    className="flex w-full items-center px-3 py-1.5 text-left text-sm text-gray-700 hover:bg-gray-100"
                    role="menuitem"
                  >
                    Rename
                  </button>
                  <button
                    onClick={() => handleDeleteSession(session.id)}
                    className="flex w-full items-center px-3 py-1.5 text-left text-sm text-red-600 hover:bg-red-50"
                    role="menuitem"
                  >
                    Delete
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      </nav>
    </div>
  );
}

// ─── Icons ───────────────────────────────────────────────────────────────────

function PlusIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <line x1="12" y1="5" x2="12" y2="19" />
      <line x1="5" y1="12" x2="19" y2="12" />
    </svg>
  );
}

function EllipsisIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="1" />
      <circle cx="19" cy="12" r="1" />
      <circle cx="5" cy="12" r="1" />
    </svg>
  );
}
