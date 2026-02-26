	"nextgen-sip/internal/models"
	"nextgen-sip/pkg/utils"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CallControl manages the state of active calls
type CallControl struct {
	mu          sync.RWMutex
	activeCalls map[string]*models.ActiveCall
	billing     BillingEngine
	workers     int
	jobQueue    chan *models.ActiveCall
}

func NewCallControl(bill BillingEngine) *CallControl {
	cc := &CallControl{
		activeCalls: make(map[string]*models.ActiveCall),
		billing:     bill,
		workers:     100, // Handle deduction for millions of calls in parallel
		jobQueue:    make(chan *models.ActiveCall, 10000),
	}
	
	// Start Billing Worker Pool
	for i := 0; i < cc.workers; i++ {
		go cc.billingWorker()
	}

	// Start the main dispatcher loop
	go cc.dispatcher()
	
	return cc
}

func (cc *CallControl) StartCall(from, to, callID, tenantID string) string {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	sessionID := uuid.New().String()
	call := &models.ActiveCall{
		SessionID: sessionID,
		TenantID:  tenantID,
		From:      from,
		To:        to,
		CallID:    callID,
		State:     models.StateTrying,
		StartTime: time.Now(),
		Rate:      0.01,
	}
	cc.activeCalls[callID] = call
	
	utils.ActiveCalls.Inc()
	log.Printf("[CallControl] Call session %s started (Tenant: %s)", sessionID, tenantID)
	return sessionID
}

func (cc *CallControl) OnAnswer(callID string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if call, ok := cc.activeCalls[callID]; ok {
		call.State = models.StateConnected
		call.StartTime = time.Now()
		log.Printf("[CallControl] Call %s connected", callID)
	}
}

func (cc *CallControl) EndCall(callID string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if _, ok := cc.activeCalls[callID]; ok {
		delete(cc.activeCalls, callID)
		utils.ActiveCalls.Dec()
		log.Printf("[CallControl] Call %s ended", callID)
	}
}

// dispatcher collects jobs for the workers
func (cc *CallControl) dispatcher() {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		cc.mu.RLock()
		for _, call := range cc.activeCalls {
			if call.State == models.StateConnected {
				cc.jobQueue <- call
			}
		}
		cc.mu.RUnlock()
	}
}

// billingWorker processes deductions in parallel
func (cc *CallControl) billingWorker() {
	for call := range cc.jobQueue {
		err := cc.billing.Deduct(call.From, call.Rate)
		if err != nil {
			log.Printf("[BillingWorker] Insufficient funds for %s. Terminating %s", call.From, call.CallID)
			cc.forceTerminate(call.CallID)
			utils.BillingDeductionErrors.Inc()
		}
	}
}


func (cc *CallControl) GetActiveCalls() []*models.ActiveCall {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	list := make([]*models.ActiveCall, 0, len(cc.activeCalls))
	for _, c := range cc.activeCalls {
		list = append(list, c)
	}
	return list
}

func (cc *CallControl) forceTerminate(callID string) {
	// In a real system, this would send a BYE to both parties
	cc.EndCall(callID)
}

// Interface expansion for Billing
type BillingEngine interface {
	CanCall(from string, to string) (bool, error)
	Deduct(user string, amount float64) error
	SetBalance(user string, amount float64)
	ListUsers() ([]models.User, error)
	SaveUser(u models.User)
	DeleteUser(uri string)
}

