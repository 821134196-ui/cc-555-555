package models

import "time"

// 角色
const (
	RoleAdmin    = "admin"    // 行政
	RoleIT       = "it"       // IT 工程师
	RoleEmployee = "employee" // 员工
)

// 搬迁单状态
const (
	StatusPending   = "pending"   // 已创建，等待 IT 完成核对
	StatusReady     = "ready"     // IT 核对全部通过，等待员工搬入并确认
	StatusBlocked   = "blocked"   // 网口测试未通过 / 员工反馈问题，流程受阻
	StatusCompleted = "completed" // 员工确认完成
	StatusCancelled = "cancelled" // 已取消
)

// ActiveStatuses 占用座位预占的状态集合（座位唯一预占索引依赖它）
var ActiveStatuses = []string{StatusPending, StatusReady, StatusBlocked}

type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"uniqueIndex;size:64" json:"username"`
	Name      string    `gorm:"size:64" json:"name"`
	Role      string    `gorm:"size:16;index" json:"role"`
	Extension string    `gorm:"size:16" json:"extension"` // 电话分机号
	Token     string    `gorm:"uniqueIndex;size:64" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

type Seat struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Code       string `gorm:"uniqueIndex;size:32" json:"code"` // 例如 5F-A03
	Floor      int    `gorm:"index" json:"floor"`
	Row        int    `json:"row"`
	Col        int    `json:"col"`
	PortCode   string `gorm:"size:32" json:"port_code"`   // 网口编号
	PortOK     bool   `json:"port_ok"`                    // 网口物理状态（模拟接口据此返回测试结果）
	HasPhone   bool   `json:"has_phone"`                  // 是否有电话端口
	HasMonitor bool   `json:"has_monitor"`                // 是否配显示器
	AssetTag   string `gorm:"size:32" json:"asset_tag"`   // 显示器资产标签（台账）
	OccupantID *uint  `json:"occupant_id"`                // 当前使用人
	Occupant   *User  `gorm:"foreignKey:OccupantID" json:"occupant,omitempty"`
}

type Ticket struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	TicketNo   string `gorm:"uniqueIndex;size:32" json:"ticket_no"`
	EmployeeID uint   `json:"employee_id"`
	Employee   User   `json:"employee"`
	FromSeatID uint   `json:"from_seat_id"`
	FromSeat   Seat   `json:"from_seat"`
	ToSeatID   uint   `json:"to_seat_id"`
	ToSeat     Seat   `json:"to_seat"`
	MoveDate   string `gorm:"size:10" json:"move_date"` // YYYY-MM-DD
	Status     string `gorm:"size:16;index" json:"status"`
	Note       string `gorm:"size:512" json:"note"`

	// IT 核对结果（仅 IT 角色可写）
	NetworkOK        bool       `json:"network_ok"`
	NetworkTestedBy  *uint      `json:"network_tested_by"`
	NetworkTestedAt  *time.Time `json:"network_tested_at"`
	NetworkLatencyMs int64      `json:"network_latency_ms"`
	NetworkMessage   string     `gorm:"size:256" json:"network_message"`
	PhoneOK          bool       `json:"phone_ok"`
	ExtensionNo      string     `gorm:"size:16" json:"extension_no"`
	MonitorOK        bool       `json:"monitor_ok"`
	AssetTagReported string     `gorm:"size:32" json:"asset_tag_reported"`
	AssetMismatch    bool       `json:"asset_mismatch"`

	CreatedByID  uint       `json:"created_by_id"`
	CreatedBy    User       `json:"created_by"`
	CompletedAt  *time.Time `json:"completed_at"`
	CancelledAt  *time.Time `json:"cancelled_at"`
	CancelReason string     `gorm:"size:256" json:"cancel_reason"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	Events []TicketEvent `gorm:"foreignKey:TicketID" json:"events,omitempty"`
}

// TicketEvent 变更经过（审计日志）
type TicketEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TicketID  uint      `gorm:"index" json:"ticket_id"`
	ActorID   uint      `json:"actor_id"`
	Actor     User      `json:"actor"`
	Action    string    `gorm:"size:32" json:"action"`
	Detail    string    `gorm:"size:512" json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}
