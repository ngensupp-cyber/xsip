package models

import "time"

// CallState represents the current state of a SIP call
type CallState string

const (
	StateNone      CallState = "NONE"
	StateTrying    CallState = "TRYING"
	StateRinging   CallState = "RINGING"
	StateConnected CallState = "CONNECTED"
	StateEnded     CallState = "ENDED"
)

// ActiveCall represents a call currently in progress
type ActiveCall struct {
	SessionID string    `json:"session_id"`
	TenantID  string    `json:"tenant_id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	CallID    string    `json:"call_id"`
	Source    string    `json:"source"`
	State     CallState `json:"state"`
	StartTime time.Time `json:"start_time"`
	Rate      float64   `json:"rate"` // Price per second
}

// CDR for billing
type CDR struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Duration  float64   `json:"duration"`
	Cost      float64   `json:"cost"`
	Status    string    `json:"status"`
}


// User represents a VoIP subscriber
type User struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Balance   float64 `json:"balance"`
	Level     int    `json:"level"` // 0: User, 1: Reseller, 2: Admin
}
