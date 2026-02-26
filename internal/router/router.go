package router

import (
	"context"
	"fmt"
	"log"
	"nextgen-sip/internal/models"
	"sync"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// RoutingEngine handles the logic of where to send SIP requests
type RoutingEngine struct {
	registrar Registrar
	billing   BillingEngine
	mu        sync.RWMutex
	calls     map[string]*models.CallState
}

type Registrar interface {
	Lookup(uri string) (string, error)
	Register(uri string, contact string) error
}

type BillingEngine interface {
	CanCall(from string, to string) (bool, error)
	StartCall(from string, to string) (string, error)
	EndCall(sessionID string) error
}

func NewRoutingEngine(reg Registrar, bill BillingEngine) *RoutingEngine {
	return &RoutingEngine{
		registrar: reg,
		billing:   bill,
		calls:     make(map[string]*models.CallState),
	}
}

// Route handles an incoming SIP request and returns the destination or an error
func (e *RoutingEngine) Route(req *sip.Request) (string, error) {
	switch req.Method() {
	case sip.REGISTER:
		return e.handleRegister(req)
	case sip.INVITE:
		return e.handleInvite(req)
	case sip.BYE:
		return e.handleBye(req)
	default:
		return "", fmt.Errorf("method %s not supported by router", req.Method())
	}
}

func (e *RoutingEngine) handleRegister(req *sip.Request) (string, error) {
	from := req.From().Address.String()
	contact := req.Contact().Address.String()
	
	log.Printf("[Router] Registering %s at %s", from, contact)
	err := e.registrar.Register(from, contact)
	if err != nil {
		return "", err
	}
	return "Registered", nil
}

func (e *RoutingEngine) handleInvite(req *sip.Request) (string, error) {
	from := req.From().Address.String()
	to := req.To().Address.String()

	log.Printf("[Router] Routing INVITE from %s to %s", from, to)

	// 1. Check Billing
	canCall, err := e.billing.CanCall(from, to)
	if err != nil || !canCall {
		return "", fmt.Errorf("billing check failed: %v", err)
	}

	// 2. Lookup Destination
	dest, err := e.registrar.Lookup(to)
	if err != nil {
		// If not registered locally, try Carrier Routing logic (simplified here)
		log.Printf("[Router] User %s not registered, trying external carrier", to)
		return "gw.carrier.com:5060", nil
	}

	return dest, nil
}

func (e *RoutingEngine) handleBye(req *sip.Request) (string, error) {
	// Logic to stop billing and cleanup session
	log.Printf("[Router] Handling BYE")
	return "OK", nil
}
