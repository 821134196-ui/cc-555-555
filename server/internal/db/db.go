package db

import (
	"fmt"
	"os"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"relocation/internal/models"
)

// Open 打开 SQLite 数据库。busy_timeout 保证并发写入时等待而不是直接报错，
// WAL 模式允许读写并发。
func Open(path string) (*gorm.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	logLevel := gormlogger.Warn
	if os.Getenv("GORM_DEBUG") != "" {
		logLevel = gormlogger.Info
	}
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, err
	}
	// SQLite 单写入者：串行化连接可避免 SQLITE_BUSY，
	// 并发正确性由事务 + 唯一索引保证。
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	return gdb, nil
}

// Migrate 建表，并创建“同一座位/同一员工仅允许一张进行中搬迁单”的部分唯一索引。
// 这是防止并发重复预占的兜底约束：即使两个请求同时通过应用层检查，
// 数据库也会拒绝后到的插入。
func Migrate(gdb *gorm.DB) error {
	if err := gdb.AutoMigrate(&models.User{}, &models.Seat{}, &models.Ticket{}, &models.TicketEvent{}); err != nil {
		return err
	}
	stmts := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tickets_active_seat
		 ON tickets(to_seat_id) WHERE status IN ('pending','ready','blocked')`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tickets_active_employee
		 ON tickets(employee_id) WHERE status IN ('pending','ready','blocked')`,
	}
	for _, s := range stmts {
		if err := gdb.Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}
