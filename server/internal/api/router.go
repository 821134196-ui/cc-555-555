package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"relocation/internal/mock"
	"relocation/internal/models"
)

type Server struct {
	db  *gorm.DB
	mock *mock.Service
}

func NewServer(db *gorm.DB, mockSvc *mock.Service) *Server {
	return &Server{db: db, mock: mockSvc}
}

func (s *Server) Router(webDist string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	if gin.Mode() != gin.TestMode {
		r.Use(gin.Logger())
	}
	r.Use(cors())

	api := r.Group("/api")
	{
		api.GET("/auth/users", s.listLoginUsers) // 登录页用户列表（演示免登录）
		api.POST("/login", s.login)

		auth := api.Group("")
		auth.Use(s.authRequired())
		{
			auth.GET("/me", s.me)
			auth.GET("/users", s.requireRole(models.RoleAdmin, models.RoleIT), s.listUsers)
			auth.GET("/floors", s.listFloors)
			auth.GET("/floors/:floor/seats", s.listFloorSeats)

			auth.GET("/tickets", s.listTickets)
			auth.GET("/tickets/:id", s.getTicket)
			auth.POST("/tickets", s.requireRole(models.RoleAdmin), s.createTicket)
			auth.PATCH("/tickets/:id/reschedule", s.requireRole(models.RoleAdmin), s.rescheduleTicket)
			auth.POST("/tickets/:id/change-seat", s.requireRole(models.RoleAdmin), s.changeSeat)
			auth.POST("/tickets/:id/cancel", s.requireRole(models.RoleAdmin), s.cancelTicket)

			auth.POST("/tickets/:id/it/network-test", s.requireRole(models.RoleIT), s.itNetworkTest)
			auth.POST("/tickets/:id/it/phone-migrate", s.requireRole(models.RoleIT), s.itPhoneMigrate)
			auth.POST("/tickets/:id/it/monitor-check", s.requireRole(models.RoleIT), s.itMonitorCheck)
			auth.POST("/tickets/:id/it/resolve-asset", s.requireRole(models.RoleIT), s.itResolveAsset)

			auth.POST("/tickets/:id/confirm", s.requireRole(models.RoleEmployee), s.employeeConfirm)

			s.mock.RegisterRoutes(auth)
		}
	}

	// 静态资源 + SPA 回退
	r.Static("/assets", webDist+"/assets")
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		c.File(webDist + "/index.html")
	})
	return r
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func abort(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg})
}
