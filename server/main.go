package main

import (
	"log"
	"os"
	"time"

	"relocation/internal/api"
	"relocation/internal/db"
	"relocation/internal/mock"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dbPath := getenv("DB_PATH", "relocation.db")
	webDist := getenv("WEB_DIST", "web/dist")
	addr := getenv("ADDR", ":8080")

	gdb, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	if err := db.Seed(gdb); err != nil {
		log.Fatalf("写入种子数据失败: %v", err)
	}

	mockSvc := mock.NewService(gdb, 120*time.Millisecond) // 模拟接口网络耗时
	r := api.NewServer(gdb, mockSvc).Router(webDist)

	log.Printf("工位搬迁系统已启动: http://localhost%s", addr)
	log.Printf("演示账号：admin(行政) / li.gong(IT) / zhang.wei(员工)，登录页可直接选择")
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
