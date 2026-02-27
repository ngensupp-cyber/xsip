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

func NewSIPEngine(ua *sipgo.UserAgent, r *router.RoutingEngine, cc *CallControl, fw *firewall.Firewall, clientAddr string) *SIPEngine {
	s, err := sipgo.NewServer(ua)
	if err != nil {
		log.Fatal(err)
	}
	c, err := sipgo.NewClient(ua, sipgo.WithClientAddr(clientAddr))
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
	e.server.OnBye(e.proxyRoute)
	e.server.OnMessage(e.proxyRoute)
	e.server.OnOptions(e.onOptions)
	e.server.OnAck(e.onAck)
	e.server.OnCancel(e.proxyRoute)

	log.Printf("=== XSIP Carrier Engine v6.0 ===")
	log.Printf("Listening on %s (%s)", addr, network)
	return e.server.ListenAndServe(ctx, network, addr)
}

// ─── Helper: send a response back to the request source ──────────
func (e *SIPEngine) reply(tx sip.ServerTransaction, req *sip.Request, code int, reason string) {
	resp := sip.NewResponseFromRequest(req, sip.StatusCode(code), reason, nil)
	resp.SetDestination(req.Source())
	if err := tx.Respond(resp); err != nil {
		log.Printf("[SIP] Failed to respond %d: %v", code, err)
	}
}

// ─── Generic Proxy Route (BYE, MESSAGE, CANCEL, etc.) ────────────
// Follows the official sipgo proxy pattern: SetDestination + ClientRequestAddVia
func (e *SIPEngine) proxyRoute(req *sip.Request, tx sip.ServerTransaction) {
	ip := req.Source()
	if !e.fw.IsAllowed(ip) {
		return
	}

	method := req.Method
	from := req.From().Address.String()
	to := req.To().Address.String()
	log.Printf("[%s] %s -> %s", method, from, to)

	// Handle BYE call tracking
	if method == sip.BYE {
		e.cc.EndCall(req.CallID().Value())
	}

	// Route to find destination
	dest, err := e.router.Route(req)
	if err != nil {
		log.Printf("[%s] ✗ Route failed: %v", method, err)
		e.reply(tx, req, 404, "Not Found")
		return
	}
	log.Printf("[%s] ✓ Dest: %s", method, dest)

	// ★ KEY: Set destination on the ORIGINAL request (don't build a new one!)
	req.SetDestination(dest)

	// ★ KEY: Use ClientRequestAddVia so sipgo properly manages Via headers
	clTx, err := e.client.TransactionRequest(context.Background(), req, sipgo.ClientRequestAddVia)
	if err != nil {
		log.Printf("[%s] ✗ Proxy failed: %v", method, err)
		e.reply(tx, req, 502, "Bad Gateway")
		return
	}
	defer clTx.Terminate()

	// ★ BLOCK: wait for responses and relay them back
	for {
		select {
		case res, more := <-clTx.Responses():
			if !more {
				return
			}

			log.Printf("[%s] ← %d %s", method, res.StatusCode, res.Reason)

			// ★ KEY: Set destination back to caller and remove top Via
			res.SetDestination(req.Source())
			res.RemoveHeader("Via")

			if err := tx.Respond(res); err != nil {
				log.Printf("[%s] ✗ Relay response failed: %v", method, err)
			}

		case <-clTx.Done():
			err := clTx.Err()
			if err != nil && err != sip.ErrTransactionTerminated {
				log.Printf("[%s] Client tx done with error: %v", method, err)
			} else {
			    log.Printf("[%s] Client tx done", method)
			}
			return

		case <-tx.Done():
			err := tx.Err()
			if err != nil && err != sip.ErrTransactionTerminated {
				log.Printf("[%s] Server tx done with error: %v", method, err)
			} else {
			    log.Printf("[%s] Server tx done", method)
			}
			return
		}
	}
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
		e.reply(tx, req, 401, "Unauthorized")
		return
	}

	log.Printf("[SIP] ✓ Registration: %s", result)
	e.reply(tx, req, 200, "OK")
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

	// Route
	dest, err := e.router.Route(req)
	if err != nil {
		log.Printf("[INVITE] ✗ Route failed: %v", err)
		e.reply(tx, req, 404, "Not Found")
		return
	}
	log.Printf("[INVITE] ✓ Dest: %s", dest)

	// Track call
	e.cc.StartCall(from, to, callID, tenantID)

	// ★ KEY: Set destination on original request
	req.SetDestination(dest)

	// ★ KEY: Forward with proper Via and Record-Route
	clTx, err := e.client.TransactionRequest(context.Background(), req, sipgo.ClientRequestAddVia)
	if err != nil {
		log.Printf("[INVITE] ✗ Proxy failed: %v", err)
		e.reply(tx, req, 503, "Service Unavailable")
		return
	}
	defer clTx.Terminate()

	log.Printf("[INVITE] ✓ Forwarded, blocking for response...")

	// ★ BLOCK: process responses, ACKs, and cancellations
	for {
		select {
		case res, more := <-clTx.Responses():
			if !more {
				return
			}

			log.Printf("[INVITE] ← %d %s", res.StatusCode, res.Reason)

			res.SetDestination(req.Source())
			res.RemoveHeader("Via")

			if err := tx.Respond(res); err != nil {
				log.Printf("[INVITE] ✗ Relay failed: %v", err)
			}

			if res.StatusCode >= 200 {
				log.Printf("[INVITE] ✓ Final response %d relayed!", res.StatusCode)
				return
			}

		case ack := <-tx.Acks():
			// Relay ACK to callee
			log.Printf("[INVITE] ACK received, relaying to %s", dest)
			ack.SetDestination(dest)
			e.client.WriteRequest(ack, sipgo.ClientRequestAddVia)

		case <-clTx.Done():
			err := clTx.Err()
			if err != nil && err != sip.ErrTransactionTerminated {
				log.Printf("[INVITE] Client tx done with error: %v", err)
			} else {
			    log.Printf("[INVITE] Client tx done", method)
			}
			return

		case <-tx.Done():
			err := tx.Err()
			if err != nil {
				if err == sip.ErrTransactionCanceled {
					// Caller canceled — send CANCEL to callee
					log.Printf("[INVITE] Caller canceled, forwarding CANCEL")
					cancelReq := sip.NewRequest(sip.CANCEL, req.Recipient)
					sip.CopyHeaders("Via", req, cancelReq)
					sip.CopyHeaders("From", req, cancelReq)
					sip.CopyHeaders("To", req, cancelReq)
					sip.CopyHeaders("Call-ID", req, cancelReq)
					cancelReq.SetDestination(dest)
					e.client.Do(context.Background(), cancelReq)
					return
				}
				if err != sip.ErrTransactionTerminated {
					log.Printf("[INVITE] Server tx error: %v", err)
				}
			}
			log.Printf("[INVITE] Server tx done")
			return
		}
	}
}

// ─── ACK (standalone, outside INVITE tx) ──────────────────────────
func (e *SIPEngine) onAck(req *sip.Request, tx sip.ServerTransaction) {
	dest, err := e.router.Route(req)
	if err != nil {
		return
	}

	log.Printf("[ACK] Relaying to %s", dest)
	req.SetDestination(dest)
	e.client.WriteRequest(req, sipgo.ClientRequestAddVia)
}

// ─── OPTIONS ──────────────────────────────────────────────────────
func (e *SIPEngine) onOptions(req *sip.Request, tx sip.ServerTransaction) {
	e.reply(tx, req, 200, "OK")
}
