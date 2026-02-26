package firewall

import (
	"log"
	"sync"
)

// Firewall handles IP blacklisting and brute-force protection
type Firewall struct {
	mu           sync.RWMutex
	blacklisted  map[string]bool
	failedAuths  map[string]int
}

func NewFirewall() *Firewall {
	return &Firewall{
		blacklisted: make(map[string]bool),
		failedAuths: make(map[string]int),
	}
}

func (f *Firewall) IsAllowed(ip string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return !f.blacklisted[ip]
}

func (f *Firewall) RecordFailedAuth(ip string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failedAuths[ip]++
	if f.failedAuths[ip] >= 5 {
		f.blacklisted[ip] = true
		log.Printf("[Firewall] !!! IP %s blockaded after 5 failed attempts !!!", ip)
	}
}

func (f *Firewall) GetBlacklist() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	list := make([]string, 0, len(f.blacklisted))
	for ip := range f.blacklisted {
		list = append(list, ip)
	}
	return list
}
