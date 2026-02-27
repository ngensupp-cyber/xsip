package engine

import (
	"context"
	"log"
	"nextgen-sip/internal/firewall"
	"nextgen-sip/internal/router"
	"nextgen-sip/pkg/utils"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type SIPEngine struct {
	server *sipgo.Server
	client *sipgo.Client
	router *router.RoutingEngine
	cc     *CallControl
	fw     *firewall.Firewall
}

func NewSIPEngine(ua *sipgo.UserAgent, r *router.RoutingEngine, cc *CallControl, fw *firewall.Firewall) *SIPEngine {
	s, err := sipgo.NewServer(ua)
	if err != nil {
		log.Fatal(err)
	}
	c, err := sipgo.NewClient(ua)
	if err != nil {
		log.Fatal(err)
	}
	return &SIPEngine{
		server: s,
		client: c,
		router: r,
		cc:     cc,
		fw:     fw,
	}
}

func (e *SIPEngine) Start(ctx context.Context, network, addr string) error {
	e.server.OnInvite(e.onInvite)
	e.server.OnRegister(e.onRegister)
	e.server.OnBye(e.onBye)
	e.server.OnMessage(e.onMessage)
	e.server.OnOptions(e.onOptions)

	log.Printf("=== Carrier-Grade SIP Engine v3.0 ===")
	log.Printf("Listening on %s (%s)", addr, network)
	return e.server.ListenAndServe(ctx, network, addr)
}

// ─── REGISTER Handler ───────────────────────────────────────────────────────
func (e *SIPEngine) onRegister(req *sip.Request, tx sip.ServerTransaction) {
	ip := req.Source()
	if !e.fw.IsAllowed(ip) {
		log.Printf("[FIREWALL] Blocked REGISTER from %s", ip)
		return
	}

	result, err := e.router.Route(req)
	if err != nil {
		e.fw.RecordFailedAuth(ip)
		res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
		tx.Respond(res)
		return
	}

	log.Printf("[SIP] ✓ Registration OK: %s", result)
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	tx.Respond(res)
}

// ─── INVITE Handler ────────────────────────────────────────────────────────
func (e *SIPEngine) onInvite(req *sip.Request, tx sip.ServerTransaction) {
	ip := req.Source()
	if !e.fw.IsAllowed(ip) {
		utils.FirewallBlocks.Inc()
		log.Printf("[FIREWALL] Blocked INVITE from %s", ip)
		return
	}

	from := req.From().Address.String()
	to := req.To().Address.String()
	callID := req.CallID().Value()

	tenantID := "default"
	if h := req.GetHeader("X-Tenant-ID"); h != nil {
		tenantID = h.Value()
	}

	utils.SipRequestsTotal.WithLabelValues("INVITE", tenantID).Inc()

	// Route to find destination
	dest, err := e.router.Route(req)
	if err != nil {
		log.Printf("[SIP] INVITE routing failed: %v", err)
		res := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
		tx.Respond(res)
		return
	}

	// Track the call
	e.cc.StartCall(from, to, callID, tenantID)

	log.Printf("[PROXY] Relaying INVITE %s -> %s (dest: %s)", from, to, dest)

	// Parse destination and forward
	var destURI sip.Uri
	if err := sip.ParseUri(dest, &destURI); err != nil {
		log.Printf("[PROXY] Bad destination URI %s: %v", dest, err)
		res := sip.NewResponseFromRequest(req, 502, "Bad Gateway", nil)
		tx.Respond(res)
		return
	}

	proxyReq := req.Clone()
	proxyReq.Recipient = destURI

	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		log.Printf("[PROXY] Transaction error: %v", err)
		res := sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil)
		tx.Respond(res)
		return
	}

	// Relay responses back to caller
	go func() {
		defer clTx.Terminate()
		for {
			select {
			case res, ok := <-clTx.Responses():
				if !ok || res == nil {
					return
				}
				log.Printf("[PROXY] Relaying response %d for INVITE", res.StatusCode)
				tx.Respond(res)
				// Final response (>= 200) ends the transaction
				if res.StatusCode >= 200 {
					return
				}
			case <-tx.Done():
				return
			case <-clTx.Done():
				return
			}
		}
	}()
}

// ─── BYE Handler ───────────────────────────────────────────────────────────
func (e *SIPEngine) onBye(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	from := req.From().Address.String()
	to := req.To().Address.String()

	log.Printf("[SIP] BYE received: %s -> %s (CallID: %s)", from, to, callID)

	// End call tracking
	e.cc.EndCall(callID)

	// Try to relay BYE to the other party
	dest, err := e.router.Route(req)
	if err != nil {
		// If we can't route, just acknowledge
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(res)
		return
	}

	var destURI sip.Uri
	if err := sip.ParseUri(dest, &destURI); err != nil {
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(res)
		return
	}

	proxyReq := req.Clone()
	proxyReq.Recipient = destURI

	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(res)
		return
	}

	go func() {
		defer clTx.Terminate()
		select {
		case res, ok := <-clTx.Responses():
			if ok && res != nil {
				tx.Respond(res)
			}
		case <-tx.Done():
		case <-clTx.Done():
		}
	}()
}

// ─── MESSAGE Handler ───────────────────────────────────────────────────────
func (e *SIPEngine) onMessage(req *sip.Request, tx sip.ServerTransaction) {
	from := req.From().Address.String()
	to := req.To().Address.String()

	log.Printf("[SIP] MESSAGE: %s -> %s", from, to)

	dest, err := e.router.Route(req)
	if err != nil {
		log.Printf("[SIP] MESSAGE routing failed: %v", err)
		res := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
		tx.Respond(res)
		return
	}

	var destURI sip.Uri
	if err := sip.ParseUri(dest, &destURI); err != nil {
		res := sip.NewResponseFromRequest(req, 502, "Bad Gateway", nil)
		tx.Respond(res)
		return
	}

	proxyReq := req.Clone()
	proxyReq.Recipient = destURI

	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		log.Printf("[PROXY] MESSAGE relay error: %v", err)
		res := sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil)
		tx.Respond(res)
		return
	}

	go func() {
		defer clTx.Terminate()
		select {
		case res, ok := <-clTx.Responses():
			if ok && res != nil {
				tx.Respond(res)
			} else {
				r := sip.NewResponseFromRequest(req, 408, "Request Timeout", nil)
				tx.Respond(r)
			}
		case <-tx.Done():
		case <-clTx.Done():
		}
	}()
}

// ─── OPTIONS Handler ───────────────────────────────────────────────────────
func (e *SIPEngine) onOptions(req *sip.Request, tx sip.ServerTransaction) {
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	tx.Respond(res)
}
