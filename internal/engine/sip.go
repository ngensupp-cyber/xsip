package engine

import (
	"context"
	"log"
	"time"
	"nextgen-sip/internal/router"
	"nextgen-sip/internal/firewall"
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
	// Add Global Middleware (Firewall)
	e.server.OnInvite(e.onInvite)
	e.server.OnRegister(e.onRegister)
	e.server.OnBye(e.onBye)
	e.server.OnMessage(e.onMessage)

	log.Printf("Carrier-Grade SIP Engine starting on %s (%s)", addr, network)
	return e.server.ListenAndServe(ctx, network, addr)
}


func (e *SIPEngine) onInvite(req *sip.Request, tx sip.ServerTransaction) {
	// 1. Firewall Check
	ip := req.Source()
	if !e.fw.IsAllowed(ip) {
		utils.FirewallBlocks.Inc()
		log.Printf("[SIP] Blocked INVITE from banned IP: %s", ip)
		return
	}

	tenantID := "default"
	if h := req.GetHeader("X-Tenant-ID"); h != nil {
		tenantID = h.String()
	}

	utils.SipRequestsTotal.WithLabelValues("INVITE", tenantID).Inc()

	dest, err := e.router.Route(req)
	if err != nil {
		e.fw.RecordFailedAuth(ip)
		res := sip.NewResponseFromRequest(req, 403, "Forbidden", nil)
		tx.Respond(res)
		return
	}
	
	// 2. Forward INVITE to Destination
	log.Printf("[SIP] Proxying INVITE to %s", dest)
	
	var destURI sip.Uri
	if err := sip.ParseUri(dest, &destURI); err != nil {
		log.Printf("[SIP] Failed to parse destination %s: %v", dest, err)
		return
	}

	// Create a proxy request
	proxyReq := req.Clone()
	proxyReq.Recipient = &destURI // Update Request-URI
	
	// Start Call Tracking
	callID := req.CallID().Value()
	e.cc.StartCall(req.From().Address.String(), req.To().Address.String(), callID, tenantID)

	// Send request through client and relay responses
	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		log.Printf("[SIP] Proxy error: %v", err)
		return
	}

	go func() {
		defer clTx.Terminate()
		for {
			select {
			case res := <-clTx.Responses():
				if res == nil {
					return
				}
				// Relay response back to original caller
				proxyRes := res.Clone()
				// Note: In a full proxy, we would adjust headers here
				tx.Respond(proxyRes)
			case <-tx.Done():
				return
			case <-clTx.Done():
				return
			}
		}
	}()
}

func (e *SIPEngine) onRegister(req *sip.Request, tx sip.ServerTransaction) {
	ip := req.Source()
	if !e.fw.IsAllowed(ip) {
		return
	}

	_, err := e.router.Route(req)
	if err != nil {
		e.fw.RecordFailedAuth(ip)
		res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
		tx.Respond(res)
		return
	}
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	tx.Respond(res)
}

func (e *SIPEngine) onBye(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	e.cc.EndCall(callID)
	
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	tx.Respond(res)
}

func (e *SIPEngine) onMessage(req *sip.Request, tx sip.ServerTransaction) {
	log.Printf("[SIP] MESSAGE received from %s to %s", req.From().Address.String(), req.To().Address.String())
	
	dest, err := e.router.Route(req)
	if err != nil {
		res := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
		tx.Respond(res)
		return
	}

	var destURI sip.Uri
	_ = sip.ParseUri(dest, &destURI)
	proxyReq := req.Clone()
	proxyReq.Recipient = &destURI

	ctx := context.Background()
	clTx, err := e.client.TransactionRequest(ctx, proxyReq)
	if err != nil {
		res := sip.NewResponseFromRequest(req, 500, "Server Internal Error", nil)
		tx.Respond(res)
		return
	}

	go func() {
		defer clTx.Terminate()
		select {
		case res := <-clTx.Responses():
			if res != nil {
				tx.Respond(res.Clone())
			}
		case <-time.After(5 * time.Second):
			res := sip.NewResponseFromRequest(req, 408, "Request Timeout", nil)
			tx.Respond(res)
		}
	}()
}



