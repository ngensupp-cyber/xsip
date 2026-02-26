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


func (b *InMemoryBilling) normalizeURI(uri string) string {
	// Extracts the user part from sip:user@domain
	var user string
	_, err := fmt.Sscanf(uri, "sip:%s", &user)
	if err != nil {
		return uri
	}
	// Split by @ to get only the username
	parts := strings.Split(user, "@")
	return "sip:" + parts[0] + "@localhost"
}

func (b *InMemoryBilling) CanCall(from string, to string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	
	from = b.normalizeURI(from)
	balance, ok := b.balances[from]
	if !ok {
		log.Printf("[Billing] Normalized user %s not found, denying call", from)
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

	user = b.normalizeURI(user)
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
