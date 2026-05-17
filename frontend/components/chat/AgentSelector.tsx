"use client";

import { useState, useCallback, useEffect, useRef } from "react";
import { useRuntimeStore, useSessionStore, Runtime } from "@/lib/store";
import { listRuntimes, bindRuntime } from "@/lib/api";

// ─── AgentSelector ───────────────────────────────────────────────────────────
// Dropdown component for selecting available agent runtimes.
// Displays agent type and version for each runtime.
// Calls bind endpoint on selection.
// Shows "no agents available" message with daemon start instructions when empty.
// Validates: Requirements 5.1, 5.2, 5.3, 5.4

export interface AgentSelectorProps {
  /** Optional callback after a runtime is bound (for testing/integration) */
  onBind?: (runtimeId: string, agentType: string, version: string) => void;
}

export function AgentSelector({ onBind }: AgentSelectorProps = {}) {
  const [isOpen, setIsOpen] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [isBinding, setIsBinding] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Store state
  const runtimes = useRuntimeStore((state) => state.runtimes);
  const setRuntimes = useRuntimeStore((state) => state.setRuntimes);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const sessions = useSessionStore((state) => state.sessions);
  const updateSession = useSessionStore((state) => state.updateSession);

  // Get the active session's bound runtime
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const boundRuntime = activeSession?.runtime_id
    ? runtimes.find((r) => r.id === activeSession.runtime_id)
    : null;

  // Filter to only available runtimes
  const availableRuntimes = runtimes.filter((r) => r.status === "available");

  // Fetch runtimes on mount and when dropdown opens
  const fetchRuntimes = useCallback(async () => {
    try {
      setIsLoading(true);
      setError(null);
      const result = await listRuntimes();
      const runtimesArray = Array.isArray(result) ? result : (result as unknown as { runtimes: Runtime[] }).runtimes ?? [];
      setRuntimes(runtimesArray);
    } catch (err) {
      setError("Failed to load agents");
      console.error("Failed to load runtimes:", err);
    } finally {
      setIsLoading(false);
    }
  }, [setRuntimes]);

  useEffect(() => {
    fetchRuntimes();
  }, [fetchRuntimes]);

  // Close dropdown on outside click
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  // Handle runtime selection and binding
  const handleSelect = useCallback(
    async (runtime: Runtime) => {
      if (!activeSessionId) {
        setError("No active session");
        return;
      }

      try {
        setIsBinding(true);
        setError(null);
        const result = await bindRuntime(activeSessionId, {
          runtime_id: runtime.id,
        });
        // Update session store with the new binding
        updateSession(activeSessionId, { runtime_id: result.runtime_id });
        setIsOpen(false);
        onBind?.(result.runtime_id, result.agent_type, result.version);
      } catch (err) {
        const message =
          err instanceof Error ? err.message : "Failed to bind agent";
        setError(message);
        console.error("Failed to bind runtime:", err);
      } finally {
        setIsBinding(false);
      }
    },
    [activeSessionId, updateSession, onBind]
  );

  const handleToggle = useCallback(() => {
    if (!isOpen) {
      // Refresh runtimes when opening
      fetchRuntimes();
    }
    setIsOpen((prev) => !prev);
  }, [isOpen, fetchRuntimes]);

  // ─── Render ──────────────────────────────────────────────────────────────

  return (
    <div className="relative" ref={dropdownRef}>
      {/* Trigger button */}
      <button
        onClick={handleToggle}
        disabled={!activeSessionId}
        className="flex items-center gap-2 rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-3 py-1.5 text-sm font-medium text-gray-700 dark:text-gray-200 shadow-sm transition-colors hover:bg-gray-50 dark:hover:bg-gray-700 disabled:cursor-not-allowed disabled:opacity-50"
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        aria-label="Select agent"
      >
        <AgentIcon />
        <span className="truncate max-w-[150px]">
          {boundRuntime
            ? `${formatAgentType(boundRuntime.agent_type)} v${boundRuntime.version}`
            : "Select Agent"}
        </span>
        <ChevronIcon isOpen={isOpen} />
      </button>

      {/* Dropdown panel */}
      {isOpen && (
        <div
          className="absolute left-0 top-full z-20 mt-1 w-72 rounded-md border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 shadow-lg"
          role="listbox"
          aria-label="Available agents"
        >
          {/* Loading state */}
          {isLoading && (
            <div className="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">
              Loading agents...
            </div>
          )}

          {/* Error state */}
          {error && (
            <div className="px-4 py-3 text-sm text-red-600 dark:text-red-400" role="alert">
              {error}
            </div>
          )}

          {/* Empty state — no agents available */}
          {!isLoading && !error && availableRuntimes.length === 0 && (
            <div className="px-4 py-4">
              <div className="flex items-start gap-2">
                <WarningIcon />
                <div>
                  <p className="text-sm font-medium text-gray-700 dark:text-gray-200">
                    No agents available
                  </p>
                  <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                    Start the AgentBridge daemon on your machine to detect local
                    AI agents:
                  </p>
                  <code className="mt-2 block rounded bg-gray-100 dark:bg-gray-900 px-2 py-1 text-xs text-gray-800 dark:text-gray-200 font-mono">
                    agentbridge-daemon start
                  </code>
                  <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">
                    The daemon will automatically detect installed agent CLIs
                    (Claude, Gemini, Kiro, etc.) and register them with the
                    server.
                  </p>
                </div>
              </div>
            </div>
          )}

          {/* Runtime list */}
          {!isLoading && availableRuntimes.length > 0 && (
            <ul className="max-h-60 overflow-y-auto py-1">
              {availableRuntimes.map((runtime) => {
                const isSelected = boundRuntime?.id === runtime.id;
                return (
                  <li key={runtime.id}>
                    <button
                      onClick={() => handleSelect(runtime)}
                      disabled={isBinding}
                      className={`flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm transition-colors ${
                        isSelected
                          ? "bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300"
                          : "text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700"
                      } disabled:cursor-wait disabled:opacity-70`}
                      role="option"
                      aria-selected={isSelected}
                    >
                      <AgentTypeIcon agentType={runtime.agent_type} />
                      <div className="flex-1 min-w-0">
                        <p className="font-medium truncate">
                          {formatAgentType(runtime.agent_type)}
                        </p>
                        <p className="text-xs text-gray-500 dark:text-gray-400">
                          v{runtime.version}
                        </p>
                      </div>
                      {isSelected && <CheckIcon />}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}

          {/* Binding indicator */}
          {isBinding && (
            <div className="border-t border-gray-200 dark:border-gray-700 px-4 py-2 text-xs text-gray-500 dark:text-gray-400">
              Binding agent...
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

/** Format agent type for display (capitalize, handle hyphens) */
function formatAgentType(agentType: string): string {
  return agentType
    .split("-")
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

// ─── Icons ───────────────────────────────────────────────────────────────────

function AgentIcon() {
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
        d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
      />
    </svg>
  );
}

function ChevronIcon({ isOpen }: { isOpen: boolean }) {
  return (
    <svg
      className={`w-4 h-4 flex-shrink-0 transition-transform ${
        isOpen ? "rotate-180" : ""
      }`}
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      strokeWidth={2}
      aria-hidden="true"
    >
      <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
    </svg>
  );
}

function WarningIcon() {
  return (
    <svg
      className="w-5 h-5 flex-shrink-0 text-amber-500"
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

function CheckIcon() {
  return (
    <svg
      className="w-4 h-4 flex-shrink-0 text-blue-600 dark:text-blue-400"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      strokeWidth={2}
      aria-hidden="true"
    >
      <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
    </svg>
  );
}

function AgentTypeIcon({ agentType }: { agentType: string }) {
  // Simple colored circle indicator based on agent type
  const colorMap: Record<string, string> = {
    claude: "bg-orange-400",
    gemini: "bg-blue-400",
    "kiro-cli": "bg-purple-400",
    codex: "bg-green-400",
    copilot: "bg-gray-600",
    opencode: "bg-teal-400",
    hermes: "bg-yellow-400",
    pi: "bg-pink-400",
    "cursor-agent": "bg-indigo-400",
    kimi: "bg-red-400",
  };

  const color = colorMap[agentType] ?? "bg-gray-400";

  return (
    <span
      className={`inline-block w-3 h-3 rounded-full flex-shrink-0 ${color}`}
      aria-hidden="true"
    />
  );
}
