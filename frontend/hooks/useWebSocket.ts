"use client";

import { useEffect, useRef } from "react";
import { getWsClient, WsMessage } from "@/lib/ws";
import { getToken } from "@/lib/api";
import {
  useAuthStore,
  useConnectionStore,
  handleChatStream,
  handleChatDone,
  handleChatError,
  handleRuntimeStatus,
  handleSessionCreated,
  handleSessionDeleted,
  handleSessionUpdated,
} from "@/lib/store";

/**
 * useWebSocket manages the WebSocket lifecycle and dispatches incoming
 * messages to the appropriate Zustand stores.
 *
 * - Connects when authenticated, disconnects on logout
 * - Updates connection status store (connected/disconnected/reconnecting)
 * - Sets tokenExpired flag when server closes connection due to auth failure
 * - Dispatches typed messages to store handlers
 *
 * Requirements: 3.3, 10.3, 10.5, 3.5
 */
export function useWebSocket() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const token = useAuthStore((state) => state.token);
  const connectionStatus = useConnectionStore((state) => state.status);
  const setStatus = useConnectionStore((state) => state.setStatus);
  const setTokenExpired = useConnectionStore((state) => state.setTokenExpired);
  const initializedRef = useRef(false);

  useEffect(() => {
    if (!isAuthenticated || !token) {
      // Not authenticated — disconnect if connected
      if (initializedRef.current) {
        const client = getWsClient();
        client.disconnect();
        setStatus("disconnected");
        initializedRef.current = false;
      }
      return;
    }

    const client = getWsClient();
    client.setToken(token);

    client.setOnConnect(() => {
      setStatus("connected");
    });

    client.setOnDisconnect(() => {
      // If not intentionally closed, we're reconnecting
      setStatus("reconnecting");
    });

    client.setOnTokenExpired(() => {
      setStatus("disconnected");
      setTokenExpired(true);
    });

    client.setOnMessage((msg: WsMessage) => {
      switch (msg.type) {
        case "chat:stream":
          handleChatStream(msg.payload as Parameters<typeof handleChatStream>[0]);
          break;
        case "chat:done":
          handleChatDone(msg.payload as Parameters<typeof handleChatDone>[0]);
          break;
        case "chat:error":
          handleChatError(msg.payload as Parameters<typeof handleChatError>[0]);
          break;
        case "runtime:status":
          handleRuntimeStatus(msg.payload as Parameters<typeof handleRuntimeStatus>[0]);
          break;
        case "session:created":
          handleSessionCreated(msg.payload as Parameters<typeof handleSessionCreated>[0]);
          break;
        case "session:deleted":
          handleSessionDeleted(msg.payload as Parameters<typeof handleSessionDeleted>[0]);
          break;
        case "session:updated":
          handleSessionUpdated(msg.payload as Parameters<typeof handleSessionUpdated>[0]);
          break;
        default:
          // Unknown message type — ignore
          break;
      }
    });

    client.connect();
    initializedRef.current = true;

    return () => {
      client.disconnect();
      initializedRef.current = false;
    };
  }, [isAuthenticated, token, setStatus, setTokenExpired]);

  return { isConnected: connectionStatus === "connected" };
}
