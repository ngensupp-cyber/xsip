package billing

import (
	"fmt"
	"log"
	"sync"
)

type InMemoryBilling struct {
	mu       sync.RWMutex
	balances map[string]float64
}

func NewInMemoryBilling() *InMemoryBilling {
	return &InMemoryBilling{
		balances: make(map[string]float64),
	}
}

func (b *InMemoryBilling) SetBalance(user string, amount float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.balances[user] = amount
}

func (b *InMemoryBilling) CanCall(from string, to string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	
	balance, ok := b.balances[from]
	if !ok {
		// For demo, allow registration if not found, but real system would deny
		log.Printf("[Billing] User %s not found, denying call", from)
		return false, fmt.Errorf("insufficient funds or user not found")
	}

	if balance <= 0 {
		return false, nil
	}
	return true, nil
}

func (b *InMemoryBilling) Deduct(user string, amount float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	balance, ok := b.balances[user]
	if !ok || balance < amount {
		return fmt.Errorf("insufficient funds")
	}

	b.balances[user] -= amount
	return nil
}

func (b *InMemoryBilling) StartCall(from string, to string) (string, error) {
	log.Printf("[Billing] Starting call from %s to %s", from, to)
	return "session-id-123", nil
}

func (b *InMemoryBilling) EndCall(sessionID string) error {
	log.Printf("[Billing] Ending session %s", sessionID)
	return nil
}
