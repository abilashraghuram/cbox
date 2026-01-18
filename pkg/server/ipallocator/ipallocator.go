package ipallocator

import (
	"fmt"
	"net"
	"sync"
)

type IPAllocator struct {
	subnet    *net.IPNet
	available []net.IP
	mutex     sync.Mutex
}

func incrementIP(ip net.IP) net.IP {
	newIP := copyIP(ip)
	for j := len(newIP) - 1; j >= 0; j-- {
		newIP[j]++
		if newIP[j] > 0 {
			break
		}
	}
	return newIP
}

func copyIP(ip net.IP) net.IP {
	dup := make(net.IP, len(ip))
	copy(dup, ip)
	return dup
}

func NewIPAllocator(subnetCIDR string) (*IPAllocator, error) {
	_, subnet, err := net.ParseCIDR(subnetCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet CIDR: %v", err)
	}

	allocator := &IPAllocator{
		subnet:    subnet,
		available: []net.IP{},
	}

	// The first one will be reserved as the gateway. Start from x.x.x.2.
	ip := incrementIP(subnet.IP)

	// Generate all available IPs in the subnet
	for ip := incrementIP(ip); subnet.Contains(ip); ip = incrementIP(ip) {
		allocator.available = append(allocator.available, copyIP(ip))
	}

	return allocator, nil
}

func (a *IPAllocator) AllocateIP() (*net.IPNet, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if len(a.available) == 0 {
		return nil, fmt.Errorf("no available IPs")
	}

	ip := a.available[0]
	a.available = a.available[1:]

	return &net.IPNet{
		IP:   ip,
		Mask: a.subnet.Mask,
	}, nil
}

func (a *IPAllocator) FreeIP(ip net.IP) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if !a.subnet.Contains(ip) {
		return fmt.Errorf("IP %v is not in the subnet", ip)
	}

	a.available = append(a.available, copyIP(ip))
	return nil
}

// ClaimIP attempts to claim a specific IP address from the pool.
// Returns error if the IP is already allocated or not in the subnet.
func (a *IPAllocator) ClaimIP(ip net.IP) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if !a.subnet.Contains(ip) {
		return fmt.Errorf("IP %v is not in the subnet", ip)
	}

	for i, availIP := range a.available {
		if availIP.Equal(ip) {
			// Remove this IP from available pool
			a.available = append(a.available[:i], a.available[i+1:]...)
			break
		}
	}
	return nil
}
