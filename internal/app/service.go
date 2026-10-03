package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"seatmove/internal/mockclient"
	"seatmove/internal/models"
)

// 业务错误（handler 据 Code 映射 HTTP 状态）
type BizError struct {
	Status int
	Msg    string
}

func (e *BizError) Error() string { return e.Msg }

func bizError(status int, format string, args ...any) error {
	return &BizError{Status: status, Msg: fmt.Sprintf(format, args...)}
}

func ErrStatus(err error) (int, string) {
	var be *BizError
	if errors.As(err, &be) {
		return be.Status, be.Msg
	}
	return 500, err.Error()
}

type Service struct {
	db   *gorm.DB
	mock *mockclient.Client
}

func NewService(db *gorm.DB, mock *mockclient.Client) *Service {
	return &Service{db: db, mock: mock}
}

// ---------- 查询 ----------

func (s *Service) ListSeats(floor string) ([]models.Seat, error) {
	q := s.db.Model(&models.Seat{}).Preload("Occupant")
	if floor != "" {
		q = q.Where("floor = ?", floor)
	}
	var seats []models.Seat
	err := q.Order("floor, y, x").Find(&seats).Error
	return seats, err
}

func (s *Service) Floors() ([]string, error) {
	var floors []string
	err := s.db.Model(&models.Seat{}).Distinct().Order("floor").Pluck("floor", &floors).Error
	return floors, err
}

type OrderFilter struct {
	Status     string
	EmployeeID uint
	MyOnly     bool
}

func (s *Service) ListOrders(f OrderFilter) ([]models.MoveOrder, error) {
	q := s.db.Model(&models.MoveOrder{}).
		Preload("Employee").Preload("FromSeat").Preload("ToSeat").
		Preload("CheckedBy")
	switch {
	case f.MyOnly || f.EmployeeID != 0:
		q = q.Where("employee_id = ?", f.EmployeeID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	var orders []models.MoveOrder
	err := q.Order("updated_at DESC, id DESC").Find(&orders).Error
	return orders, err
}

func (s *Service) GetOrder(id uint) (*models.MoveOrder, []models.MoveEvent, error) {
	var o models.MoveOrder
	err := s.db.Preload("Employee").Preload("FromSeat").Preload("ToSeat").
		Preload("CheckedBy").Preload("ITChecks").
		First(&o, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, bizError(404, "搬迁单不存在")
	}
	if err != nil {
		return nil, nil, err
	}
	var events []models.MoveEvent
	if err := s.db.Where("order_id = ?", id).Order("id DESC").Find(&events).Error; err != nil {
		return nil, nil, err
	}
	return &o, events, nil
}

func (s *Service) ListEmployees() ([]models.User, error) {
	var us []models.User
	err := s.db.Where("role = ?", models.RoleEmployee).Order("id").Find(&us).Error
	return us, err
}

// ActiveOrderBySeat 返回某座位当前进行中的搬迁单（预占座位可由此跳到单据）
func (s *Service) ActiveOrderBySeat(seatID uint) (*models.MoveOrder, error) {
	var o models.MoveOrder
	err := s.db.Preload("Employee").
		Where("to_seat_id = ? AND status IN ?", seatID,
			[]string{models.StatusReserved, models.StatusITChecked}).
		Order("id DESC").First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// ---------- 行政：创建 ----------

type CreateOrderInput struct {
	EmployeeID uint
	FromSeatID uint // 0 表示取员工登记工位
	ToSeatID   uint
	MoveDate   time.Time
}

// reserveSeat 条件更新：仅当座位仍为 free 时才能预占成功。
// 并发两单抢同一座位时，数据库只放行一个（SQLite 写串行 + 条件 WHERE 双保险）。
func reserveSeat(tx *gorm.DB, seatID uint) error {
	res := tx.Model(&models.Seat{}).
		Where("id = ? AND status = ?", seatID, models.SeatFree).
		Update("status", models.SeatReserved)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return bizError(409, "目标座位已被占用或预占，无法分配")
	}
	return nil
}

func releaseSeat(tx *gorm.DB, seatID uint) error {
	return tx.Model(&models.Seat{}).Where("id = ?", seatID).
		Update("status", models.SeatFree).Error
}

func (s *Service) CreateOrder(actor *models.User, in CreateOrderInput) (*models.MoveOrder, error) {
	if in.MoveDate.IsZero() {
		return nil, bizError(400, "搬迁日期必填")
	}
	var emp models.User
	if err := s.db.First(&emp, in.EmployeeID).Error; err != nil {
		return nil, bizError(400, "员工不存在")
	}
	if emp.Role != models.RoleEmployee {
		return nil, bizError(400, "搬迁对象必须是员工")
	}
	var to models.Seat
	if err := s.db.First(&to, in.ToSeatID).Error; err != nil {
		return nil, bizError(400, "目标座位不存在")
	}

	fromID := in.FromSeatID
	if fromID == 0 && emp.SeatID != nil {
		fromID = *emp.SeatID
	}

	var order *models.MoveOrder
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 同一员工不能同时有两张进行中的搬迁单
		var cnt int64
		if err := tx.Model(&models.MoveOrder{}).
			Where("employee_id = ? AND status IN ?", emp.ID,
				[]string{models.StatusReserved, models.StatusITChecked}).
			Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			return bizError(409, "该员工已有进行中的搬迁单，请先处理")
		}
		// 不允许搬到自己当前工位
		if emp.SeatID != nil && *emp.SeatID == in.ToSeatID {
			return bizError(400, "目标座位就是该员工当前工位")
		}
		if err := reserveSeat(tx, in.ToSeatID); err != nil {
			return err
		}
		o := models.MoveOrder{
			EmployeeID: emp.ID, ToSeatID: in.ToSeatID, MoveDate: in.MoveDate,
			Status: models.StatusReserved, CreatedByID: actor.ID,
		}
		if fromID != 0 {
			o.FromSeatID = &fromID
		}
		if err := tx.Create(&o).Error; err != nil {
			return err
		}
		if err := addEvent(tx, o.ID, actor, "created",
			fmt.Sprintf("创建搬迁单：%s %s → %s，搬迁日期 %s",
				emp.Name, seatName(tx, fromID), to.Code, in.MoveDate.Format("2006-01-02"))); err != nil {
			return err
		}
		order = &o
		return nil
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

// ---------- 行政：改期 / 换座 ----------

type RescheduleInput struct {
	NewMoveDate time.Time // 零值表示不改日期
	NewToSeatID uint      // 0 表示不换座位
}

func (s *Service) Reschedule(actor *models.User, orderID uint, in RescheduleInput) error {
	var o models.MoveOrder
	if err := s.db.First(&o, orderID).Error; err != nil {
		return bizError(404, "搬迁单不存在")
	}
	if o.Status != models.StatusReserved && o.Status != models.StatusITChecked {
		return bizError(409, "搬迁单已结束，不能改期")
	}
	seatChanged := in.NewToSeatID != 0 && in.NewToSeatID != o.ToSeatID

	var oldTo models.Seat
	_ = s.db.First(&oldTo, o.ToSeatID).Error
	newDate := o.MoveDate
	if !in.NewMoveDate.IsZero() {
		newDate = in.NewMoveDate
	}
	var newTo models.Seat
	if seatChanged {
		if err := s.db.First(&newTo, in.NewToSeatID).Error; err != nil {
			return bizError(400, "新目标座位不存在")
		}
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		if seatChanged {
			// 先抢新座，抢不到整体回滚，旧预占不受影响
			if err := reserveSeat(tx, in.NewToSeatID); err != nil {
				return err
			}
			if err := releaseSeat(tx, o.ToSeatID); err != nil {
				return err
			}
			o.ToSeatID = in.NewToSeatID
		}
		o.MoveDate = newDate
		// 座位或日期变化后，此前 IT 核对结论失效，重置为待核对（历史记录保留）
		resetITResult(&o)
		o.Status = models.StatusReserved
		if err := tx.Save(&o).Error; err != nil {
			return err
		}
		detail := fmt.Sprintf("改期为 %s", newDate.Format("2006-01-02"))
		if seatChanged {
			detail = fmt.Sprintf("目标座位 %s 调整为 %s，搬迁日期 %s；原预占已释放，IT 核对结果作废需重新核对",
				oldTo.Code, newTo.Code, newDate.Format("2006-01-02"))
		}
		return addEvent(tx, o.ID, actor, "rescheduled", detail)
	})
	if err != nil {
		return err
	}
	if seatChanged {
		// 原座位的分机迁移占用一并释放
		s.mock.ReleaseExt(fmt.Sprintf("%d", orderID))
	}
	return nil
}

// ---------- 取消 ----------

func (s *Service) Cancel(actor *models.User, orderID uint, reason string) error {
	var o models.MoveOrder
	if err := s.db.First(&o, orderID).Error; err != nil {
		return bizError(404, "搬迁单不存在")
	}
	if o.Status == models.StatusCancelled || o.Status == models.StatusConfirmed {
		return bizError(409, "搬迁单已结束，无需取消")
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := releaseSeat(tx, o.ToSeatID); err != nil {
			return err
		}
		o.Status = models.StatusCancelled
		if err := tx.Save(&o).Error; err != nil {
			return err
		}
		detail := "取消搬迁，目标座位预占已释放"
		if reason != "" {
			detail += "；原因：" + reason
		}
		return addEvent(tx, o.ID, actor, "cancelled", detail)
	})
	if err != nil {
		return err
	}
	s.mock.ReleaseExt(fmt.Sprintf("%d", orderID))
	return nil
}

// ---------- IT：核对 ----------

type ITCheckInput struct {
	MonitorOK bool
	// 现场看到的资产标签；留空表示未发现标签（视为不符）
	ActualAssetTag string
	Note           string
}

type ITCheckOutput struct {
	Passed bool
	Order  *models.MoveOrder
}

// ITCheck 调用模拟网络测试与分机迁移，结合显示器/资产标签核对决定是否通过。
// 无论是否通过都写入一条 ITCheck 留痕。
func (s *Service) ITCheck(ctx context.Context, actor *models.User, orderID uint, in ITCheckInput) (*ITCheckOutput, error) {
	var o models.MoveOrder
	if err := s.db.First(&o, orderID).Error; err != nil {
		return nil, bizError(404, "搬迁单不存在")
	}
	if o.Status != models.StatusReserved && o.Status != models.StatusITChecked {
		return nil, bizError(409, "搬迁单已结束，无法核对")
	}
	var emp models.User
	if err := s.db.First(&emp, o.EmployeeID).Error; err != nil {
		return nil, err
	}
	var seat models.Seat
	if err := s.db.First(&seat, o.ToSeatID).Error; err != nil {
		return nil, err
	}

	// 网口物理不可用（座位未布线或已登记故障）：直接拦截，且不允许结束搬迁单
	if seat.NetPort == "" {
		return nil, s.recordFailedCheck(ctx, actor, &o, &emp, &seat, in,
			false, false, "目标座位无网口", "", "", "座位无网口")
	}
	if !seat.NetEnabled {
		return nil, s.recordFailedCheck(ctx, actor, &o, &emp, &seat, in,
			false, false, "网口已登记故障，等待维修", "", "", "网口故障")
	}

	// 1) 模拟网络测试 —— 结果只能来自模拟接口，IT 不能手填
	net, err := s.mock.NetworkTest(ctx, seat.NetPort)
	if err != nil {
		return nil, bizError(502, "网络测试服务异常: %v", err)
	}

	// 2) 模拟分机迁移（原分机 → 目标分机；无目标分机则跳过）
	var fromExt, toExt string
	if emp.SeatID != nil {
		var fs models.Seat
		if err := s.db.First(&fs, *emp.SeatID).Error; err == nil {
			fromExt = fs.PhoneExt
		}
	}
	toExt = seat.PhoneExt
	ext, err := s.mock.MigrateExt(ctx, fmt.Sprintf("%d", o.ID), fromExt, toExt)
	if err != nil {
		return nil, bizError(502, "分机迁移服务异常: %v", err)
	}

	assetMatch := in.ActualAssetTag != "" && in.ActualAssetTag == emp.AssetTag
	assetNote := ""
	if !assetMatch {
		assetNote = fmt.Sprintf("资产标签不符：期望 %s，现场 %q",
			emp.AssetTag, in.ActualAssetTag)
	}

	passed := net.OK && (ext.OK || ext.Skipped) && in.MonitorOK && assetMatch

	now := time.Now()
	record := models.ITCheck{
		OrderID: o.ID, CheckedByID: actor.ID, CheckedAt: now,
		NetTested: net.OK, NetDetail: firstNonEmpty(net.Detail, net.Reason),
		ExtMigrated: ext.OK && !ext.Skipped,
		ExtDetail:   firstNonEmpty(ext.Detail, ext.Reason),
		MonitorOK:   in.MonitorOK, AssetMatch: assetMatch,
		ExpectedTag: emp.AssetTag, ActualTag: in.ActualAssetTag,
		Passed: passed, Note: in.Note,
	}
	if err := s.db.Create(&record).Error; err != nil {
		return nil, err
	}

	if !passed {
		// 核对不通过：单据保持 reserved，并记录具体原因事件（含资产标签不符）
		o.Status = models.StatusReserved
		applyITResult(&o, actor.ID, now, false, false, in.MonitorOK, assetMatch, in.Note)
		if err := s.db.Save(&o).Error; err != nil {
			return nil, err
		}
		detail := buildFailDetail(net, ext, in.MonitorOK, assetMatch, assetNote)
		action := "it_check_failed"
		if !assetMatch {
			action = "asset_mismatch"
		} else if !net.OK {
			action = "net_failed"
		}
		if err := addEvent(s.db, o.ID, actor, action, detail); err != nil {
			return nil, err
		}
		return &ITCheckOutput{Passed: false}, bizError(422, "核对未通过：%s", detail)
	}

	// 通过：it_checked；网络测试通过是硬条件
	o.Status = models.StatusITChecked
	applyITResult(&o, actor.ID, now, true, ext.OK && !ext.Skipped, in.MonitorOK, true, in.Note)
	if err := s.db.Save(&o).Error; err != nil {
		return nil, err
	}
	detail := fmt.Sprintf("IT 核对通过：网口 %s 网络测试通过（%s）；%s；显示器齐备；资产标签 %s 一致",
		seat.NetPort, net.Detail, extDetailText(ext, toExt), emp.AssetTag)
	if err := addEvent(s.db, o.ID, actor, "it_checked", detail); err != nil {
		return nil, err
	}
	if err := s.db.First(&o, o.ID).Error; err != nil {
		return nil, err
	}
	return &ITCheckOutput{Passed: true, Order: &o}, nil
}

// recordFailedCheck 在根本无法测试（无网口/网口故障）时也留下 IT 核对记录
func (s *Service) recordFailedCheck(ctx context.Context, actor *models.User, o *models.MoveOrder,
	emp *models.User, seat *models.Seat, in ITCheckInput,
	netOK, extOK bool, netDetail, extDetail, expected, actual string) error {
	now := time.Now()
	rec := models.ITCheck{
		OrderID: o.ID, CheckedByID: actor.ID, CheckedAt: now,
		NetTested: false, NetDetail: netDetail,
		ExtMigrated: false, ExtDetail: extDetail,
		MonitorOK: in.MonitorOK, AssetMatch: false,
		ExpectedTag: expected, ActualTag: actual,
		Passed: false, Note: in.Note,
	}
	if err := s.db.Create(&rec).Error; err != nil {
		return err
	}
	o.Status = models.StatusReserved
	applyITResult(o, actor.ID, now, false, false, in.MonitorOK, false, in.Note)
	if err := s.db.Save(o).Error; err != nil {
		return err
	}
	_ = addEvent(s.db, o.ID, actor, "net_failed",
		fmt.Sprintf("IT 核对阻断：座位 %s %s，搬迁单不可结束", seat.Code, netDetail))
	return bizError(422, "网口不可用：%s；不能结束搬迁单", netDetail)
}

// ---------- IT：登记/解除网口故障 ----------

func (s *Service) SetSeatNetEnabled(actor *models.User, seatID uint, enabled bool, reason string) error {
	var seat models.Seat
	if err := s.db.First(&seat, seatID).Error; err != nil {
		return bizError(404, "座位不存在")
	}
	if seat.NetPort == "" {
		return bizError(400, "该座位没有网口")
	}
	if err := s.mock.SetPort(context.Background(), seat.NetPort, enabled, reason); err != nil {
		return bizError(502, "模拟网管接口异常: %v", err)
	}
	if err := s.db.Model(&seat).Update("net_enabled", enabled).Error; err != nil {
		return err
	}
	action := "net_port_fault"
	verb := "登记网口故障"
	if enabled {
		action, verb = "net_port_repaired", "解除网口故障"
	}
	// 关联到以该座位为目标的进行中单据
	var orders []models.MoveOrder
	s.db.Where("to_seat_id = ? AND status = ?", seatID, models.StatusReserved).Find(&orders)
	for _, o := range orders {
		detail := fmt.Sprintf("%s：座位 %s 网口 %s", verb, seat.Code, seat.NetPort)
		if reason != "" {
			detail += "；说明：" + reason
		}
		if !enabled {
			detail += "；相关搬迁单需待修复并重新核对"
		}
		_ = addEvent(s.db, o.ID, actor, action, detail)
		// 故障后原核对结论作废
		if !enabled {
			resetITResult(&o)
			_ = s.db.Save(&o)
		}
	}
	return nil
}

// ---------- 员工：确认接收 ----------

type ConfirmInput struct {
	NetUsable    bool
	DeviceUsable bool
	Note         string
}

// Confirm 仅允许搬迁单上的员工本人调用；完成条件：IT 已核对且网络测试通过 + 本人确认网络与设备均可用。
func (s *Service) Confirm(actor *models.User, orderID uint, in ConfirmInput) (*models.MoveOrder, error) {
	var o models.MoveOrder
	if err := s.db.First(&o, orderID).Error; err != nil {
		return nil, bizError(404, "搬迁单不存在")
	}
	// 员工不能替其他人确认接收
	if o.EmployeeID != actor.ID {
		return nil, bizError(403, "只能由搬迁单上的员工本人确认")
	}
	if o.Status == models.StatusConfirmed {
		return nil, bizError(409, "搬迁单已确认完成")
	}
	if o.Status == models.StatusCancelled {
		return nil, bizError(409, "搬迁单已取消")
	}
	if o.Status != models.StatusITChecked {
		return nil, bizError(422, "IT 尚未核对通过，不能确认")
	}
	// 网络测试通过是完成的硬性条件
	if !o.NetTested || o.CheckedAt == nil {
		return nil, bizError(422, "网络测试未通过，不能完成搬迁")
	}
	if !in.NetUsable || !in.DeviceUsable {
		return nil, bizError(400, "需确认网络与设备均可用后才能完成")
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		o.Status = models.StatusConfirmed
		o.ConfirmedByID = &actor.ID
		o.ConfirmedAt = &now
		o.NetUsable = true
		o.DeviceUsable = true
		o.ConfirmNote = in.Note
		if err := tx.Save(&o).Error; err != nil {
			return err
		}
		// 目标座位：预占 → 占用
		res := tx.Model(&models.Seat{}).
			Where("id = ? AND status = ?", o.ToSeatID, models.SeatReserved).
			Updates(map[string]any{"status": models.SeatOccupied, "occupant_id": actor.ID})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return bizError(409, "目标座位预占状态异常，请联系行政")
		}
		// 原座位：腾空；员工登记工位更新
		if o.FromSeatID != nil {
			if err := tx.Model(&models.Seat{}).Where("id = ?", o.FromSeatID).
				Updates(map[string]any{"status": models.SeatFree, "occupant_id": nil}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&models.User{}).Where("id = ?", actor.ID).
			Update("seat_id", o.ToSeatID).Error; err != nil {
			return err
		}
		return addEvent(tx, o.ID, actor, "confirmed",
			"员工本人现场确认：网络可用、设备可用，搬迁完成")
	})
	if err != nil {
		return nil, err
	}
	// 搬迁完成，分机迁移会话结束（分机已投入正常使用），释放迁移锁
	s.mock.ReleaseExt(fmt.Sprintf("%d", orderID))
	_ = s.db.First(&o, o.ID)
	return &o, nil
}

// ---------- 辅助 ----------

func addEvent(tx *gorm.DB, orderID uint, actor *models.User, action, detail string) error {
	return tx.Create(&models.MoveEvent{
		OrderID: orderID, ActorID: actor.ID, ActorName: actor.Name,
		Action: action, Detail: detail,
	}).Error
}

func applyITResult(o *models.MoveOrder, by uint, at time.Time, net, ext, monitor, asset bool, note string) {
	o.CheckedByID = &by
	o.CheckedAt = &at
	o.NetTested = net
	o.ExtMigrated = ext
	o.MonitorOK = monitor
	o.AssetMatch = asset
	o.ITNote = note
}

func resetITResult(o *models.MoveOrder) {
	o.CheckedByID = nil
	o.CheckedAt = nil
	o.NetTested = false
	o.ExtMigrated = false
	o.MonitorOK = false
	o.AssetMatch = false
	o.ITNote = ""
}

func seatName(tx *gorm.DB, id uint) string {
	if id == 0 {
		return "（未登记工位）"
	}
	var st models.Seat
	if err := tx.Select("code").First(&st, id).Error; err != nil {
		return fmt.Sprintf("#%d", id)
	}
	return st.Code
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func extDetailText(ext mockclient.ExtResult, toExt string) string {
	if ext.Skipped || toExt == "" {
		return "目标座位无分机，已跳过迁移"
	}
	return "分机迁移完成"
}

func buildFailDetail(net mockclient.NetResult, ext mockclient.ExtResult, monitor, asset bool, assetNote string) string {
	var parts []string
	if !net.OK {
		parts = append(parts, fmt.Sprintf("网络测试未通过（%s）", firstNonEmpty(net.Reason, net.Detail)))
	}
	if !ext.OK && !ext.Skipped {
		parts = append(parts, fmt.Sprintf("分机迁移失败（%s）", ext.Reason))
	}
	if !monitor {
		parts = append(parts, "显示器未就位")
	}
	if !asset {
		parts = append(parts, assetNote)
	}
	if len(parts) == 0 {
		return "未知原因"
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "；"
		}
		out += p
	}
	return out
}
