package fountain

import (
	"fmt"
	"os/exec"
	"sync"

	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/cleanup"
)

const (
	LowID  int32 = 0
	HighID int32 = 65535
)

// TapDevice represents a tap network device
type TapDevice struct {
	Name string
	ID   int32
}

// String implements the fmt.Stringer interface.
func (t *TapDevice) String() string {
	return fmt.Sprintf("TapDevice{Name: %s, ID: %d}", t.Name, t.ID)
}

type Fountain struct {
	bridgeDevice string
	mutex        sync.Mutex
	available    []int32 // Available tap IDs
	lowID        int32   // Lowest ID to allocate
	highID       int32   // Highest ID to allocate
}

func NewFountain(bridgeDevice string) *Fountain {
	f := &Fountain{
		bridgeDevice: bridgeDevice,
		lowID:        LowID,
		highID:       HighID,
		available:    make([]int32, 0, HighID-LowID+1),
	}

	for id := LowID; id <= HighID; id++ {
		f.available = append(f.available, id)
	}
	return f
}

// allocateTapID allocates a new tap device ID (internal use).
func (f *Fountain) allocateTapID() (int32, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if len(f.available) == 0 {
		return -1, fmt.Errorf("no available tap device IDs in range %d-%d", f.lowID, f.highID)
	}

	id := f.available[0]
	f.available = f.available[1:]
	return id, nil
}

// freeTapID returns a tap ID to the pool of available IDs (internal use).
func (f *Fountain) freeTapID(id int32) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if id < f.lowID || id > f.highID {
		return fmt.Errorf("tap ID %d is outside allocator range %d-%d", id, f.lowID, f.highID)
	}

	for _, i := range f.available {
		if i == id {
			return fmt.Errorf("tap ID %d is already free", id)
		}
	}
	f.available = append(f.available, id)
	return nil
}

// claimID attempts to claim a specific tap ID from the pool
// It returns an error if the ID is not available or outside the valid range
func (f *Fountain) claimID(id int32) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// Check if ID is in valid range
	if id < f.lowID || id > f.highID {
		return fmt.Errorf("tap ID %d is outside allocator range %d-%d", id, f.lowID, f.highID)
	}

	// Check if ID is available
	isAvailable := false
	for i, availableID := range f.available {
		if availableID == id {
			// Remove this ID from the available pool
			f.available = append(f.available[:i], f.available[i+1:]...)
			isAvailable = true
			break
		}
	}

	if !isAvailable {
		return fmt.Errorf("tap ID %d is not available", id)
	}

	return nil
}

// CreateTapDevice creates a new tap device with an auto-allocated ID and returns a TapDevice
// If id is provided, it will attempt to claim that specific ID instead of auto-allocating
func (f *Fountain) CreateTapDevice(id *int32) (*TapDevice, error) {
	logger := log.WithField("action", "CreateTapDevice")
	cleanup := cleanup.Make(func() {
		logger.Debug("createTapDevice cleanup")
	})
	defer cleanup.Clean()

	var allocatedID int32
	var err error
	if id != nil {
		if err := f.claimID(*id); err != nil {
			return nil, err
		}
		allocatedID = *id
	} else {
		// Use existing auto-allocation logic
		allocatedID, err = f.allocateTapID()
		if err != nil {
			return nil, fmt.Errorf("failed to allocate tap ID: %w", err)
		}
	}
	cleanup.Add(func() {
		if err := f.freeTapID(allocatedID); err != nil {
			logger.WithError(err).Errorf("failed to free tap ID %d during cleanup", allocatedID)
		}
	})

	deviceName := fmt.Sprintf("tap%d", allocatedID)
	if output, err := exec.Command(
		"ip", "tuntap", "add", "dev", deviceName, "mode", "tap",
	).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to create: %v: %s %w", deviceName, output, err)
	}

	if output, err := exec.Command(
		"ip", "l", "set", "dev", deviceName, "master", f.bridgeDevice,
	).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to add: %v to: %v: %s %w", deviceName, f.bridgeDevice, output, err)
	}

	if output, err := exec.Command(
		"ip", "l", "set", deviceName, "up",
	).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to up: %v: %s %w", deviceName, output, err)
	}

	cleanup.Release()
	return &TapDevice{
		Name: deviceName,
		ID:   allocatedID,
	}, nil
}

// DestroyTapDevice destroys a tap device and frees its ID.
func (f *Fountain) DestroyTapDevice(device *TapDevice) error {
	log.WithFields(log.Fields{
		"deviceName": device.Name,
		"deviceID":   device.ID,
	}).Info("destroy tap device")

	// Remove the tap device from the bridge
	if err := exec.Command("ip", "link", "set", device.Name, "nomaster").Run(); err != nil {
		return fmt.Errorf("failed to remove %v from bridge: %w", device.Name, err)
	}

	// Bring the tap device down
	if err := exec.Command("ip", "link", "set", device.Name, "down").Run(); err != nil {
		return fmt.Errorf("failed to bring down %v: %w", device.Name, err)
	}

	// Delete the tap device
	if err := exec.Command("ip", "tuntap", "del", "dev", device.Name, "mode", "tap").Run(); err != nil {
		return fmt.Errorf("failed to delete %v: %w", device.Name, err)
	}

	// Free the ID if it's valid
	if device.ID >= f.lowID && device.ID <= f.highID {
		// Ignore error from freeTapID as the device is already deleted
		_ = f.freeTapID(device.ID)
	}

	return nil
}
