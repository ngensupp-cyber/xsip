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

	log.Printf("=== XSIP Carrier Engine v4.1 ===")
	log.Printf("Listening on %s (%s)", addr, network)
	return e.server.ListenAndServe(ctx, network, addr)
}

// parseDestination takes a stored destination like "sip:100.64.0.3:16412;transport=tcp"
// and returns a properly constructed sip.Uri
func parseDestination(dest string) (sip.Uri, error) {
	var uri sip.Uri

	// Remove sip: prefix for manual parsing
	raw := strings.TrimPrefix(dest, "sip:")
	raw = strings.TrimPrefix(raw, "sips:")

	// Split off params (;transport=tcp etc)
	hostPort := raw
	if idx := strings.Index(raw, ";"); idx >= 0 {
		hostPort = raw[:idx]
	}

	// Split host:port
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

// ─── INVITE ───────────────────────────────────────────────────────
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

	// Step 1: Send 100 Trying immediately
	trying := sip.NewResponseFromRequest(req, 100, "Trying", nil)
	tx.Respond(trying)
	log.Printf("[INVITE] Sent 100 Trying to caller")

	// Step 2: Route to find destination
	dest, err := e.router.Route(req)
	if err != nil {
		log.Printf("[INVITE] ✗ Routing failed: %v", err)
		res := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
		tx.Respond(res)
		return
	}
	log.Printf("[INVITE] ✓ Destination resolved: %s", dest)

	// Step 3: Track the call
	e.cc.StartCall(from, to, callID, tenantID)

	// Step 4: Build destination URI
	destURI, err := parseDestination(dest)
	if err != nil {
		log.Printf("[INVITE] ✗ URI parse error: %v", err)
		res := sip.NewResponseFromRequest(req, 502, "Bad Gateway", nil)
		tx.Respond(res)
		return
	}
	log.Printf("[INVITE] Relay target: host=%s port=%d", destURI.Host, destURI.Port)

	// Step 5: Clone request and set destination
	proxyReq := req.Clone()
	proxyReq.Recipient = destURI

	// Step 6: Send via client transaction
	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		log.Printf("[INVITE] ✗ Transaction failed: %v", err)
		res := sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil)
		tx.Respond(res)
		return
	}
	log.Printf("[INVITE] ✓ Client transaction created, waiting for responses...")

	// Step 7: Relay responses back
	go func() {
		defer clTx.Terminate()
		for {
			select {
			case res, ok := <-clTx.Responses():
				if !ok || res == nil {
					log.Printf("[INVITE] Response channel closed for %s", callID)
					return
				}
				log.Printf("[INVITE] ← Response %d %s for %s", res.StatusCode, res.Reason, callID)
				tx.Respond(res)
				if res.StatusCode >= 200 {
					log.Printf("[INVITE] Final response %d relayed, done", res.StatusCode)
					return
				}
			case <-tx.Done():
				log.Printf("[INVITE] Server transaction done for %s", callID)
				return
			case <-clTx.Done():
				log.Printf("[INVITE] Client transaction done for %s", callID)
				return
			}
		}
	}()
}

// ─── BYE ──────────────────────────────────────────────────────────
func (e *SIPEngine) onBye(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	from := req.From().Address.String()
	to := req.To().Address.String()

	log.Printf("[BYE] %s -> %s (CallID: %s)", from, to, callID)
	e.cc.EndCall(callID)

	dest, err := e.router.Route(req)
	if err != nil {
		log.Printf("[BYE] No route, sending local 200 OK")
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

	proxyReq := req.Clone()
	proxyReq.Recipient = destURI

	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		log.Printf("[BYE] Relay failed: %v, sending local 200 OK", err)
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(res)
		return
	}

	go func() {
		defer clTx.Terminate()
		select {
		case res, ok := <-clTx.Responses():
			if ok && res != nil {
				log.Printf("[BYE] ← Response %d relayed", res.StatusCode)
				tx.Respond(res)
			}
		case <-tx.Done():
		case <-clTx.Done():
		}
	}()
}

// ─── MESSAGE ──────────────────────────────────────────────────────
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

	proxyReq := req.Clone()
	proxyReq.Recipient = destURI

	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		log.Printf("[MESSAGE] ✗ Relay failed: %v", err)
		res := sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil)
		tx.Respond(res)
		return
	}
	log.Printf("[MESSAGE] ✓ Transaction created, waiting for response...")

	go func() {
		defer clTx.Terminate()
		select {
		case res, ok := <-clTx.Responses():
			if ok && res != nil {
				log.Printf("[MESSAGE] ← Response %d relayed", res.StatusCode)
				tx.Respond(res)
			} else {
				log.Printf("[MESSAGE] ✗ No response, sending 408")
				r := sip.NewResponseFromRequest(req, 408, "Request Timeout", nil)
				tx.Respond(r)
			}
		case <-tx.Done():
			log.Printf("[MESSAGE] Server tx done")
		case <-clTx.Done():
			log.Printf("[MESSAGE] Client tx done")
		}
	}()
}

// ─── ACK ──────────────────────────────────────────────────────────
func (e *SIPEngine) onAck(req *sip.Request, tx sip.ServerTransaction) {
	log.Printf("[ACK] Received for CallID: %s", req.CallID().Value())

	dest, err := e.router.Route(req)
	if err != nil {
		return
	}

	destURI, err := parseDestination(dest)
	if err != nil {
		return
	}

	proxyReq := req.Clone()
	proxyReq.Recipient = destURI

	ctx := context.Background()
	e.client.TransactionRequest(ctx, proxyReq)
	log.Printf("[ACK] Relayed to %s:%d", destURI.Host, destURI.Port)
}

// ─── OPTIONS ──────────────────────────────────────────────────────
func (e *SIPEngine) onOptions(req *sip.Request, tx sip.ServerTransaction) {
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	tx.Respond(res)
}
