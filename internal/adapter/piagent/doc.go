// Package piagent implements an adapter for standalone Pi Agent sessions.
//
// Pi Agent stores sessions in ~/.pi/agent/sessions/<encoded-path>/ where
// encoded-path is the project path with slashes replaced by dashes, wrapped
// in double dashes (e.g., /home/user/project → --home-user-project--).
//
// This differs from the openclaw-embedded Pi which uses a flat global directory.
// The JSONL format is identical to the pi package.
package piagent
