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

// normalizeURI extracts user part for consistent lookups
func normalizeURI(uri string) string {
	// sip:055@some.domain.com -> 055
	uri = strings.TrimPrefix(uri, "sip:")
	parts := strings.SplitN(uri, "@", 2)
	if len(parts) >= 1 {
		return parts[0]
	}
	return uri
}

// Route handles an incoming SIP request and returns the destination or an error
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

	// Check billing for INVITE and MESSAGE
	if req.Method == sip.INVITE || req.Method == sip.MESSAGE {
		canCall, err := e.billing.CanCall(from, to)
		if err != nil || !canCall {
			return "", fmt.Errorf("billing check failed for %s: %v", from, err)
		}
	}

	// Normalize the To URI for lookup
	userPart := normalizeURI(to)

	// Try all possible key formats for lookup
	lookupKeys := []string{
		to,                                          // exact match
		fmt.Sprintf("sip:%s@localhost", userPart),  // normalized
	}

	for _, key := range lookupKeys {
		dest, err := e.registrar.Lookup(key)
		if err == nil {
			log.Printf("[Router] Found destination for %s: %s", userPart, dest)
			return dest, nil
		}
	}

	return "", fmt.Errorf("user %s not registered", to)
}

func (e *RoutingEngine) handleRegister(req *sip.Request) (string, error) {
	from := req.From().Address.String()
	contact := req.Contact().Address.String()
	source := req.Source()

	// We register under multiple keys for flexible lookup
	userPart := normalizeURI(from)
	normalizedURI := fmt.Sprintf("sip:%s@localhost", userPart)

	// Store as full SIP URI pointing to the real source address
	destValue := fmt.Sprintf("sip:%s;transport=tcp", source)

	log.Printf("[Router] Registering user=%s contact=%s source=%s", from, contact, source)

	// Register under both the original URI and normalized URI
	e.registrar.Register(from, destValue)
	e.registrar.Register(normalizedURI, destValue)

	return "Registered", nil
}
