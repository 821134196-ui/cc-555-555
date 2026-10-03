package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"relocation/internal/models"
)

func isActive(status string) bool {
	for _, s := range models.ActiveStatuses {
		if s == status {
			return true
		}
	}
	return false
}

func addEvent(tx *gorm.DB, ticketID, actorID uint, action, detail string) error {
	return tx.Create(&models.TicketEvent{
		TicketID: ticketID, ActorID: actorID, Action: action, Detail: detail,
	}).Error
}

// updateTicket 按 ID 更新搬迁单。
// 注意：不能对预加载过关联的结构体调用 Model(t).Updates(map)，
// GORM 会同步 belongs-to 关联的外键，把 map 里的新值覆盖回旧值。
func updateTicket(tx *gorm.DB, id uint, updates map[string]any) *gorm.DB {
	return tx.Model(&models.Ticket{}).Where("id = ?", id).Updates(updates)
}

// 唯一索引冲突 → 409。索引名区分座位冲突与员工重复建单。
func conflictErr(err error) (string, bool) {
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return "", false
	}
	if strings.Contains(err.Error(), "idx_tickets_active_employee") {
		return "该员工已有进行中的搬迁单", true
	}
	return "目标座位已被其他搬迁单预占", true
}

func (s *Server) loadTicket(c *gin.Context) (*models.Ticket, bool) {
	var t models.Ticket
	err := s.db.Preload("Employee").Preload("FromSeat").Preload("ToSeat").
		Preload("CreatedBy").First(&t, c.Param("id")).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		abort(c, http.StatusNotFound, "搬迁单不存在")
		return nil, false
	}
	if err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return &t, true
}

// ---------- 查询 ----------

func (s *Server) listTickets(c *gin.Context) {
	u := currentUser(c)
	q := s.db.Preload("Employee").Preload("FromSeat").Preload("ToSeat").
		Preload("CreatedBy").Order("id DESC")
	if u.Role == models.RoleEmployee {
		q = q.Where("employee_id = ?", u.ID) // 员工只能看自己的搬迁单
	}
	if st := c.Query("status"); st != "" {
		q = q.Where("status = ?", st)
	}
	var tickets []models.Ticket
	if err := q.Find(&tickets).Error; err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, tickets)
}

func (s *Server) getTicket(c *gin.Context) {
	t, ok := s.loadTicket(c)
	if !ok {
		return
	}
	u := currentUser(c)
	if u.Role == models.RoleEmployee && t.EmployeeID != u.ID {
		abort(c, http.StatusForbidden, "不能查看他人的搬迁单")
		return
	}
	var events []models.TicketEvent
	s.db.Preload("Actor").Where("ticket_id = ?", t.ID).Order("id").Find(&events)
	t.Events = events
	c.JSON(http.StatusOK, t)
}

// ---------- 行政操作 ----------

func (s *Server) createTicket(c *gin.Context) {
	var req struct {
		EmployeeID uint   `json:"employee_id" binding:"required"`
		ToSeatID   uint   `json:"to_seat_id" binding:"required"`
		MoveDate   string `json:"move_date" binding:"required"`
		Note       string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "employee_id、to_seat_id、move_date 必填")
		return
	}
	if _, err := time.Parse("2006-01-02", req.MoveDate); err != nil {
		abort(c, http.StatusBadRequest, "move_date 格式应为 YYYY-MM-DD")
		return
	}
	u := currentUser(c)

	var emp models.User
	if err := s.db.First(&emp, req.EmployeeID).Error; err != nil || emp.Role != models.RoleEmployee {
		abort(c, http.StatusBadRequest, "员工不存在")
		return
	}

	var ticket models.Ticket
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 原座位 = 员工当前座位
		var fromSeat models.Seat
		if err := tx.Where("occupant_id = ?", emp.ID).First(&fromSeat).Error; err != nil {
			return errors.New("该员工当前没有座位，无法发起搬迁")
		}
		if fromSeat.ID == req.ToSeatID {
			return errors.New("目标座位不能与原座位相同")
		}
		var toSeat models.Seat
		if err := tx.First(&toSeat, req.ToSeatID).Error; err != nil {
			return errors.New("目标座位不存在")
		}
		if toSeat.OccupantID != nil {
			return errors.New("目标座位已有人使用")
		}
		ticket = models.Ticket{
			EmployeeID: emp.ID, FromSeatID: fromSeat.ID, ToSeatID: req.ToSeatID,
			MoveDate: req.MoveDate, Status: models.StatusPending,
			Note: req.Note, CreatedByID: u.ID,
		}
		// 唯一部分索引兜底：并发下同座位/同员工只成功一单
		if err := tx.Create(&ticket).Error; err != nil {
			return err
		}
		ticket.TicketNo = fmt.Sprintf("MV-%04d", ticket.ID)
		if err := tx.Model(&ticket).Update("ticket_no", ticket.TicketNo).Error; err != nil {
			return err
		}
		return addEvent(tx, ticket.ID, u.ID, "created",
			fmt.Sprintf("创建搬迁单：%s 从 %s 迁至 %s，计划 %s",
				emp.Name, fromSeat.Code, toSeat.Code, req.MoveDate))
	})
	if err != nil {
		if msg, ok := conflictErr(err); ok {
			abort(c, http.StatusConflict, msg)
		} else {
			abort(c, http.StatusBadRequest, err.Error())
		}
		return
	}
	s.db.Preload("Employee").Preload("FromSeat").Preload("ToSeat").
		Preload("CreatedBy").First(&ticket, ticket.ID)
	c.JSON(http.StatusCreated, ticket)
}

func (s *Server) rescheduleTicket(c *gin.Context) {
	var req struct {
		MoveDate string `json:"move_date" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "move_date 必填")
		return
	}
	if _, err := time.Parse("2006-01-02", req.MoveDate); err != nil {
		abort(c, http.StatusBadRequest, "move_date 格式应为 YYYY-MM-DD")
		return
	}
	t, ok := s.loadTicket(c)
	if !ok {
		return
	}
	if !isActive(t.Status) {
		abort(c, http.StatusConflict, "搬迁单已结束，不能改期")
		return
	}
	u := currentUser(c)
	old := t.MoveDate
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := updateTicket(tx, t.ID, map[string]any{"move_date": req.MoveDate}).Error; err != nil {
			return err
		}
		return addEvent(tx, t.ID, u.ID, "rescheduled",
			fmt.Sprintf("搬迁日期由 %s 改为 %s", old, req.MoveDate))
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.getTicket(c)
}

func (s *Server) changeSeat(c *gin.Context) {
	var req struct {
		ToSeatID uint `json:"to_seat_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "to_seat_id 必填")
		return
	}
	t, ok := s.loadTicket(c)
	if !ok {
		return
	}
	if !isActive(t.Status) {
		abort(c, http.StatusConflict, "搬迁单已结束，不能更换座位")
		return
	}
	if t.ToSeatID == req.ToSeatID {
		abort(c, http.StatusBadRequest, "新座位与当前目标座位相同")
		return
	}
	u := currentUser(c)
	var newSeat models.Seat
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&newSeat, req.ToSeatID).Error; err != nil {
			return errors.New("目标座位不存在")
		}
		if newSeat.OccupantID != nil {
			return errors.New("目标座位已有人使用")
		}
		// 换座后旧座位的预占随之释放；新座位由唯一索引保证不被重复预占。
		// 此前针对旧座位的 IT 检查结果全部作废，需重新核对。
		updates := map[string]any{
			"to_seat_id": req.ToSeatID, "status": models.StatusPending,
			"network_ok": false, "network_tested_by": nil, "network_tested_at": nil,
			"network_latency_ms": 0, "network_message": "",
			"phone_ok": false, "extension_no": "",
			"monitor_ok": false, "asset_tag_reported": "", "asset_mismatch": false,
		}
		if err := updateTicket(tx, t.ID, updates).Error; err != nil {
			return err
		}
		return addEvent(tx, t.ID, u.ID, "seat_changed",
			fmt.Sprintf("目标座位由 %s 改为 %s，IT 核对结果已重置", t.ToSeat.Code, newSeat.Code))
	})
	if err != nil {
		if msg, ok := conflictErr(err); ok {
			abort(c, http.StatusConflict, msg)
		} else {
			abort(c, http.StatusBadRequest, err.Error())
		}
		return
	}
	s.getTicket(c)
}

func (s *Server) cancelTicket(c *gin.Context) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	t, ok := s.loadTicket(c)
	if !ok {
		return
	}
	if !isActive(t.Status) {
		abort(c, http.StatusConflict, "搬迁单已结束，不能取消")
		return
	}
	u := currentUser(c)
	now := time.Now()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"status": models.StatusCancelled, "cancelled_at": &now,
			"cancel_reason": req.Reason,
		}
		if err := updateTicket(tx, t.ID, updates).Error; err != nil {
			return err
		}
		// 状态变为 cancelled 后，唯一索引自动释放该座位的预占
		detail := "取消搬迁"
		if req.Reason != "" {
			detail += "：" + req.Reason
		}
		detail += fmt.Sprintf("（已释放 %s 的预占）", t.ToSeat.Code)
		return addEvent(tx, t.ID, u.ID, "cancelled", detail)
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.getTicket(c)
}

// ---------- IT 操作（行政无权代填） ----------

// maybeReady 三项核对全部通过且无资产异常时，搬迁单进入 ready
func (s *Server) maybeReady(tx *gorm.DB, ticketID uint, actorID uint) error {
	var t models.Ticket
	if err := tx.First(&t, ticketID).Error; err != nil {
		return err
	}
	if !isActive(t.Status) {
		return nil
	}
	if t.NetworkOK && t.PhoneOK && t.MonitorOK && !t.AssetMismatch {
		if err := updateTicket(tx, t.ID, map[string]any{"status": models.StatusReady}).Error; err != nil {
			return err
		}
		return addEvent(tx, t.ID, actorID, "it_ready",
			"网口、分机、显示器核对全部通过，具备搬迁条件")
	}
	return nil
}

func (s *Server) itNetworkTest(c *gin.Context) {
	t, ok := s.loadTicket(c)
	if !ok {
		return
	}
	if !isActive(t.Status) {
		abort(c, http.StatusConflict, "搬迁单已结束，不能测试")
		return
	}
	u := currentUser(c)
	res := s.mock.TestNetworkPort(t.ToSeat.PortCode)
	now := time.Now()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"network_ok": res.OK, "network_tested_by": u.ID, "network_tested_at": &now,
			"network_latency_ms": res.LatencyMs, "network_message": res.Message,
		}
		action := "network_test_passed"
		if !res.OK {
			// 网口不可用：搬迁单进入受阻状态，不能直接结束
			updates["status"] = models.StatusBlocked
			action = "network_test_failed"
		}
		if err := updateTicket(tx, t.ID, updates).Error; err != nil {
			return err
		}
		if err := addEvent(tx, t.ID, u.ID, action,
			fmt.Sprintf("网口 %s 测试：%s（%dms）", t.ToSeat.PortCode, res.Message, res.LatencyMs)); err != nil {
			return err
		}
		return s.maybeReady(tx, t.ID, u.ID)
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.getTicket(c)
}

func (s *Server) itPhoneMigrate(c *gin.Context) {
	t, ok := s.loadTicket(c)
	if !ok {
		return
	}
	if !isActive(t.Status) {
		abort(c, http.StatusConflict, "搬迁单已结束，不能操作")
		return
	}
	u := currentUser(c)
	res := s.mock.MigrateExtension(t.Employee.Extension, t.ToSeatID)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"phone_ok": res.OK}
		if res.OK {
			updates["extension_no"] = res.Extension
		}
		if err := updateTicket(tx, t.ID, updates).Error; err != nil {
			return err
		}
		action := "phone_migrated"
		if !res.OK {
			action = "phone_failed"
		}
		if err := addEvent(tx, t.ID, u.ID, action, res.Message); err != nil {
			return err
		}
		return s.maybeReady(tx, t.ID, u.ID)
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.getTicket(c)
}

func (s *Server) itMonitorCheck(c *gin.Context) {
	var req struct {
		MonitorOK bool   `json:"monitor_ok"`
		AssetTag  string `json:"asset_tag" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "asset_tag 必填（扫描显示器资产标签）")
		return
	}
	t, ok := s.loadTicket(c)
	if !ok {
		return
	}
	if !isActive(t.Status) {
		abort(c, http.StatusConflict, "搬迁单已结束，不能操作")
		return
	}
	if req.MonitorOK && !t.ToSeat.HasMonitor {
		abort(c, http.StatusBadRequest, "该座位未配置显示器，不能标记显示器正常")
		return
	}
	u := currentUser(c)
	mismatch := req.AssetTag != t.ToSeat.AssetTag
	err := s.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"monitor_ok": req.MonitorOK, "asset_tag_reported": req.AssetTag,
			"asset_mismatch": mismatch,
		}
		if err := updateTicket(tx, t.ID, updates).Error; err != nil {
			return err
		}
		detail := fmt.Sprintf("显示器核对：%s；资产标签 %s",
			map[bool]string{true: "正常", false: "异常"}[req.MonitorOK], req.AssetTag)
		if err := addEvent(tx, t.ID, u.ID, "monitor_checked", detail); err != nil {
			return err
		}
		if mismatch {
			// 资产标签不符：保留变更经过，等待核实
			if err := addEvent(tx, t.ID, u.ID, "asset_mismatch",
				fmt.Sprintf("资产标签不符：台账 %s，实物 %s，待核实", t.ToSeat.AssetTag, req.AssetTag)); err != nil {
				return err
			}
		}
		return s.maybeReady(tx, t.ID, u.ID)
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.getTicket(c)
}

// itResolveAsset IT 现场核实后更正台账，解除资产异常
func (s *Server) itResolveAsset(c *gin.Context) {
	var req struct {
		AssetTag string `json:"asset_tag" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "asset_tag 必填")
		return
	}
	t, ok := s.loadTicket(c)
	if !ok {
		return
	}
	if !isActive(t.Status) {
		abort(c, http.StatusConflict, "搬迁单已结束")
		return
	}
	if !t.AssetMismatch {
		abort(c, http.StatusBadRequest, "当前没有待处理的资产异常")
		return
	}
	u := currentUser(c)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Seat{}).Where("id = ?", t.ToSeatID).
			Update("asset_tag", req.AssetTag).Error; err != nil {
			return err
		}
		if err := updateTicket(tx, t.ID, map[string]any{
			"asset_mismatch": false, "asset_tag_reported": req.AssetTag,
		}).Error; err != nil {
			return err
		}
		if err := addEvent(tx, t.ID, u.ID, "asset_resolved",
			fmt.Sprintf("资产台账已更正为 %s", req.AssetTag)); err != nil {
			return err
		}
		return s.maybeReady(tx, t.ID, u.ID)
	})
	if err != nil {
		abort(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.getTicket(c)
}

// ---------- 员工确认（仅限本人） ----------

func (s *Server) employeeConfirm(c *gin.Context) {
	var req struct {
		NetworkOK bool   `json:"network_ok"`
		PhoneOK   bool   `json:"phone_ok"`
		MonitorOK bool   `json:"monitor_ok"`
		Comment   string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		abort(c, http.StatusBadRequest, "请求体非法")
		return
	}
	t, ok := s.loadTicket(c)
	if !ok {
		return
	}
	u := currentUser(c)
	if t.EmployeeID != u.ID {
		abort(c, http.StatusForbidden, "不能替其他员工确认接收")
		return
	}
	// 完成条件：IT 核对全部通过（含网口测试通过）。网口不可用时不能直接结束。
	if t.Status != models.StatusReady || !t.NetworkOK {
		abort(c, http.StatusConflict, "IT 核对未全部通过（需网口测试通过），暂不能确认完成")
		return
	}

	allOK := req.NetworkOK && req.PhoneOK && req.MonitorOK
	now := time.Now()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if !allOK {
			// 员工反馈问题：记录经过并打回给 IT 处理
			if err := updateTicket(tx, t.ID, map[string]any{"status": models.StatusBlocked}).Error; err != nil {
				return err
			}
			return addEvent(tx, t.ID, u.ID, "employee_issue",
				fmt.Sprintf("员工反馈问题：网络=%v 电话=%v 显示器=%v；%s",
					req.NetworkOK, req.PhoneOK, req.MonitorOK, req.Comment))
		}
		// 乐观状态守卫：仅当单仍处于 ready 时才允许完成
		res := tx.Model(&models.Ticket{}).Where("id = ? AND status = ?", t.ID, models.StatusReady).
			Updates(map[string]any{"status": models.StatusCompleted, "completed_at": &now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("搬迁单状态已变化，请刷新后重试")
		}
		// 工位交接：原座位释放，目标座位登记给员工
		if err := tx.Model(&models.Seat{}).Where("id = ?", t.FromSeatID).
			Update("occupant_id", nil).Error; err != nil {
			return err
		}
		upd := tx.Model(&models.Seat{}).Where("id = ? AND occupant_id IS NULL", t.ToSeatID).
			Update("occupant_id", u.ID)
		if upd.Error != nil {
			return upd.Error
		}
		if upd.RowsAffected == 0 {
			return errors.New("目标座位已被占用，无法完成交接")
		}
		if err := addEvent(tx, t.ID, u.ID, "employee_confirmed",
			"员工确认设备与网络可用"); err != nil {
			return err
		}
		return addEvent(tx, t.ID, u.ID, "completed",
			fmt.Sprintf("搬迁完成：%s → %s", t.FromSeat.Code, t.ToSeat.Code))
	})
	if err != nil {
		abort(c, http.StatusConflict, err.Error())
		return
	}
	s.getTicket(c)
}
