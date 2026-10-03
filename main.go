package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"seatmove/internal/app"
	"seatmove/internal/auth"
	"seatmove/internal/mockclient"
	"seatmove/internal/mocksvc"
	"seatmove/internal/store"
)

//go:embed all:web/dist
var distFS embed.FS

func main() {
	addr := env("ADDR", ":8080")
	mockAddr := env("MOCK_ADDR", "127.0.0.1:9090")
	dbPath := env("DB_PATH", "seatmove.db")
	jwtSecret := env("JWT_SECRET", "seatmove-dev-secret")

	// 1) 模拟网络测试 / 分机迁移接口（独立端口）
	mock := mocksvc.New(mockAddr)
	mockSrv := mock.Run()

	// 2) 数据库
	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	store.SyncMockPorts(db, mock)

	mockCli := mockclient.New("http://" + mockAddr)
	svc := app.NewService(db, mockCli)
	h := app.NewHandler(db, svc, auth.NewManager(jwtSecret))

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	h.Register(r)

	// 3) 前端静态文件（Vue 构建产物嵌入二进制）
	dist, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	if hasIndex(dist) {
		fileSrv := http.FileServer(http.FS(dist))
		r.NoRoute(func(c *gin.Context) {
			p := strings.TrimPrefix(c.Request.URL.Path, "/")
			if p == "" {
				p = "index.html"
			}
			if _, err := fs.Stat(dist, p); err != nil {
				// SPA 回退
				c.Request.URL.Path = "/"
			}
			fileSrv.ServeHTTP(c.Writer, c.Request)
		})
	} else {
		r.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api/") {
				c.JSON(404, gin.H{"error": "not found"})
				return
			}
			c.String(200, "前端尚未构建：请在 web/ 目录执行 npm install && npm run build（或直接运行 ./run.sh）")
		})
	}

	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("员工工位搬迁系统启动: http://localhost%s （模拟接口 http://%s）", addr, mockAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务退出: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在关闭...")
	_ = srv.Close()
	_ = mockSrv.Close()
	time.Sleep(200 * time.Millisecond)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func hasIndex(dist fs.FS) bool {
	_, err := fs.Stat(dist, "index.html")
	return err == nil
}
