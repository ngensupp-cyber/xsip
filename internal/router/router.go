package router

import (
	"fmt"
	"log"
	"strings"

	"github.com/emiago/sipgo/sip"
)

// RoutingEngine handles the logic of where to send SIP requests
type RoutingEngine struct {
	registrar Registrar
	billing   BillingEngine
}

type Registrar interface {
	Lookup(uri string) (string, error)
	Register(uri string, contact string) error
}

type BillingEngine interface {
	CanCall(from string, to string) (bool, error)
}

func NewRoutingEngine(reg Registrar, bill BillingEngine) *RoutingEngine {
	return &RoutingEngine{
		registrar: reg,
		billing:   bill,
	}
}

// ─── Phone Number Normalization ─────────────────────────────
// Strips sip: prefix, @domain, country codes, and + sign
// to get the raw local number for flexible lookup
func extractUser(uri string) string {
	s := uri
	// Remove sip: / sips: prefix
	s = strings.TrimPrefix(s, "sip:")
	s = strings.TrimPrefix(s, "sips:")
	// Remove @domain
	if idx := strings.Index(s, "@"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// stripDialPrefix removes +, country codes, and common prefixes
// to normalize "how people dial" vs "how they registered"
func stripDialPrefix(number string) string {
	s := number
	// Remove leading +
	s = strings.TrimPrefix(s, "+")
	// Common country code prefixes to strip
	prefixes := []string{
		"972", "971", "970", "966", "965", "964", "963", "962", "961",
		"20", "90", "44", "1", "49", "33", "86",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) && len(s) > len(p)+3 {
			stripped := s[len(p):]
			// Remove leading 0 after country code
			stripped = strings.TrimPrefix(stripped, "0")
			return stripped
		}
	}
	// Remove leading 0
	s = strings.TrimPrefix(s, "0")
	return s
}

// generateLookupKeys creates all possible registration keys from a dialed URI
func generateLookupKeys(uri string) []string {
	user := extractUser(uri)
	stripped := stripDialPrefix(user)

	// Extract domain for full URI matching
	domain := ""
	if idx := strings.Index(uri, "@"); idx >= 0 {
		raw := strings.TrimPrefix(uri, "sip:")
		raw = strings.TrimPrefix(raw, "sips:")
		if idx2 := strings.Index(raw, "@"); idx2 >= 0 {
			domain = raw[idx2+1:]
		}
	}

	keys := []string{
		uri, // exact: sip:+972056233121@domain.com
	}

	if domain != "" {
		keys = append(keys,
			fmt.Sprintf("sip:%s@%s", user, domain),      // sip:+972056233121@domain
			fmt.Sprintf("sip:%s@%s", stripped, domain),   // sip:56233121@domain
			fmt.Sprintf("sip:0%s@%s", stripped, domain),  // sip:056233121@domain
		)
	}

	keys = append(keys,
		fmt.Sprintf("sip:%s@localhost", user),            // sip:+972056233121@localhost
		fmt.Sprintf("sip:%s@localhost", stripped),         // sip:56233121@localhost
		fmt.Sprintf("sip:0%s@localhost", stripped),        // sip:056233121@localhost
	)

	return keys
}

// ─── Route ──────────────────────────────────────────────────
func (e *RoutingEngine) Route(req *sip.Request) (string, error) {
	if req.Method == sip.REGISTER {
		return e.handleRegister(req)
	}
	return e.handleGenericRoute(req)
}

func (e *RoutingEngine) handleGenericRoute(req *sip.Request) (string, error) {
	from := req.From().Address.String()
	to := req.To().Address.String()

	log.Printf("[Router] Routing %s: %s -> %s", req.Method, from, to)

	// Billing check (only for INVITE and MESSAGE)
	if req.Method == sip.INVITE || req.Method == sip.MESSAGE {
		canCall, err := e.billing.CanCall(from, to)
		if err != nil {
			log.Printf("[Router] Billing check error (allowing anyway): %v", err)
			// Don't block — treat billing errors as permissive
		} else if !canCall {
			return "", fmt.Errorf("insufficient balance for %s", from)
		}
	}

	// Try all possible lookup keys for the destination
	lookupKeys := generateLookupKeys(to)
	for _, key := range lookupKeys {
		dest, err := e.registrar.Lookup(key)
		if err == nil {
			log.Printf("[Router] ✓ Found %s via key: %s => %s", to, key, dest)
			return dest, nil
		}
	}

	log.Printf("[Router] ✗ No registration found for %s (tried %d keys)", to, len(lookupKeys))
	return "", fmt.Errorf("user %s not registered", to)
}

func (e *RoutingEngine) handleRegister(req *sip.Request) (string, error) {
	from := req.From().Address.String()
	contact := req.Contact().Address.String()
	source := req.Source()

	user := extractUser(from)
	stripped := stripDialPrefix(user)
	destValue := source

	// Extract domain
	domain := ""
	raw := strings.TrimPrefix(from, "sip:")
	if idx := strings.Index(raw, "@"); idx >= 0 {
		domain = raw[idx+1:]
	}

	log.Printf("[Router] Registering user=%s contact=%s source=%s", from, contact, source)

	// Register under MANY keys so we can find this user no matter how they're dialed
	keysToRegister := []string{
		from, // original: sip:055@domain.com
		fmt.Sprintf("sip:%s@localhost", user),       // sip:055@localhost
		fmt.Sprintf("sip:%s@localhost", stripped),    // sip:55@localhost  (stripped)
	}

	if domain != "" {
		keysToRegister = append(keysToRegister,
			fmt.Sprintf("sip:%s@%s", stripped, domain),   // sip:55@domain
			fmt.Sprintf("sip:0%s@%s", stripped, domain),  // sip:055@domain (with leading 0)
			fmt.Sprintf("sip:0%s@localhost", stripped),    // sip:055@localhost (with leading 0)
		)
	}

	for _, key := range keysToRegister {
		e.registrar.Register(key, destValue)
	}

	return "Registered", nil
}
