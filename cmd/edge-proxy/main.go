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
	// 1. Environment Variables Configuration
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379" // Default fallback
	}

	adminPort := os.Getenv("PORT")
	if adminPort == "" {
		adminPort = "8080"
	}

	sipPort := os.Getenv("SIP_PORT")
	if sipPort == "" {
		sipPort = "5060"
	}

	sipProtocol := os.Getenv("SIP_PROTOCOL")
	if sipProtocol == "" {
		sipProtocol = "udp"
	}

	// 2. Initialize Components
	reg := registrar.NewRedisRegistrar(redisURL)
	bill := billing.NewInMemoryBilling()
	fw := firewall.NewFirewall()
	cc := engine.NewCallControl(bill)
	admin := engine.NewAdminAPI(cc, bill)
	
	// Seed some test data
	bill.SetBalance("sip:100@localhost", 50.0)
	bill.SetBalance("sip:200@localhost", 10.0)

	rt := router.NewRoutingEngine(reg, bill)

	ua, err := sipgo.NewUA(
		sipgo.WithUserAgent("NextGen-SIP-Proxy/2.5-Railway"),
	)
	if err != nil {
		log.Fatalf("Failed to create UA: %v", err)
	}

	sipAddr := "0.0.0.0:" + sipPort
	sipEngine := engine.NewSIPEngine(ua, rt, cc, fw, sipAddr)

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Graceful shutdown initiated...")
		cancel()
	}()

	// Start Admin API
	go func() {
		addr := ":" + adminPort
		log.Printf("Admin API starting on %s", addr)
		if err := admin.Start(addr); err != nil {
			log.Printf("Admin API failed: %v", err)
		}
	}()

	// Start SIP Engine
	if err := sipEngine.Start(ctx, sipProtocol, sipAddr); err != nil {
		log.Fatalf("SIP Engine failed: %v", err)
	}
}


