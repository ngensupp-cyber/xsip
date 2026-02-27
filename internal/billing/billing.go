package billing

import (
	"fmt"
	"log"
	"sync"
	"strings"
	"nextgen-sip/internal/models"
)

type InMemoryBilling struct {
	mu       sync.RWMutex
	users    map[string]models.User
	balances map[string]float64
}

func NewInMemoryBilling() *InMemoryBilling {
	return &InMemoryBilling{
		users:    make(map[string]models.User),
		balances: make(map[string]float64),
	}
}

func (b *InMemoryBilling) SetBalance(user string, amount float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.balances[user] = amount
}

func (b *InMemoryBilling) SaveUser(u models.User) {
	b.mu.Lock()
	defer b.mu.Unlock()
	uri := "sip:" + u.ID + "@localhost"
	b.users[uri] = u
	b.balances[uri] = u.Balance
}

func (b *InMemoryBilling) ListUsers() ([]models.User, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	list := make([]models.User, 0, len(b.users))
	for _, u := range b.users {
		list = append(list, u)
	}
	return list, nil
}

func (b *InMemoryBilling) DeleteUser(uri string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.users, uri)
	delete(b.balances, uri)
}

// normalizeURI extracts the user part for flexible billing lookup
func (b *InMemoryBilling) normalizeURI(uri string) string {
	s := strings.TrimPrefix(uri, "sip:")
	s = strings.TrimPrefix(s, "sips:")
	parts := strings.Split(s, "@")
	user := parts[0]
	// Strip + and country codes
	user = strings.TrimPrefix(user, "+")
	return "sip:" + user + "@localhost"
}

func (b *InMemoryBilling) CanCall(from string, to string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	normalized := b.normalizeURI(from)
	balance, ok := b.balances[normalized]
	if !ok {
		// User not in billing system — ALLOW the call by default
		// This is critical: users registered via Linphone won't be in billing
		log.Printf("[Billing] User %s not in billing, allowing call", from)
		return true, nil
	}

	if balance <= 0 {
		log.Printf("[Billing] User %s has no balance ($%.2f), denying", from, balance)
		return false, nil
	}
	return true, nil
}

func (b *InMemoryBilling) Deduct(user string, amount float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	normalized := b.normalizeURI(user)
	balance, ok := b.balances[normalized]
	if !ok {
		return nil // User not in billing — skip deduction
	}
	if balance < amount {
		return fmt.Errorf("insufficient funds")
	}

	b.balances[normalized] -= amount
	return nil
}

func (b *InMemoryBilling) StartCall(from string, to string) (string, error) {
	return "session", nil
}

func (b *InMemoryBilling) EndCall(sessionID string) error {
	return nil
}
