	"github.com/labstack/echo/v4"
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

	// Static Files for Web Dashboard
	e.Static("/", "web")

	// API Routes
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	e.GET("/calls/active", a.listActiveCalls)
	
	// User Management CRUD
	e.GET("/users", a.listUsers)
	e.POST("/users", a.createUser)
	e.POST("/users/:id/balance", a.updateBalance)
	e.DELETE("/users/:id", a.deleteUser)

	e.GET("/stats", a.getStats)

	return e.Start(addr)
}

func (a *AdminAPI) listUsers(c echo.Context) error {
	users, _ := a.billing.ListUsers()
	return c.JSON(http.StatusOK, users)
}

func (a *AdminAPI) createUser(c echo.Context) error {
	var user models.User
	if err := c.Bind(&user); err != nil {
		return err
	}
	a.billing.SaveUser(user)
	return c.NoContent(http.StatusCreated)
}

func (a *AdminAPI) deleteUser(c echo.Context) error {
	id := c.Param("id")
	a.billing.DeleteUser("sip:" + id + "@localhost")
	return c.NoContent(http.StatusOK)
}

func (a *AdminAPI) listActiveCalls(c echo.Context) error {
	calls := a.cc.GetActiveCalls()
	return c.JSON(http.StatusOK, calls)
}

func (a *AdminAPI) updateBalance(c echo.Context) error {
	id := c.Param("id")
	var data struct {
		Amount float64 `json:"amount"`
	}
	if err := c.Bind(&data); err != nil {
		return err
	}
	a.billing.SetBalance("sip:"+id+"@localhost", data.Amount)
	return c.NoContent(http.StatusOK)
}

func (a *AdminAPI) getStats(c echo.Context) error {
	calls := a.cc.GetActiveCalls()
	return c.JSON(http.StatusOK, map[string]interface{}{
		"active_calls":   len(calls),
		"registrations":  124, // Mocked for UI
		"system_status": "optimal",
		"version":       "3.0.0-premium",
	})
}

