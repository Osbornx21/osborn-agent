package agents

import (
	"context"
	"errors"
	"testing"
)

func TestModeStoreTracksDeviceOverrides(t *testing.T) {
	store := NewModeStore(ModeCasual, []string{"stackchan-s3-main"})

	status, err := store.GetDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("GetDeviceMode() error = %v", err)
	}
	if status.ActiveMode != ModeCasual || status.DefaultMode != ModeCasual || status.Override {
		t.Fatalf("initial status = %+v, want casual default without override", status)
	}

	status, err = store.SetDeviceMode(context.Background(), "stackchan-s3-main", ModeProfessional)
	if err != nil {
		t.Fatalf("SetDeviceMode() error = %v", err)
	}
	if status.ActiveMode != ModeProfessional || !status.Override {
		t.Fatalf("set status = %+v, want professional override", status)
	}

	status, err = store.ClearDeviceMode(context.Background(), "stackchan-s3-main")
	if err != nil {
		t.Fatalf("ClearDeviceMode() error = %v", err)
	}
	if status.ActiveMode != ModeCasual || status.Override {
		t.Fatalf("cleared status = %+v, want default casual without override", status)
	}
}

func TestModeStoreRejectsUnknownDeviceAndMode(t *testing.T) {
	store := NewModeStore(ModeCasual, []string{"stackchan-s3-main"})

	if _, err := store.GetDeviceMode(context.Background(), "missing-device"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("unknown device error = %v, want ErrDeviceNotFound", err)
	}
	if _, err := store.SetDeviceMode(context.Background(), "stackchan-s3-main", Mode("root")); !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidMode", err)
	}
}

func TestModeStoreListsAvailableModesAndDeviceStatuses(t *testing.T) {
	store := NewModeStore(ModeRoleplay, []string{"stackchan-s3-main", "desk-secondary"})
	if _, err := store.SetDeviceMode(context.Background(), "stackchan-s3-main", ModeTool); err != nil {
		t.Fatalf("SetDeviceMode() error = %v", err)
	}

	catalog, err := store.ListModes(context.Background())
	if err != nil {
		t.Fatalf("ListModes() error = %v", err)
	}

	if catalog.DefaultMode != ModeRoleplay {
		t.Fatalf("default mode = %q, want roleplay", catalog.DefaultMode)
	}
	if len(catalog.AvailableModes) != 4 ||
		catalog.AvailableModes[0] != ModeCasual ||
		catalog.AvailableModes[1] != ModeRoleplay ||
		catalog.AvailableModes[2] != ModeProfessional ||
		catalog.AvailableModes[3] != ModeTool {
		t.Fatalf("available modes = %+v, want stable mode list", catalog.AvailableModes)
	}
	if len(catalog.Devices) != 2 {
		t.Fatalf("devices = %+v, want two configured devices", catalog.Devices)
	}
	if catalog.Devices[0].DeviceID != "desk-secondary" || catalog.Devices[0].ActiveMode != ModeRoleplay || catalog.Devices[0].Override {
		t.Fatalf("first device status = %+v, want sorted default roleplay", catalog.Devices[0])
	}
	if catalog.Devices[1].DeviceID != "stackchan-s3-main" || catalog.Devices[1].ActiveMode != ModeTool || !catalog.Devices[1].Override {
		t.Fatalf("second device status = %+v, want sorted tool override", catalog.Devices[1])
	}
}
