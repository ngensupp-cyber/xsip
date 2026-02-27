package engine

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	e.server.OnAck(e.onAck)

	log.Printf("=== XSIP Carrier Engine v5.0 ===")
	log.Printf("Listening on %s (%s)", addr, network)
	return e.server.ListenAndServe(ctx, network, addr)
}

// ─── Build proxy request (no Via pollution) ───────────────────────
func buildProxyRequest(method sip.RequestMethod, destURI sip.Uri, original *sip.Request) *sip.Request {
	newReq := sip.NewRequest(method, destURI)

	sip.CopyHeaders("From", original, newReq)
	sip.CopyHeaders("To", original, newReq)
	sip.CopyHeaders("Call-ID", original, newReq)
	sip.CopyHeaders("CSeq", original, newReq)
	sip.CopyHeaders("Contact", original, newReq)
	sip.CopyHeaders("Max-Forwards", original, newReq)
	sip.CopyHeaders("Content-Type", original, newReq)

	if original.Body() != nil {
		newReq.SetBody(original.Body())
	}

	return newReq
}

// ─── Parse destination URI ────────────────────────────────────────
func parseDestination(dest string) (sip.Uri, error) {
	var uri sip.Uri

	raw := strings.TrimPrefix(dest, "sip:")
	raw = strings.TrimPrefix(raw, "sips:")

	hostPort := raw
	if idx := strings.Index(raw, ";"); idx >= 0 {
		hostPort = raw[:idx]
	}

	host := hostPort
	port := 0
	if idx := strings.LastIndex(hostPort, ":"); idx >= 0 {
		host = hostPort[:idx]
		fmt.Sscanf(hostPort[idx+1:], "%d", &port)
	}

	uri.Host = host
	if port > 0 {
		uri.Port = port
	}
	uri.UriParams = sip.NewParams()
	uri.UriParams.Add("transport", "tcp")

	return uri, nil
}

// ─── REGISTER ─────────────────────────────────────────────────────
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

// ─── INVITE (BLOCKING — keeps server tx alive) ───────────────────
func (e *SIPEngine) onInvite(req *sip.Request, tx sip.ServerTransaction) {
	ip := req.Source()
	if !e.fw.IsAllowed(ip) {
		utils.FirewallBlocks.Inc()
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

	log.Printf("[INVITE] %s -> %s (CallID: %s)", from, to, callID)

	// Send 100 Trying
	trying := sip.NewResponseFromRequest(req, 100, "Trying", nil)
	tx.Respond(trying)

	// Route
	dest, err := e.router.Route(req)
	if err != nil {
		log.Printf("[INVITE] ✗ Routing failed: %v", err)
		res := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
		tx.Respond(res)
		return
	}
	log.Printf("[INVITE] ✓ Destination: %s", dest)

	// Track call
	e.cc.StartCall(from, to, callID, tenantID)

	// Build destination URI
	destURI, err := parseDestination(dest)
	if err != nil {
		res := sip.NewResponseFromRequest(req, 502, "Bad Gateway", nil)
		tx.Respond(res)
		return
	}

	// Build and send proxy request
	proxyReq := buildProxyRequest(sip.INVITE, destURI, req)
	log.Printf("[INVITE] → Sending to %s:%d", destURI.Host, destURI.Port)

	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		log.Printf("[INVITE] ✗ Send failed: %v", err)
		res := sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil)
		tx.Respond(res)
		return
	}
	log.Printf("[INVITE] ✓ Sent! Blocking until response...")

	// ★ BLOCK HERE — do NOT return from handler until final response
	// This keeps the server transaction alive so we can relay responses
	defer clTx.Terminate()
	for {
		select {
		case res, ok := <-clTx.Responses():
			if !ok || res == nil {
				log.Printf("[INVITE] ✗ Response channel closed")
				return
			}
			log.Printf("[INVITE] ← %d %s", res.StatusCode, res.Reason)

			// Build a new response for the original transaction
			relay := sip.NewResponseFromRequest(req, res.StatusCode, res.Reason, res.Body())
			tx.Respond(relay)

			if res.StatusCode >= 200 {
				log.Printf("[INVITE] ✓ Call established! Final=%d", res.StatusCode)
				return
			}
		case <-clTx.Done():
			log.Printf("[INVITE] Client tx done for %s", callID)
			return
		}
	}
}

// ─── BYE (BLOCKING) ──────────────────────────────────────────────
func (e *SIPEngine) onBye(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	log.Printf("[BYE] CallID: %s", callID)
	e.cc.EndCall(callID)

	dest, err := e.router.Route(req)
	if err != nil {
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(res)
		return
	}

	destURI, err := parseDestination(dest)
	if err != nil {
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(res)
		return
	}

	proxyReq := buildProxyRequest(sip.BYE, destURI, req)

	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		log.Printf("[BYE] Relay failed: %v", err)
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(res)
		return
	}

	// Block until response
	defer clTx.Terminate()
	select {
	case res, ok := <-clTx.Responses():
		if ok && res != nil {
			log.Printf("[BYE] ← %d", res.StatusCode)
			relay := sip.NewResponseFromRequest(req, res.StatusCode, res.Reason, nil)
			tx.Respond(relay)
		} else {
			res := sip.NewResponseFromRequest(req, 200, "OK", nil)
			tx.Respond(res)
		}
	case <-clTx.Done():
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(res)
	}
}

// ─── MESSAGE (BLOCKING) ──────────────────────────────────────────
func (e *SIPEngine) onMessage(req *sip.Request, tx sip.ServerTransaction) {
	from := req.From().Address.String()
	to := req.To().Address.String()
	log.Printf("[MESSAGE] %s -> %s", from, to)

	dest, err := e.router.Route(req)
	if err != nil {
		log.Printf("[MESSAGE] ✗ Routing failed: %v", err)
		res := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
		tx.Respond(res)
		return
	}
	log.Printf("[MESSAGE] ✓ Destination: %s", dest)

	destURI, err := parseDestination(dest)
	if err != nil {
		res := sip.NewResponseFromRequest(req, 502, "Bad Gateway", nil)
		tx.Respond(res)
		return
	}

	proxyReq := buildProxyRequest(sip.MESSAGE, destURI, req)

	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		log.Printf("[MESSAGE] ✗ Send failed: %v", err)
		res := sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil)
		tx.Respond(res)
		return
	}
	log.Printf("[MESSAGE] ✓ Sent, blocking...")

	// Block until response
	defer clTx.Terminate()
	select {
	case res, ok := <-clTx.Responses():
		if ok && res != nil {
			log.Printf("[MESSAGE] ← %d %s", res.StatusCode, res.Reason)
			relay := sip.NewResponseFromRequest(req, res.StatusCode, res.Reason, nil)
			tx.Respond(relay)
		} else {
			r := sip.NewResponseFromRequest(req, 408, "Request Timeout", nil)
			tx.Respond(r)
		}
	case <-clTx.Done():
		log.Printf("[MESSAGE] Client tx done")
	}
}

// ─── ACK ──────────────────────────────────────────────────────────
func (e *SIPEngine) onAck(req *sip.Request, tx sip.ServerTransaction) {
	log.Printf("[ACK] CallID: %s", req.CallID().Value())

	dest, err := e.router.Route(req)
	if err != nil {
		return
	}

	destURI, err := parseDestination(dest)
	if err != nil {
		return
	}

	proxyReq := buildProxyRequest(sip.ACK, destURI, req)

	ctx := context.Background()
	e.client.TransactionRequest(ctx, proxyReq)
}

// ─── OPTIONS ──────────────────────────────────────────────────────
func (e *SIPEngine) onOptions(req *sip.Request, tx sip.ServerTransaction) {
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	tx.Respond(res)
}
