"use client";

import { useEffect } from "react";
import { useAuthStore, useRuntimeStore } from "@/lib/store";
import { useRouter } from "next/navigation";
import { listRuntimes } from "@/lib/api";

export default function ChatPage() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const runtimes = useRuntimeStore((state) => state.runtimes);
  const setRuntimes = useRuntimeStore((state) => state.setRuntimes);
  const router = useRouter();

  useEffect(() => {
    if (!isAuthenticated) {
      router.replace("/");
    }
  }, [isAuthenticated, router]);

  // Fetch runtimes to show status
  useEffect(() => {
    async function fetchRuntimes() {
      try {
        const result = await listRuntimes();
        const runtimesArray = Array.isArray(result) ? result : (result as unknown as { runtimes: typeof runtimes }).runtimes ?? [];
        setRuntimes(runtimesArray);
      } catch {
        // Ignore — user may not have any runtimes yet
      }
    }
    fetchRuntimes();
  }, [setRuntimes]);

  const availableRuntimes = runtimes.filter((r) => r.status === "available");

  return (
    <div className="flex flex-1 items-center justify-center p-8">
      <div className="max-w-lg w-full space-y-8">
        {/* Welcome */}
        <div className="text-center">
          <h2 className="text-xl font-semibold text-gray-800 dark:text-gray-200">
            Welcome to AgentBridge
          </h2>
          <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">
            Chat with your local AI agents through the web.
          </p>
        </div>

        {/* Agent Status */}
        <div className="rounded-lg border border-gray-200 dark:border-gray-700 p-5">
          <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3 flex items-center gap-2">
            <span className={`inline-block w-2.5 h-2.5 rounded-full ${availableRuntimes.length > 0 ? "bg-green-500" : "bg-gray-300"}`} />
            Registered Agents
          </h3>

          {availableRuntimes.length > 0 ? (
            <div className="space-y-2">
              {availableRuntimes.map((rt) => (
                <div
                  key={rt.id}
                  className="flex items-center justify-between rounded-md bg-gray-50 dark:bg-gray-800 px-3 py-2"
                >
                  <div className="flex items-center gap-2">
                    <span className="inline-block w-2 h-2 rounded-full bg-green-500" />
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-200 capitalize">
                      {rt.agent_type}
                    </span>
                  </div>
                  <span className="text-xs text-gray-500 dark:text-gray-400">
                    v{rt.version}
                  </span>
                </div>
              ))}
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-3">
                Click &quot;New Chat&quot; in the sidebar, then select an agent to start chatting.
              </p>
            </div>
          ) : (
            <div className="space-y-3">
              <p className="text-sm text-gray-500 dark:text-gray-400">
                No agents detected. Start the AgentBridge daemon on your machine to register your local AI agent CLIs.
              </p>
              <div className="rounded-md bg-gray-50 dark:bg-gray-800 p-4 space-y-3">
                <p className="text-xs font-medium text-gray-600 dark:text-gray-300">
                  Step 1: Set up the daemon
                </p>
                <code className="block rounded bg-gray-100 dark:bg-gray-900 px-3 py-2 text-xs font-mono text-gray-800 dark:text-gray-200">
                  export AGENTBRIDGE_TOKEN=&quot;your-jwt-token&quot;{"\n"}
                  export AGENTBRIDGE_USER_ID=&quot;your-user-id&quot;{"\n"}
                  export AGENTBRIDGE_SERVER_URL=&quot;ws://localhost:8080/ws/daemon&quot;
                </code>
                <p className="text-xs font-medium text-gray-600 dark:text-gray-300">
                  Step 2: Start the daemon
                </p>
                <code className="block rounded bg-gray-100 dark:bg-gray-900 px-3 py-2 text-xs font-mono text-gray-800 dark:text-gray-200">
                  agentbridge-daemon start
                </code>
                <p className="text-xs text-gray-500 dark:text-gray-400">
                  The daemon will automatically scan your PATH for supported agent CLIs
                  (claude, gemini, kiro-cli, codex, copilot, etc.) and register them with the server.
                </p>
              </div>
              <p className="text-xs text-gray-400 dark:text-gray-500">
                Supported agents: Claude, Gemini, Kiro CLI, Codex, Copilot, OpenCode, Hermes, Pi, Cursor Agent, Kimi
              </p>
            </div>
          )}
        </div>

        {/* Quick start hint */}
        {availableRuntimes.length > 0 && (
          <div className="text-center">
            <p className="text-xs text-gray-400">
              Select a session from the sidebar or create a new one to begin.
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
