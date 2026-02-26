package engine

import (
	"context"
	"log"
	"nextgen-sip/internal/router"
	"nextgen-sip/internal/firewall"
	"nextgen-sip/pkg/utils"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type SIPEngine struct {
	server *sipgo.Server
	router *router.RoutingEngine
	cc     *CallControl
	fw     *firewall.Firewall
}

func NewSIPEngine(ua *sipgo.UserAgent, r *router.RoutingEngine, cc *CallControl, fw *firewall.Firewall) *SIPEngine {
	s, err := sipgo.NewServer(ua)
	if err != nil {
		log.Fatal(err)
	}
	return &SIPEngine{
		server: s,
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
	
	// 2. Start Call Tracking
	callID := req.CallID().Value()
	e.cc.StartCall(req.From().Address.String(), req.To().Address.String(), callID, tenantID)


	log.Printf("Routing INVITE to %s", dest)
	// In reality, here we would use sipgo to forward the request
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

