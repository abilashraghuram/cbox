package cidallocator

import (
	"fmt"
	"sync"
)

// CIDAllocator manages allocation of Context IDs (CIDs) for VMs
type CIDAllocator struct {
	lowCID    uint32
	highCID   uint32
	available []uint32
	mutex     sync.Mutex
}

// NewCIDAllocator creates a new CID allocator for the given CID range
func NewCIDAllocator(lowCID, highCID uint32) (*CIDAllocator, error) {
	if lowCID < 3 || highCID > 0xFFFFFFFF || lowCID > highCID {
		return nil, fmt.Errorf("invalid CID range: %d-%d", lowCID, highCID)
	}

	allocator := &CIDAllocator{
		lowCID:    lowCID,
		highCID:   highCID,
		available: make([]uint32, 0, highCID-lowCID+1),
	}

	// Initialize available CIDs
	for cid := lowCID; cid <= highCID; cid++ {
		allocator.available = append(allocator.available, cid)
	}

	return allocator, nil
}

// AllocateCID allocates and returns the next available CID
func (a *CIDAllocator) AllocateCID() (uint32, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if len(a.available) == 0 {
		return 0, fmt.Errorf("no available CIDs in range %d-%d", a.lowCID, a.highCID)
	}

	// Take the first available CID
	cid := a.available[0]
	a.available = a.available[1:]

	return cid, nil
}

// FreeCID returns a CID to the pool of available CIDs
func (a *CIDAllocator) FreeCID(cid uint32) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if cid < a.lowCID || cid > a.highCID {
		return fmt.Errorf("CID %d is outside allocator range %d-%d", cid, a.lowCID, a.highCID)
	}

	// Check if CID is already in available pool
	for _, c := range a.available {
		if c == cid {
			return fmt.Errorf("CID %d is already free", cid)
		}
	}

	a.available = append(a.available, cid)
	return nil
}

// ClaimCID claims a specific CID from the pool of available CIDs
func (a *CIDAllocator) ClaimCID(cid uint32) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	for i, c := range a.available {
		if c == cid {
			a.available = append(a.available[:i], a.available[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("CID %d is not available", cid)
} 