package main

import (
	"context"
	"log"
	"nextgen-sip/internal/billing"
	"nextgen-sip/internal/engine"
	"nextgen-sip/internal/firewall"
	"nextgen-sip/internal/registrar"
	"nextgen-sip/internal/router"
	"os"
	"os/signal"
	"syscall"

	"github.com/emiago/sipgo"
)

func main() {
	// Initialize Components
	reg := registrar.NewRedisRegistrar("localhost:6379")
	bill := billing.NewInMemoryBilling()
	fw := firewall.NewFirewall()
	cc := engine.NewCallControl(bill)
	admin := engine.NewAdminAPI(cc, bill)
	
	// Seed some test data
	bill.SetBalance("sip:100@localhost", 50.0)
	bill.SetBalance("sip:200@localhost", 10.0)

	rt := router.NewRoutingEngine(reg, bill)

	ua, err := sipgo.NewUA(
		sipgo.WithUserAgent("NextGen-SIP-Proxy/2.0"),
	)
	if err != nil {
		log.Fatalf("Failed to create UA: %v", err)
	}

	sipEngine := engine.NewSIPEngine(ua, rt, cc, fw)

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down SIP and Admin API...")
		cancel()
	}()

	// Start Admin API in background
	go func() {
		log.Println("Admin API starting on :8080")
		if err := admin.Start(":8080"); err != nil {
			log.Printf("Admin API failed: %v", err)
		}
	}()

	if err := sipEngine.Start(ctx); err != nil {
		log.Fatalf("Engine failed: %v", err)
	}
}

