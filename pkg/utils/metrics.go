package utils

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ActiveCalls = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sip_active_calls",
		Help: "The total number of active SIP calls",
	})

	SipRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sip_requests_total",
		Help: "The total number of SIP requests processed",
	}, []string{"method", "tenant_id"})

	BillingDeductionErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "billing_deduction_errors_total",
		Help: "Total number of failed billing deductions",
	})
	
	FirewallBlocks = promauto.NewCounter(prometheus.CounterOpts{
		Name: "firewall_blocks_total",
		Help: "Total number of IP blocks by firewall",
	})
)
