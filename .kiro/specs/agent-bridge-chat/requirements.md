# Requirements Document

## Introduction

AgentBridge is a lightweight web application that enables users to interact with AI agents through a chat interface. The system detects locally running AI agent CLIs on each user's machine (such as Claude Code, Kiro CLI, Gemini CLI, etc.), allows users to bind a detected agent to their chat session, and relays messages between the web UI and the local agent in real time. A local daemon process on each user's machine handles agent detection, server registration, heartbeats, and task execution. The server maintains runtime state and relays communication between the web UI and daemons via WebSocket connections. The system supports thousands of concurrent users, each on their own machine with their own local agents.

## Glossary

- **Server**: The central backend application that manages user sessions, daemon registrations, message relay, and runtime state
- **Daemon**: A lightweight background process running on each user's machine that detects local agent CLIs, registers with the Server, sends heartbeats, and executes chat tasks
- **Agent_CLI**: A locally installed AI coding assistant command-line interface (e.g., claude, kiro-cli, gemini, codex)
- **Chat_Session**: A persistent conversation thread between a user and a bound agent, identified by a unique session ID
- **Agent_Binding**: The association between a Chat_Session and a specific detected Agent_CLI on the user's Daemon
- **Runtime**: A registered instance of an Agent_CLI on a specific Daemon, representing an available agent execution environment
- **Heartbeat**: A periodic signal sent from the Daemon to the Server to indicate liveness and availability
- **WebSocket_Connection**: A persistent bidirectional communication channel between the Server and either the web UI client or a Daemon

## Requirements

### Requirement 1: Local Agent Detection

**User Story:** As a user, I want the system to automatically detect AI agent CLIs installed on my machine, so that I can use them for chat without manual configuration.

#### Acceptance Criteria

1. WHEN the Daemon starts, THE Daemon SHALL scan the system PATH for supported Agent_CLI binaries (claude, kiro-cli, gemini, codex, copilot, opencode, hermes, pi, cursor-agent, kimi) and additionally check agent-specific environment variable path overrides (e.g., MULTICA_CLAUDE_PATH) for custom binary locations
2. WHEN a supported Agent_CLI binary is found, THE Daemon SHALL determine its version by executing the binary with a version flag, applying a timeout of 10 seconds for the version command to complete
3. IF a version command fails, times out, or returns unparseable output, THEN THE Daemon SHALL mark that Agent_CLI as detected but unavailable, record the failure reason, and continue scanning remaining binaries
4. WHEN detection completes, THE Daemon SHALL compile a list of RuntimeInfo objects containing the agent type, binary path, version (or unknown if version retrieval failed), and availability status (available if version was successfully retrieved, unavailable otherwise) for each detected Agent_CLI
5. WHEN no supported Agent_CLI binaries are found, THE Daemon SHALL report an empty runtime list and log a warning message
6. WHILE the Daemon is running, THE Daemon SHALL re-scan for Agent_CLI binaries at a configurable interval (default: 60 seconds) to detect newly installed or removed agents, updating the RuntimeInfo list to reflect current system state

### Requirement 2: Daemon Registration and Heartbeat

**User Story:** As a user, I want my local daemon to register with the server and maintain a live connection, so that the server knows my machine is available for chat.

#### Acceptance Criteria

1. WHEN the Daemon starts and agent detection completes, THE Daemon SHALL establish a WebSocket_Connection to the Server and send a DaemonRegister message containing the daemon ID, user ID, and list of detected Runtimes
2. WHEN the Server receives a valid DaemonRegister message, THE Server SHALL store or update the daemon record and its associated Runtimes in the runtime state registry, replacing any previously registered Runtimes for that daemon
3. WHILE the Daemon is connected, THE Daemon SHALL send a Heartbeat message to the Server at a configurable interval (default: 15 seconds, minimum: 5 seconds, maximum: 120 seconds)
4. WHEN the Server receives a Heartbeat, THE Server SHALL update the last_seen_at timestamp for the corresponding daemon record
5. IF the Server does not receive a Heartbeat from a Daemon within three consecutive heartbeat intervals, THEN THE Server SHALL mark the daemon and its Runtimes as offline
6. WHEN the Daemon loses its WebSocket_Connection to the Server, THE Daemon SHALL attempt to reconnect using exponential backoff starting at 1 second with a maximum delay of 60 seconds
7. WHEN the Daemon reconnects after a disconnection, THE Daemon SHALL re-send the DaemonRegister message with the current runtime list
8. IF the Daemon cannot establish the initial WebSocket_Connection to the Server, THEN THE Daemon SHALL retry using the same exponential backoff strategy as reconnection (starting at 1 second, maximum delay of 60 seconds) and SHALL log each failed attempt
9. IF the Server receives a DaemonRegister message with missing required fields or an unrecognized user ID, THEN THE Server SHALL reject the registration, send an error message indicating the reason for rejection, and close the WebSocket_Connection

### Requirement 3: User Authentication and Session Management

**User Story:** As a user, I want to log in and have my chat sessions persist across browser refreshes, so that I can continue conversations without losing context.

#### Acceptance Criteria

1. WHEN a user navigates to the web application, THE Server SHALL require authentication before granting access to chat functionality
2. WHEN a user authenticates successfully, THE Server SHALL create a user session with a unique session token valid for 24 hours
3. WHEN a user opens the chat interface, THE Server SHALL establish a WebSocket_Connection between the client browser and the Server for real-time message delivery
4. THE Server SHALL support at least 5000 concurrent authenticated user sessions while maintaining message delivery latency under 200 milliseconds at the 95th percentile
5. WHEN a user session token expires, THE Server SHALL prompt the user to re-authenticate and SHALL NOT discard existing Chat_Session history
6. IF a user's WebSocket_Connection drops unexpectedly, THEN THE Server SHALL retain the Chat_Session state for at least 5 minutes and allow the user to resume upon reconnection within that window

### Requirement 4: Chat Session Creation and Management

**User Story:** As a user, I want to create, list, and manage chat sessions, so that I can organize my conversations with different agents.

#### Acceptance Criteria

1. WHEN a user requests a new chat, THE Server SHALL create a new Chat_Session with a unique ID, a creation timestamp, a default title set to "New Chat", and associate it with the authenticated user
2. WHEN a user requests their chat session list, THE Server SHALL return up to 50 Chat_Sessions per page ordered by the most recent message timestamp (or creation timestamp if no messages exist), with pagination support for additional results
3. WHEN a user selects an existing Chat_Session, THE Server SHALL load and return the full message history for that session
4. WHEN a user deletes a Chat_Session, THE Server SHALL cancel any in-progress agent task for that session, remove the session and its message history from persistent storage, and notify the user's other connected clients of the deletion
5. WHEN a user renames a Chat_Session, THE Server SHALL validate that the new title is between 1 and 100 characters after trimming whitespace, update the session title, and broadcast the change to other connected clients of the same user
6. IF a user attempts to rename a Chat_Session with an empty or whitespace-only title or a title exceeding 100 characters, THEN THE Server SHALL reject the request with an error message indicating the title length constraints

### Requirement 5: Agent Binding

**User Story:** As a user, I want to select which detected local agent to use for a chat session, so that I can choose the best agent for my task.

#### Acceptance Criteria

1. WHEN a user opens the agent selection interface, THE Server SHALL return the list of Runtimes currently registered by the user's Daemon that have an online status, including each Runtime's agent type and version, within 2 seconds
2. WHEN a user selects a Runtime from the available list, THE Server SHALL create an Agent_Binding associating the current Chat_Session with the selected Runtime, replacing any existing Agent_Binding for that Chat_Session
3. WHEN an Agent_Binding is created, THE Server SHALL return the bound agent's type and version to the client for display in the chat interface header
4. IF the user's Daemon has no online Runtimes, THEN THE Server SHALL display a message indicating no local agents are available and include instructions for starting the Daemon
5. WHEN a bound Runtime goes offline during an active Chat_Session, THE Server SHALL notify the user that the agent is unavailable and disable message sending until the Runtime returns online or a new binding is created
6. WHEN a user changes the Agent_Binding for an existing Chat_Session, THE Server SHALL update the binding, preserve the existing message history, and use the newly selected Runtime for subsequent messages
7. IF a user selects a Runtime that has gone offline since the list was retrieved, THEN THE Server SHALL reject the binding request, return an error indicating the selected agent is no longer available, and prompt the user to refresh the available Runtimes list

### Requirement 6: Real-Time Chat Message Flow

**User Story:** As a user, I want to send messages and receive agent responses in real time, so that the chat experience feels instant and interactive.

#### Acceptance Criteria

1. WHEN a user sends a chat message, THE Server SHALL persist the message, associate it with the Chat_Session, and relay it to the user's Daemon via the Daemon's WebSocket_Connection within 100 milliseconds
2. WHEN the Daemon receives a chat message, THE Daemon SHALL invoke the bound Agent_CLI with the message content and the session's conversation history (up to the most recent 200 messages) as context
3. WHILE the Agent_CLI is generating a response, THE Daemon SHALL stream intermediate output tokens back to the Server as ChatMessage payloads with a monotonically increasing sequence number, sending each token within 200 milliseconds of its generation
4. WHEN the Server receives a ChatMessage from the Daemon, THE Server SHALL forward it to the user's browser WebSocket_Connection within 50 milliseconds of receipt
5. WHEN the Agent_CLI completes its response, THE Daemon SHALL send a ChatDone message to the Server containing the final complete response and elapsed time
6. WHEN the Server receives a ChatDone message, THE Server SHALL persist the complete assistant message and notify the client that the response is complete
7. IF the Agent_CLI fails or does not produce any output within 300 seconds during response generation, THEN THE Daemon SHALL send an error message to the Server indicating the failure reason, THE Server SHALL display the error to the user in the chat interface, and any partial response tokens already delivered SHALL be retained in the chat display
8. IF a user sends a chat message that is empty or exceeds 32,000 characters, THEN THE Server SHALL reject the message and return an error indication to the client without relaying it to the Daemon
9. WHILE the Daemon is processing a chat message for a Chat_Session, IF the user sends another message in the same Chat_Session, THEN THE Server SHALL queue the new message and deliver it to the Daemon only after the current response completes or fails

### Requirement 7: Multi-User Isolation

**User Story:** As a user, I want my chat sessions and agent bindings to be completely isolated from other users, so that my conversations remain private and my local agents are only accessible to me.

#### Acceptance Criteria

1. THE Server SHALL associate each Daemon registration with exactly one authenticated user and SHALL NOT allow any other user to list, invoke, or access Runtimes belonging to that Daemon
2. THE Server SHALL enforce that a user can only create Agent_Bindings to Runtimes registered by their own Daemon
3. THE Server SHALL store Chat_Session data with user-scoped access controls, preventing any user from reading or modifying another user's sessions
4. WHEN the Server relays a chat message to a Daemon, THE Server SHALL verify that the target Daemon belongs to the authenticated user who owns the Chat_Session
5. THE Server SHALL maintain separate WebSocket_Connection channels per user, ensuring that broadcast messages for one user are never delivered to another user's connections
6. IF a user attempts any operation targeting a resource owned by a different user (including Runtimes, Chat_Sessions, Agent_Bindings, or Daemon registrations), THEN THE Server SHALL reject the request with an authorization error indicating access is denied and SHALL NOT reveal whether the target resource exists
7. IF relay verification in criterion 4 fails, THEN THE Server SHALL discard the message without delivering it to any Daemon, return an error to the requesting client indicating the agent is not accessible, and log the violation for audit purposes

### Requirement 8: Daemon Lifecycle Management

**User Story:** As a user, I want to easily start, stop, and monitor my local daemon, so that I can control when my machine is available for chat.

#### Acceptance Criteria

1. WHEN the user executes the daemon start command and no existing Daemon instance is running, THE Daemon SHALL start as a background process, perform agent detection, register with the Server, and output a confirmation message indicating successful startup within 10 seconds
2. IF the user executes the daemon start command while a Daemon instance is already running, THEN THE Daemon SHALL not start a second instance and SHALL output a message indicating that the Daemon is already running
3. WHEN the user executes the daemon stop command and the Daemon is running, THE Daemon SHALL deregister all Runtimes from the Server, close the WebSocket_Connection, and terminate the background process within 10 seconds
4. IF the user executes the daemon stop command while no Daemon instance is running, THEN THE Daemon SHALL output a message indicating that no running Daemon was found
5. WHEN the user executes the daemon status command, THE Daemon SHALL report its connection state (one of: connected, disconnected, reconnecting), uptime in seconds, list of detected agents with their types and versions, and current task status (one of: idle, executing)
6. THE Daemon SHALL write operational logs to a configurable log file (default: ~/.agentbridge/daemon.log) and SHALL rotate the log file when it reaches 50 MB, retaining the 3 most recent rotated files
7. IF the Daemon process crashes unexpectedly, THEN THE Server SHALL detect the absence via missed Heartbeats and mark the associated Runtimes as offline within 45 seconds
8. IF the Daemon fails to connect to the Server during startup, THEN THE Daemon SHALL output an error message indicating the connection failure and terminate the process with a non-zero exit code

### Requirement 9: Chat Message Persistence

**User Story:** As a user, I want my chat history to be saved, so that I can review past conversations and continue where I left off.

#### Acceptance Criteria

1. THE Server SHALL persist every chat message (both user and assistant messages) with the message ID, Chat_Session ID, role, content (up to 100,000 characters), and timestamp
2. WHEN a user loads a Chat_Session, THE Server SHALL return messages in chronological order with correct sequence numbering starting from 1
3. THE Server SHALL support Chat_Sessions containing up to 1000 messages without degradation in load time (under 500 milliseconds for full history retrieval)
4. WHEN the Server persists a message, THE Server SHALL assign a monotonically increasing sequence number within the Chat_Session starting at 1 to ensure correct ordering
5. IF the Server fails to persist a chat message, THEN THE Server SHALL return an error indication to the sending client and SHALL NOT relay the message to the Daemon until persistence is confirmed
6. IF a user sends a message with content exceeding 100,000 characters, THEN THE Server SHALL reject the message and return an error indicating the content length limit was exceeded

### Requirement 10: WebSocket Connection Management

**User Story:** As a user, I want the system to handle network interruptions gracefully, so that temporary connectivity issues do not lose my messages or break my session.

#### Acceptance Criteria

1. WHEN a client WebSocket_Connection is established, THE Server SHALL authenticate the connection using the user's session token within 5 seconds before accepting messages
2. IF the session token is invalid or expired during WebSocket_Connection authentication, THEN THE Server SHALL reject the connection with an authentication error indication and close the WebSocket_Connection
3. WHILE a client WebSocket_Connection is active, THE Server SHALL send a ping frame every 30 seconds and expect a pong response within 10 seconds
4. IF a client does not respond to a ping within 10 seconds, THEN THE Server SHALL close the WebSocket_Connection and mark the client as disconnected
5. WHEN a client reconnects after a disconnection, THE Server SHALL deliver any messages that were generated during the disconnection period in chronological order, up to a maximum of 100 buffered messages within a 5-minute disconnection window
6. THE Server SHALL support at least 10000 concurrent WebSocket_Connections (combining both client and Daemon connections) on a single server instance
7. WHEN the Server receives a malformed WebSocket message, THE Server SHALL log the error, discard the message, and maintain the connection; IF the Server receives more than 10 malformed messages from the same connection within 60 seconds, THEN THE Server SHALL close that WebSocket_Connection
