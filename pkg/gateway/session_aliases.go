package gateway

import "github.com/anyclaw/anyclaw/pkg/sessionstore"

type Session = sessionstore.Session
type SessionMessage = sessionstore.SessionMessage
type SessionManager = sessionstore.SessionManager
type SessionAgent = sessionstore.SessionAgent
type SessionCreateOptions = sessionstore.SessionCreateOptions
type SessionPatchOptions = sessionstore.SessionPatchOptions

var NewSessionManager = sessionstore.NewSessionManager

func cloneSession(session *Session) *Session {
	return sessionstore.CloneSession(session)
}

func normalizeParticipants(primary string, participants []string) []string {
	return sessionstore.NormalizeParticipants(primary, participants)
}

func shortenTitle(input string) string {
	return sessionstore.ShortenTitle(input)
}
