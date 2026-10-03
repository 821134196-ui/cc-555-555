package models

import "time"

// 角色常量
const (
	RoleAdmin    = "admin"    // 行政
	RoleIT       = "it"       // IT
	RoleEmployee = "employee" // 员工
)

// 座位状态：座位是否被物理占用
const (
	SeatFree     = "free"     // 可用
	SeatReserved = "reserved" // 预占（存在进行中的搬迁单）
	SeatOccupied = "occupied" // 已占用
)

// 搬迁单状态机：
//
//	reserved  -> it_checked -> confirmed（完成）
//	reserved  -> cancelled（取消，释放预占）
//	it_checked -> reschedule（回到 reserved，释放旧预占后重新预占新座位/日期）
//	任意进行态 -> cancelled
const (
	StatusReserved  = "reserved"   // 行政已创建，目标座位预占
	StatusITChecked = "it_checked" // IT 已核对通过（网络测试通过、资产标签一致）
	StatusConfirmed = "confirmed"  // 员工本人确认设备与网络可用，搬迁完成
	StatusCancelled = "cancelled"  // 已取消，预占已释放
)

// User 系统用户：行政、IT、员工
type User struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Username string `gorm:"uniqueIndex;size:64;not null" json:"username"`
	Name     string `gorm:"size:64;not null" json:"name"`
	Role     string `gorm:"size:16;not null" json:"role"`
	// AssetTag 员工名下设备登记的资产标签（IT 现场核对时的期望值）
	AssetTag string `gorm:"size:64" json:"asset_tag"`
	// 当前实际工位（员工用）
	SeatID *uint `json:"seat_id"`
	Seat   *Seat `gorm:"foreignKey:SeatID" json:"seat,omitempty"`
}

// Seat 工位/桌位
type Seat struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Floor      string `gorm:"size:32;not null;index" json:"floor"`
	Code       string `gorm:"size:32;not null;uniqueIndex" json:"code"` // 如 F3-A-012
	X          int    `json:"x"`                                        // 楼层图坐标（格）
	Y          int    `json:"y"`
	NetPort    string `gorm:"size:32" json:"net_port"`  // 网口编号，空表示无网口
	PhoneExt   string `gorm:"size:16" json:"phone_ext"` // 电话分机
	HasMonitor bool   `gorm:"default:false" json:"has_monitor"`
	// NetEnabled 网口是否可用（物理线路状态，IT 可在现场登记故障）
	NetEnabled bool   `gorm:"default:true" json:"net_enabled"`
	Status     string `gorm:"size:16;not null;default:free" json:"status"` // free/reserved/occupied
	// OccupantID 当前占用该座位的员工（status=occupied 时有效）
	OccupantID *uint `json:"occupant_id"`
	Occupant   *User `gorm:"foreignKey:OccupantID" json:"occupant,omitempty"`
}

// MoveOrder 搬迁单 —— 行政、IT、员工协作的同一张单
type MoveOrder struct {
	ID uint `gorm:"primaryKey" json:"id"`

	EmployeeID uint `gorm:"index;not null" json:"employee_id"`
	Employee   User `gorm:"foreignKey:EmployeeID" json:"employee"`

	FromSeatID *uint `json:"from_seat_id"`
	FromSeat   *Seat `gorm:"foreignKey:FromSeatID" json:"from_seat,omitempty"`

	ToSeatID uint `gorm:"index;not null" json:"to_seat_id"`
	ToSeat   Seat `gorm:"foreignKey:ToSeatID" json:"to_seat"`

	MoveDate time.Time `gorm:"not null;index" json:"move_date"`

	Status string `gorm:"size:16;not null;default:reserved;index" json:"status"`

	// IT 核对结果（仅 IT 可写）
	CheckedByID *uint      `json:"checked_by_id"`
	CheckedBy   *User      `gorm:"foreignKey:CheckedByID" json:"checked_by,omitempty"`
	CheckedAt   *time.Time `json:"checked_at"`
	NetTested   bool       `json:"net_tested"`   // 网络测试是否通过（模拟接口返回）
	ExtMigrated bool       `json:"ext_migrated"` // 分机是否已迁移（模拟接口返回）
	MonitorOK   bool       `json:"monitor_ok"`   // 显示器核对
	AssetMatch  bool       `json:"asset_match"`  // 资产标签是否相符
	ITNote      string     `gorm:"size:512" json:"it_note"`
	ITChecks    []ITCheck  `gorm:"foreignKey:OrderID" json:"it_checks,omitempty"`

	// 员工确认（仅本人可写）
	ConfirmedByID *uint      `json:"confirmed_by_id"`
	ConfirmedAt   *time.Time `json:"confirmed_at"`
	NetUsable     bool       `json:"net_usable"`    // 员工侧网络可用
	DeviceUsable  bool       `json:"device_usable"` // 设备可用
	ConfirmNote   string     `gorm:"size:512" json:"confirm_note"`

	CreatedByID uint      `json:"created_by_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ITCheck 每次 IT 核对尝试都留痕（包括不通过的），记录模拟接口原始结果
type ITCheck struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	OrderID     uint      `gorm:"index;not null" json:"order_id"`
	CheckedByID uint      `json:"checked_by_id"`
	CheckedAt   time.Time `json:"checked_at"`
	NetTested   bool      `json:"net_tested"`
	NetDetail   string    `gorm:"size:256" json:"net_detail"`
	ExtMigrated bool      `json:"ext_migrated"`
	ExtDetail   string    `gorm:"size:256" json:"ext_detail"`
	MonitorOK   bool      `json:"monitor_ok"`
	AssetMatch  bool      `json:"asset_match"`
	ExpectedTag string    `gorm:"size:64" json:"expected_tag"`
	ActualTag   string    `gorm:"size:64" json:"actual_tag"`
	Passed      bool      `json:"passed"`
	Note        string    `gorm:"size:512" json:"note"`
}

// MoveEvent 变更经过：改期、取消、资产标签不符、状态流转全程留痕
type MoveEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrderID   uint      `gorm:"index;not null" json:"order_id"`
	ActorID   uint      `json:"actor_id"`
	ActorName string    `gorm:"size:64" json:"actor_name"`
	Action    string    `gorm:"size:32;not null" json:"action"` // created/rescheduled/cancelled/it_checked/asset_mismatch/confirmed/...
	Detail    string    `gorm:"size:1024" json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}
