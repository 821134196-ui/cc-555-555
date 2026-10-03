// Package mocksvc 模拟外部网络测试与电话分机迁移接口。
// 独立 HTTP 端口，真实 HTTP 调用，业务侧不可直接写测试结果，只能调用接口获取。
package mocksvc

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Service struct {
	addr string

	mu          sync.Mutex
	brokenPorts map[string]string // 网口编号 -> 故障原因
	extOwner    map[string]string // 分机 -> 当前占用工单号（模拟占线）
}

func New(addr string) *Service {
	return &Service{
		addr:        addr,
		brokenPorts: map[string]string{},
		extOwner:    map[string]string{},
	}
}

// SetPortEnabled 由业务侧在启动同步/IT 登记故障时调用
func (s *Service) SetPortEnabled(port string, enabled bool, reason string) {
	if port == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled {
		delete(s.brokenPorts, port)
	} else {
		if reason == "" {
			reason = "链路不通"
		}
		s.brokenPorts[port] = reason
	}
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mock/network/test", s.handleNetTest)
	mux.HandleFunc("/mock/network/set", s.handleNetSet)
	mux.HandleFunc("/mock/phone/migrate", s.handleExtMigrate)
	mux.HandleFunc("/mock/phone/release", s.handleExtRelease)
	mux.HandleFunc("/mock/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	return logRequests(mux)
}

func (s *Service) Run() *http.Server {
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("[mock] 模拟网络/分机接口监听 %s", s.addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[mock] 服务退出: %v", err)
		}
	}()
	return srv
}

type netTestReq struct {
	Port string `json:"port"`
}

func (s *Service) handleNetTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req netTestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Port) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "port 必填"})
		return
	}
	s.mu.Lock()
	reason, broken := s.brokenPorts[req.Port]
	s.mu.Unlock()

	time.Sleep(time.Duration(80+rand.Intn(220)) * time.Millisecond) // 模拟测试耗时
	if broken {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         false,
			"port":       req.Port,
			"reason":     reason,
			"latency_ms": 0,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"port":       req.Port,
		"latency_ms": 1 + rand.Intn(8),
		"detail":     "DHCP 正常，网关可达，DNS 解析正常",
	})
}

type netSetReq struct {
	Port    string `json:"port"`
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

// handleNetSet IT 登记/解除网口故障（模拟在网管系统中报障）
func (s *Service) handleNetSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req netSetReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Port == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "port 必填"})
		return
	}
	s.SetPortEnabled(req.Port, req.Enabled, req.Reason)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "port": req.Port, "enabled": req.Enabled})
}

type extMigrateReq struct {
	OrderID string `json:"order_id"`
	FromExt string `json:"from_ext"`
	ToExt   string `json:"to_ext"`
}

func (s *Service) handleExtMigrate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req extMigrateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if strings.TrimSpace(req.ToExt) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "skipped": true, "detail": "目标座位无电话分机，跳过迁移"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 模拟：目标分机正被其他工单占用时失败（释放时由业务侧调用 release）
	if owner, busy := s.extOwner[req.ToExt]; busy && owner != req.OrderID {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"reason": fmt.Sprintf("分机 %s 迁移占用中（工单 %s）", req.ToExt, owner),
		})
		return
	}
	time.Sleep(time.Duration(50+rand.Intn(150)) * time.Millisecond)
	s.extOwner[req.ToExt] = req.OrderID
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"detail": fmt.Sprintf("分机 %s -> %s 迁移成功，拨号测试正常", req.FromExt, req.ToExt),
	})
}

// ReleaseExt 工单取消/改期离开该座位时释放分机占用
func (s *Service) ReleaseExt(orderID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ext, owner := range s.extOwner {
		if owner == orderID {
			delete(s.extOwner, ext)
		}
	}
}

type extReleaseReq struct {
	OrderID string `json:"order_id"`
}

func (s *Service) handleExtRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req extReleaseReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "order_id 必填"})
		return
	}
	s.ReleaseExt(req.OrderID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[mock] %s %s", r.Method, r.URL.Path)
		h.ServeHTTP(w, r)
	})
}
