package store

import (
	"fmt"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"seatmove/internal/mocksvc"
	"seatmove/internal/models"
)

// Open 打开 SQLite 并执行自动迁移与种子数据写入
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if err := db.AutoMigrate(
		&models.User{}, &models.Seat{}, &models.MoveOrder{},
		&models.ITCheck{}, &models.MoveEvent{},
	); err != nil {
		return nil, fmt.Errorf("迁移失败: %w", err)
	}
	// SQLite 单写入者：连接池收敛为 1，彻底避免并发事务 SQLITE_BUSY。
	// 座位互斥由条件 UPDATE（WHERE status='free'）保证，与连接串行化无关。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := seed(db); err != nil {
		return nil, err
	}
	return db, nil
}

func seed(db *gorm.DB) error {
	var n int64
	db.Model(&models.User{}).Count(&n)
	if n > 0 {
		return nil
	}
	// 楼层：3F，6x6 格的一部分；8F，4x4
	type seatSpec struct {
		floor, code string
		x, y        int
		net, ext    string
		monitor     bool
		netEnabled  bool
	}
	specs := []seatSpec{}
	for y := 1; y <= 6; y++ {
		for x := 1; x <= 8; x++ {
			code := fmt.Sprintf("F3-%c-%02d", 'A'+y-1, x)
			s := seatSpec{floor: "3F", code: code, x: x, y: y,
				net: fmt.Sprintf("N3-%02d%02d", y, x), monitor: true, netEnabled: true}
			// 安排电话分机：每排前 4 个工位有座机
			if x <= 4 {
				s.ext = fmt.Sprintf("3%02d", y*10+x)
			}
			// 预置两个故障网口，供 IT 核对/修复场景使用
			if code == "F3-C-05" || code == "F3-E-07" {
				s.netEnabled = false
			}
			specs = append(specs, s)
		}
	}
	for y := 1; y <= 4; y++ {
		for x := 1; x <= 4; x++ {
			specs = append(specs, seatSpec{
				floor: "8F", code: fmt.Sprintf("F8-%c-%02d", 'A'+y-1, x), x: x, y: y,
				net: fmt.Sprintf("N8-%02d%02d", y, x), ext: fmt.Sprintf("8%02d", y*10+x),
				monitor: true, netEnabled: true,
			})
		}
	}

	seats := make([]models.Seat, 0, len(specs))
	for _, sp := range specs {
		seats = append(seats, models.Seat{
			Floor: sp.floor, Code: sp.code, X: sp.x, Y: sp.y,
			NetPort: sp.net, PhoneExt: sp.ext, HasMonitor: sp.monitor,
			NetEnabled: sp.netEnabled, Status: models.SeatFree,
		})
	}
	if err := db.Create(&seats).Error; err != nil {
		return err
	}
	// GORM 对带 default:true 的 bool 零值会在 INSERT 时省略，故障网口需显式更新
	for _, sp := range specs {
		if !sp.netEnabled {
			if err := db.Model(&models.Seat{}).Where("code = ?", sp.code).
				Update("net_enabled", false).Error; err != nil {
				return err
			}
		}
	}
	codeID := map[string]uint{}
	for _, s := range seats {
		codeID[s.Code] = s.ID
	}

	// 用户：1 个行政、1 个 IT、若干员工；部分员工已占用 3F 工位
	users := []models.User{
		{Username: "admin", Name: "行政-林晓", Role: models.RoleAdmin},
		{Username: "it", Name: "IT-周工", Role: models.RoleIT},
		{Username: "wangwu", Name: "员工-王五", Role: models.RoleEmployee, AssetTag: "AST-1001", SeatID: ptr(codeID["F3-A-01"])},
		{Username: "zhaoliu", Name: "员工-赵六", Role: models.RoleEmployee, AssetTag: "AST-1002", SeatID: ptr(codeID["F3-A-02"])},
		{Username: "sunqi", Name: "员工-孙七", Role: models.RoleEmployee, AssetTag: "AST-1003", SeatID: ptr(codeID["F3-B-01"])},
		{Username: "chenba", Name: "员工-陈八", Role: models.RoleEmployee, AssetTag: "AST-1004"},
		{Username: "zhoujiu", Name: "员工-周九", Role: models.RoleEmployee, AssetTag: "AST-1005"},
		{Username: "wushi", Name: "员工-吴十", Role: models.RoleEmployee, AssetTag: "AST-1006"},
	}
	if err := db.Create(&users).Error; err != nil {
		return err
	}

	// 标记已占用座位
	occ := map[string]bool{"F3-A-01": true, "F3-A-02": true, "F3-B-01": true}
	for code := range occ {
		var u models.User
		if err := db.Where("username = ?", map[string]string{
			"F3-A-01": "wangwu", "F3-A-02": "zhaoliu", "F3-B-01": "sunqi",
		}[code]).First(&u).Error; err != nil {
			return err
		}
		if err := db.Model(&models.Seat{}).Where("id = ?", codeID[code]).
			Updates(map[string]any{"status": models.SeatOccupied, "occupant_id": u.ID}).Error; err != nil {
			return err
		}
	}

	// 一条历史已完成单（王五曾搬到 A-01），让时间线有数据
	from := codeID["F3-A-03"]
	to := codeID["F3-A-01"]
	mv := time.Date(2026, 9, 20, 10, 0, 0, 0, time.Local)
	ct := time.Now().Add(-72 * time.Hour)
	order := models.MoveOrder{
		EmployeeID: users[2].ID, FromSeatID: &from, ToSeatID: to,
		MoveDate: mv, Status: models.StatusConfirmed, CreatedByID: users[0].ID,
		CreatedAt: ct, UpdatedAt: ct,
	}
	if err := db.Create(&order).Error; err != nil {
		return err
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// SyncMockPorts 把数据库里网口的启用状态同步到模拟接口
func SyncMockPorts(db *gorm.DB, svc *mocksvc.Service) {
	var seats []models.Seat
	db.Where("net_port <> ''").Find(&seats)
	for _, s := range seats {
		if !s.NetEnabled {
			svc.SetPortEnabled(s.NetPort, false, "配线架链路断开，等待维修")
		}
	}
}
