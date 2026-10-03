// Package mock 提供网络测试与分机迁移的模拟接口。
// 真实环境中它们对应机房交换机端口测试与 PBX 分机迁移系统，
// 这里用确定性逻辑模拟：结果取决于座位表中的端口/话机配置。
package mock

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"relocation/internal/models"
)

type PortResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latency_ms"`
	Message   string `json:"message"`
}

type ExtResult struct {
	OK        bool   `json:"ok"`
	Extension string `json:"extension"`
	Message   string `json:"message"`
}

type Service struct {
	db      *gorm.DB
	latency time.Duration // 模拟接口耗时
}

func NewService(db *gorm.DB, latency time.Duration) *Service {
	return &Service{db: db, latency: latency}
}

// TestNetworkPort 模拟对网口打测：端口存在且物理状态正常则通过。
func (s *Service) TestNetworkPort(portCode string) PortResult {
	if s.latency > 0 {
		time.Sleep(s.latency)
	}
	var seat models.Seat
	if err := s.db.Where("port_code = ?", portCode).First(&seat).Error; err != nil {
		return PortResult{OK: false, Message: "网口编号不存在: " + portCode}
	}
	if !seat.PortOK {
		return PortResult{OK: false, LatencyMs: int64(s.latency / time.Millisecond),
			Message: "端口无链路（link down），需检查配线架或更换端口"}
	}
	return PortResult{OK: true, LatencyMs: int64(s.latency / time.Millisecond),
		Message: "链路正常 1000Mbps 全双工，DHCP 可达"}
}

// MigrateExtension 模拟把员工分机迁移到目标座位的电话端口。
func (s *Service) MigrateExtension(extension string, toSeatID uint) ExtResult {
	if s.latency > 0 {
		time.Sleep(s.latency)
	}
	var seat models.Seat
	if err := s.db.First(&seat, toSeatID).Error; err != nil {
		return ExtResult{OK: false, Message: "目标座位不存在"}
	}
	if !seat.HasPhone {
		return ExtResult{OK: false, Message: "目标座位 " + seat.Code + " 无电话端口，无法迁移分机"}
	}
	return ExtResult{OK: true, Extension: extension,
		Message: "分机 " + extension + " 已迁移至 " + seat.Code}
}

// RegisterRoutes 把模拟接口同时以 HTTP 端点暴露，便于演示与联调。
func (s *Service) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/mock/network-test", func(c *gin.Context) {
		var req struct {
			PortCode string `json:"port_code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "port_code 必填"})
			return
		}
		c.JSON(http.StatusOK, s.TestNetworkPort(req.PortCode))
	})
	rg.POST("/mock/extension-migrate", func(c *gin.Context) {
		var req struct {
			Extension string `json:"extension" binding:"required"`
			ToSeatID  uint   `json:"to_seat_id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "extension 与 to_seat_id 必填"})
			return
		}
		c.JSON(http.StatusOK, s.MigrateExtension(req.Extension, req.ToSeatID))
	})
}
