package engine

import (
	"net/http"
	"nextgen-sip/internal/models"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type AdminAPI struct {
	cc      *CallControl
	billing BillingEngine
}

func NewAdminAPI(cc *CallControl, bill BillingEngine) *AdminAPI {
	return &AdminAPI{
		cc:      cc,
		billing: bill,
	}
}

func (a *AdminAPI) Start(addr string) error {
	e := echo.New()
	e.HideBanner = true

	// CORS for web dashboard
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{echo.GET, echo.POST, echo.PUT, echo.DELETE},
	}))

	// Static Files for Web Dashboard
	e.Static("/", "web")

	// Metrics
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	// ─── Stats ───────────────────────────────────────────
	e.GET("/api/stats", a.getStats)

	// ─── User Management CRUD ────────────────────────────
	e.GET("/api/users", a.listUsers)
	e.POST("/api/users", a.createUser)
	e.PUT("/api/users/:id", a.updateUser)
	e.POST("/api/users/:id/balance", a.updateBalance)
	e.DELETE("/api/users/:id", a.deleteUser)

	// ─── Active Calls ────────────────────────────────────
	e.GET("/api/calls/active", a.listActiveCalls)

	// ─── System Config ───────────────────────────────────
	e.GET("/api/config", a.getConfig)

	return e.Start(addr)
}

// ─── Stats ───────────────────────────────────────────────────────────────────
func (a *AdminAPI) getStats(c echo.Context) error {
	calls := a.cc.GetActiveCalls()
	users, _ := a.billing.ListUsers()
	return c.JSON(http.StatusOK, map[string]interface{}{
		"active_calls":  len(calls),
		"total_users":   len(users),
		"system_status": "operational",
		"version":       "3.0.0-carrier",
		"uptime":        "running",
	})
}

// ─── Users ───────────────────────────────────────────────────────────────────
func (a *AdminAPI) listUsers(c echo.Context) error {
	users, _ := a.billing.ListUsers()
	return c.JSON(http.StatusOK, users)
}

func (a *AdminAPI) createUser(c echo.Context) error {
	var user models.User
	if err := c.Bind(&user); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if user.ID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id is required"})
	}
	a.billing.SaveUser(user)
	return c.JSON(http.StatusCreated, user)
}

func (a *AdminAPI) updateUser(c echo.Context) error {
	var user models.User
	if err := c.Bind(&user); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	user.ID = c.Param("id")
	a.billing.SaveUser(user)
	return c.JSON(http.StatusOK, user)
}

func (a *AdminAPI) deleteUser(c echo.Context) error {
	id := c.Param("id")
	a.billing.DeleteUser("sip:" + id + "@localhost")
	return c.NoContent(http.StatusOK)
}

func (a *AdminAPI) updateBalance(c echo.Context) error {
	id := c.Param("id")
	var data struct {
		Amount float64 `json:"amount"`
	}
	if err := c.Bind(&data); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	a.billing.SetBalance("sip:"+id+"@localhost", data.Amount)
	return c.NoContent(http.StatusOK)
}

// ─── Active Calls ────────────────────────────────────────────────────────────
func (a *AdminAPI) listActiveCalls(c echo.Context) error {
	calls := a.cc.GetActiveCalls()
	return c.JSON(http.StatusOK, calls)
}

// ─── Config ──────────────────────────────────────────────────────────────────
func (a *AdminAPI) getConfig(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"sip_protocol":        "TCP",
		"max_concurrent_calls": 100000,
		"billing_rate":        0.01,
		"registration_ttl":   "1h",
		"firewall_threshold": 5,
	})
}
