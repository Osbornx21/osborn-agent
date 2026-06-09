package agents

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrMissingDeviceID = errors.New("device id is required")
	ErrDeviceNotFound  = errors.New("device is not configured")
	ErrInvalidMode     = errors.New("agent mode is invalid")
)

type ModeStatus struct {
	DeviceID    string `json:"device_id"`
	DefaultMode Mode   `json:"default_mode"`
	ActiveMode  Mode   `json:"active_mode"`
	Override    bool   `json:"override"`
}

type ModeCatalog struct {
	DefaultMode    Mode         `json:"default_mode"`
	AvailableModes []Mode       `json:"available_modes"`
	Devices        []ModeStatus `json:"devices"`
}

type ModeController interface {
	ListModes(ctx context.Context) (ModeCatalog, error)
	GetDeviceMode(ctx context.Context, deviceID string) (ModeStatus, error)
	SetDeviceMode(ctx context.Context, deviceID string, mode Mode) (ModeStatus, error)
	ClearDeviceMode(ctx context.Context, deviceID string) (ModeStatus, error)
}

type ModeReader interface {
	GetDeviceMode(ctx context.Context, deviceID string) (ModeStatus, error)
}

type ModeStore struct {
	mu          sync.RWMutex
	defaultMode Mode
	devices     map[string]struct{}
	overrides   map[string]Mode
}

func NewModeStore(defaultMode Mode, deviceIDs []string) *ModeStore {
	defaultMode = normalizeMode(defaultMode)
	if !IsValidMode(defaultMode) {
		defaultMode = ModeCasual
	}
	devices := make(map[string]struct{}, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		deviceID = strings.TrimSpace(deviceID)
		if deviceID != "" {
			devices[deviceID] = struct{}{}
		}
	}
	return &ModeStore{
		defaultMode: defaultMode,
		devices:     devices,
		overrides:   make(map[string]Mode),
	}
}

func (s *ModeStore) GetDeviceMode(_ context.Context, deviceID string) (ModeStatus, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ModeStatus{}, ErrMissingDeviceID
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.deviceAllowed(deviceID) {
		return ModeStatus{}, ErrDeviceNotFound
	}
	mode, override := s.overrides[deviceID]
	if !override {
		mode = s.defaultMode
	}
	return ModeStatus{
		DeviceID:    deviceID,
		DefaultMode: s.defaultMode,
		ActiveMode:  mode,
		Override:    override,
	}, nil
}

func (s *ModeStore) ListModes(_ context.Context) (ModeCatalog, error) {
	if s == nil {
		return ModeCatalog{}, ErrDeviceNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	deviceIDs := make([]string, 0, len(s.devices))
	for deviceID := range s.devices {
		deviceIDs = append(deviceIDs, deviceID)
	}
	sort.Strings(deviceIDs)

	devices := make([]ModeStatus, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		mode, override := s.overrides[deviceID]
		if !override {
			mode = s.defaultMode
		}
		devices = append(devices, ModeStatus{
			DeviceID:    deviceID,
			DefaultMode: s.defaultMode,
			ActiveMode:  mode,
			Override:    override,
		})
	}

	return ModeCatalog{
		DefaultMode:    s.defaultMode,
		AvailableModes: []Mode{ModeCasual, ModeRoleplay, ModeProfessional, ModeTool},
		Devices:        devices,
	}, nil
}

func (s *ModeStore) SetDeviceMode(_ context.Context, deviceID string, mode Mode) (ModeStatus, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ModeStatus{}, ErrMissingDeviceID
	}
	mode = normalizeMode(mode)
	if !IsValidMode(mode) {
		return ModeStatus{}, ErrInvalidMode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.deviceAllowed(deviceID) {
		return ModeStatus{}, ErrDeviceNotFound
	}
	s.overrides[deviceID] = mode
	return ModeStatus{
		DeviceID:    deviceID,
		DefaultMode: s.defaultMode,
		ActiveMode:  mode,
		Override:    true,
	}, nil
}

func (s *ModeStore) ClearDeviceMode(_ context.Context, deviceID string) (ModeStatus, error) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ModeStatus{}, ErrMissingDeviceID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.deviceAllowed(deviceID) {
		return ModeStatus{}, ErrDeviceNotFound
	}
	delete(s.overrides, deviceID)
	return ModeStatus{
		DeviceID:    deviceID,
		DefaultMode: s.defaultMode,
		ActiveMode:  s.defaultMode,
		Override:    false,
	}, nil
}

func (s *ModeStore) deviceAllowed(deviceID string) bool {
	if s == nil {
		return false
	}
	if len(s.devices) == 0 {
		return true
	}
	_, ok := s.devices[deviceID]
	return ok
}

func IsValidMode(mode Mode) bool {
	switch normalizeMode(mode) {
	case ModeCasual, ModeRoleplay, ModeProfessional, ModeTool:
		return true
	default:
		return false
	}
}

func normalizeMode(mode Mode) Mode {
	return Mode(strings.ToLower(strings.TrimSpace(string(mode))))
}
