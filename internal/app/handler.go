package app

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"seatmove/internal/auth"
	"seatmove/internal/models"
)

type Handler struct {
	db     *gorm.DB
	svc    *Service
	jwtMgr *auth.Manager
}

func NewHandler(db *gorm.DB, svc *Service, jwtMgr *auth.Manager) *Handler {
	return &Handler{db: db, svc: svc, jwtMgr: jwtMgr}
}

func (h *Handler) Register(r *gin.Engine) {
	api := r.Group("/api")
	api.POST("/login", h.login)

	authed := api.Group("")
	authed.Use(h.jwtMgr.Middleware())

	authed.GET("/me", h.me)
	authed.GET("/floors", h.listFloors)
	authed.GET("/seats", h.listSeats)
	authed.GET("/employees", h.listEmployees)

	authed.GET("/orders", h.listOrders)
	authed.GET("/orders/:id", h.getOrder)
	authed.GET("/orders/:id/events", h.listEvents)
	authed.GET("/seats/:id/active-order", h.activeOrderBySeat)

	// 行政
	admin := authed.Group("")
	admin.Use(auth.RequireRole(models.RoleAdmin))
	admin.POST("/orders", h.createOrder)
	admin.POST("/orders/:id/reschedule", h.reschedule)
	admin.POST("/orders/:id/cancel", h.cancelOrder)

	// IT
	it := authed.Group("")
	it.Use(auth.RequireRole(models.RoleIT))
	it.POST("/orders/:id/it-check", h.itCheck)
	it.POST("/seats/:id/net-state", h.setNetState)

	// 员工
	emp := authed.Group("")
	emp.Use(auth.RequireRole(models.RoleEmployee))
	emp.POST("/orders/:id/confirm", h.confirm)
}

// ---------- 认证 ----------

func (h *Handler) login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入用户名"})
		return
	}
	var u models.User
	if err := h.db.Where("username = ?", req.Username).First(&u).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户不存在（演示环境无密码）"})
		return
	}
	tok, err := h.jwtMgr.Issue(&u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发令牌失败"})
		return
	}
	u.Seat = nil
	c.JSON(http.StatusOK, gin.H{"token": tok, "user": u})
}

func (h *Handler) me(c *gin.Context) {
	cl := auth.Current(c)
	var u models.User
	if err := h.db.Preload("Seat").First(&u, cl.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, u)
}

// ---------- 楼层/座位 ----------

func (h *Handler) listFloors(c *gin.Context) {
	fs, err := h.svc.Floors()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"floors": fs})
}

func (h *Handler) listSeats(c *gin.Context) {
	seats, err := h.svc.ListSeats(c.Query("floor"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"seats": seats})
}

func (h *Handler) listEmployees(c *gin.Context) {
	us, err := h.svc.ListEmployees()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"employees": us})
}

func (h *Handler) activeOrderBySeat(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	o, err := h.svc.ActiveOrderBySeat(uint(id))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	cl := auth.Current(c)
	if o != nil && cl.Role == models.RoleEmployee && o.EmployeeID != cl.UserID {
		c.JSON(http.StatusOK, gin.H{"order": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"order": o})
}

// ---------- 搬迁单 ----------

func (h *Handler) listOrders(c *gin.Context) {
	cl := auth.Current(c)
	f := OrderFilter{Status: c.Query("status")}
	if cl.Role == models.RoleEmployee {
		// 员工只能看到自己的单
		f.EmployeeID = cl.UserID
	}
	orders, err := h.svc.ListOrders(f)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"orders": orders})
}

func (h *Handler) getOrder(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	o, events, err := h.svc.GetOrder(uint(id))
	if err != nil {
		status, msg := ErrStatus(err)
		c.JSON(status, gin.H{"error": msg})
		return
	}
	cl := auth.Current(c)
	// 员工只能查看自己的单
	if cl.Role == models.RoleEmployee && o.EmployeeID != cl.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看他人的搬迁单"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"order": o, "events": events})
}

func (h *Handler) listEvents(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	o, _, err := h.svc.GetOrder(uint(id))
	if err != nil {
		status, msg := ErrStatus(err)
		c.JSON(status, gin.H{"error": msg})
		return
	}
	cl := auth.Current(c)
	if cl.Role == models.RoleEmployee && o.EmployeeID != cl.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权查看"})
		return
	}
	var events []models.MoveEvent
	h.db.Where("order_id = ?", id).Order("id DESC").Find(&events)
	c.JSON(http.StatusOK, gin.H{"events": events})
}

func (h *Handler) createOrder(c *gin.Context) {
	cl := auth.Current(c)
	var req struct {
		EmployeeID uint   `json:"employee_id"`
		FromSeatID uint   `json:"from_seat_id"`
		ToSeatID   uint   `json:"to_seat_id"`
		MoveDate   string `json:"move_date"` // YYYY-MM-DD
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "请求格式错误"})
		return
	}
	md, err := time.ParseInLocation("2006-01-02", req.MoveDate, time.Local)
	if err != nil {
		c.JSON(400, gin.H{"error": "搬迁日期格式应为 YYYY-MM-DD"})
		return
	}
	o, err := h.svc.CreateOrder(currentUser(cl), CreateOrderInput{
		EmployeeID: req.EmployeeID, FromSeatID: req.FromSeatID,
		ToSeatID: req.ToSeatID, MoveDate: md,
	})
	if err != nil {
		status, msg := ErrStatus(err)
		c.JSON(status, gin.H{"error": msg})
		return
	}
	full, _, err := h.svc.GetOrder(o.ID)
	if err != nil {
		c.JSON(201, gin.H{"order": o})
		return
	}
	c.JSON(201, gin.H{"order": full})
}

func (h *Handler) reschedule(c *gin.Context) {
	cl := auth.Current(c)
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		NewMoveDate string `json:"new_move_date"`
		NewToSeatID uint   `json:"new_to_seat_id"`
	}
	_ = c.ShouldBindJSON(&req)
	in := RescheduleInput{NewToSeatID: req.NewToSeatID}
	if req.NewMoveDate != "" {
		md, err := time.ParseInLocation("2006-01-02", req.NewMoveDate, time.Local)
		if err != nil {
			c.JSON(400, gin.H{"error": "日期格式应为 YYYY-MM-DD"})
			return
		}
		in.NewMoveDate = md
	}
	if err := h.svc.Reschedule(currentUser(cl), uint(id), in); err != nil {
		status, msg := ErrStatus(err)
		c.JSON(status, gin.H{"error": msg})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) cancelOrder(c *gin.Context) {
	cl := auth.Current(c)
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := h.svc.Cancel(currentUser(cl), uint(id), req.Reason); err != nil {
		status, msg := ErrStatus(err)
		c.JSON(status, gin.H{"error": msg})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) itCheck(c *gin.Context) {
	cl := auth.Current(c)
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		MonitorOK      bool   `json:"monitor_ok"`
		ActualAssetTag string `json:"actual_asset_tag"`
		Note           string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "请求格式错误"})
		return
	}
	out, err := h.svc.ITCheck(c.Request.Context(), currentUser(cl), uint(id), ITCheckInput{
		MonitorOK:      req.MonitorOK,
		ActualAssetTag: req.ActualAssetTag,
		Note:           req.Note,
	})
	if err != nil {
		status, msg := ErrStatus(err)
		// 核对未通过（422）时返回结构化结果，前端展示原因
		var be *BizError
		if errors.As(err, &be) {
			c.JSON(status, gin.H{"error": msg, "passed": out != nil && out.Passed})
			return
		}
		_ = be
		c.JSON(status, gin.H{"error": msg})
		return
	}
	full, _, gerr := h.svc.GetOrder(out.Order.ID)
	if gerr != nil {
		c.JSON(200, gin.H{"ok": true, "passed": true, "order": out.Order})
		return
	}
	c.JSON(200, gin.H{"ok": true, "passed": true, "order": full})
}

func (h *Handler) setNetState(c *gin.Context) {
	cl := auth.Current(c)
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "请求格式错误"})
		return
	}
	if err := h.svc.SetSeatNetEnabled(currentUser(cl), uint(id), req.Enabled, req.Reason); err != nil {
		status, msg := ErrStatus(err)
		c.JSON(status, gin.H{"error": msg})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) confirm(c *gin.Context) {
	cl := auth.Current(c)
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		NetUsable    bool   `json:"net_usable"`
		DeviceUsable bool   `json:"device_usable"`
		Note         string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "请求格式错误"})
		return
	}
	o, err := h.svc.Confirm(currentUser(cl), uint(id), ConfirmInput{
		NetUsable: req.NetUsable, DeviceUsable: req.DeviceUsable, Note: req.Note,
	})
	if err != nil {
		status, msg := ErrStatus(err)
		c.JSON(status, gin.H{"error": msg})
		return
	}
	full, _, gerr := h.svc.GetOrder(o.ID)
	if gerr != nil {
		c.JSON(200, gin.H{"ok": true, "order": o})
		return
	}
	c.JSON(200, gin.H{"ok": true, "order": full})
}

// currentUser 从 JWT claims 还原一个精简 User（服务层只需要 id/name）
func currentUser(cl *auth.Claims) *models.User {
	return &models.User{ID: cl.UserID, Name: cl.Name, Username: cl.Username, Role: cl.Role}
}
