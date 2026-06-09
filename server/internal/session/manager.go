package session

import (
	"fmt"
	"sync"
)

type Manager struct {
	mu             sync.Mutex
	nextSequence   int64
	byID           map[string]*Session
	activeByDevice map[string]*Session
}

func NewManager() *Manager {
	return &Manager{
		nextSequence:   1,
		byID:           map[string]*Session{},
		activeByDevice: map[string]*Session{},
	}
}

func (m *Manager) CreateSession(deviceID string, clientID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	firstGeneration := int64(1)
	if previous, ok := m.activeByDevice[deviceID]; ok {
		firstGeneration = previous.CurrentGeneration() + 1
		if firstGeneration < 1 {
			firstGeneration = 1
		}
		previous.Close()
	}

	id := fmt.Sprintf("sess_%d", m.nextSequence)
	m.nextSequence++

	session := newWithFirstGeneration(id, deviceID, clientID, firstGeneration)
	m.byID[id] = session
	m.activeByDevice[deviceID] = session
	return session
}

func (m *Manager) GetSession(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.byID[id]
	return session, ok
}

func (m *Manager) ActiveSessionForDevice(deviceID string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.activeByDevice[deviceID]
	return session, ok
}

func (m *Manager) CloseSession(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.byID[id]
	if !ok {
		return false
	}
	session.Close()
	if active, ok := m.activeByDevice[session.DeviceID()]; ok && active == session {
		delete(m.activeByDevice, session.DeviceID())
	}
	delete(m.byID, id)
	return true
}
