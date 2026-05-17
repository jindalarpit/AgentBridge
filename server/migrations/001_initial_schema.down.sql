-- 001_initial_schema.down.sql
-- Reverse the initial schema migration (drop in reverse dependency order)

DROP TABLE IF EXISTS message_buffer;
DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chat_sessions;
DROP TABLE IF EXISTS runtimes;
DROP TABLE IF EXISTS daemons;
DROP TABLE IF EXISTS users;
