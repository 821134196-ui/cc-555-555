package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"relocation/internal/api"
	"relocation/internal/db"
	"relocation/internal/mock"
	"relocation/internal/models"
)

const (
	tokAdmin = "tok-admin"
	tokIT    = "tok-it1"
	tokEmp1  = "tok-emp1"
	tokEmp2  = "tok-emp2"
)

// testEnv 测试环境：真实路由 + 临时文件 SQLite（支持并发）
type testEnv struct {
	router *gin.Engine
	db     *gorm.DB
	// 种子数据 ID
	adminID, itID     uint
	emp1ID, emp2ID    uint
	emp1Seat, emp2Seat uint // 员工当前座位
	goodSeat  uint // 空闲、网口正常
	badSeat   uint // 空闲、网口故障
	noPhoneSeat uint // 空闲、无电话端口
	extraSeat uint   // 空闲备用
}

func setup(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	gdb, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatal(err)
	}

	mkUser := func(name, role, token, ext string) models.User {
		u := models.User{Username: name, Name: name, Role: role, Token: token, Extension: ext}
		if err := gdb.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		return u
	}
	admin := mkUser("admin", models.RoleAdmin, tokAdmin, "")
	it := mkUser("it1", models.RoleIT, tokIT, "")
	emp1 := mkUser("emp1", models.RoleEmployee, tokEmp1, "6001")
	emp2 := mkUser("emp2", models.RoleEmployee, tokEmp2, "6002")

	mkSeat := func(code string, portOK, hasPhone bool) models.Seat {
		s := models.Seat{Code: code, Floor: 5, PortCode: "P" + code,
			PortOK: portOK, HasPhone: hasPhone, HasMonitor: true, AssetTag: "MON-" + code}
		if err := gdb.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
		return s
	}
	emp1Seat := mkSeat("5F-A01", true, true)
	emp2Seat := mkSeat("5F-A02", true, true)
	good := mkSeat("5F-B01", true, true)
	bad := mkSeat("5F-B02", false, true)      // 网口故障
	noPhone := mkSeat("5F-B03", true, false)  // 无电话端口
	extra := mkSeat("5F-B04", true, true)

	gdb.Model(&emp1Seat).Update("occupant_id", emp1.ID)
	gdb.Model(&emp2Seat).Update("occupant_id", emp2.ID)

	webDist := filepath.Join(dir, "dist")
	os.MkdirAll(filepath.Join(webDist, "assets"), 0o755)
	os.WriteFile(filepath.Join(webDist, "index.html"), []byte("<html></html>"), 0o644)

	mockSvc := mock.NewService(gdb, 0)
	router := api.NewServer(gdb, mockSvc).Router(webDist)
	return &testEnv{
		router: router, db: gdb,
		adminID: admin.ID, itID: it.ID,
		emp1ID: emp1.ID, emp2ID: emp2.ID,
		emp1Seat: emp1Seat.ID, emp2Seat: emp2Seat.ID,
		goodSeat: good.ID, badSeat: bad.ID, noPhoneSeat: noPhone.ID, extraSeat: extra.ID,
	}
}

func do(t *testing.T, env *testEnv, method, path, token string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	var parsed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	return rec, parsed
}

func createTicket(t *testing.T, env *testEnv, empID, toSeat uint) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	return do(t, env, "POST", "/api/tickets", tokAdmin, map[string]any{
		"employee_id": empID, "to_seat_id": toSeat, "move_date": "2026-10-15",
	})
}

// 完成 IT 三项核对（网口/分机/显示器）
func passITChecks(t *testing.T, env *testEnv, ticketID uint) {
	t.Helper()
	url := fmt.Sprintf("/api/tickets/%d", ticketID)
	if rec, _ := do(t, env, "POST", url+"/it/network-test", tokIT, nil); rec.Code != 200 {
		t.Fatalf("network-test 失败: %d", rec.Code)
	}
	if rec, _ := do(t, env, "POST", url+"/it/phone-migrate", tokIT, nil); rec.Code != 200 {
		t.Fatalf("phone-migrate 失败: %d", rec.Code)
	}
	var seat models.Seat
	var tk models.Ticket
	env.db.First(&tk, ticketID)
	env.db.First(&seat, tk.ToSeatID)
	rec, resp := do(t, env, "POST", url+"/it/monitor-check", tokIT, map[string]any{
		"monitor_ok": true, "asset_tag": seat.AssetTag,
	})
	if rec.Code != 200 {
		t.Fatalf("monitor-check 失败: %d %v", rec.Code, resp)
	}
}

func ticketStatus(t *testing.T, env *testEnv, id uint) string {
	t.Helper()
	var tk models.Ticket
	env.db.First(&tk, id)
	return tk.Status
}

func seatStatus(env *testEnv, seatID uint) string {
	var seat models.Seat
	env.db.First(&seat, seatID)
	if seat.OccupantID != nil {
		return "occupied"
	}
	var n int64
	env.db.Model(&models.Ticket{}).Where("to_seat_id = ? AND status IN ?", seatID, models.ActiveStatuses).Count(&n)
	if n > 0 {
		return "reserved"
	}
	return "available"
}

// ---------- 并发：同一座位同时申请，仅一单成功 ----------

func TestConcurrentSameSeat(t *testing.T) {
	env := setup(t)
	// 8 名员工各占一个座位，同时抢同一个目标座位
	empIDs := make([]uint, 0, 8)
	for i := 0; i < 8; i++ {
		u := models.User{Username: fmt.Sprintf("c%d", i), Name: fmt.Sprintf("并发%d", i),
			Role: models.RoleEmployee, Token: fmt.Sprintf("tok-c%d", i)}
		env.db.Create(&u)
		s := models.Seat{Code: fmt.Sprintf("3F-C%02d", i), Floor: 3, PortCode: fmt.Sprintf("P3F-C%02d", i),
			PortOK: true, HasPhone: true, HasMonitor: true, OccupantID: &u.ID}
		env.db.Create(&s)
		empIDs = append(empIDs, u.ID)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for _, empID := range empIDs {
		wg.Add(1)
		go func(empID uint) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{
				"employee_id": empID, "to_seat_id": env.goodSeat, "move_date": "2026-10-15",
			})
			req := httptest.NewRequest("POST", "/api/tickets", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+tokAdmin)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)
			mu.Lock()
			codes[rec.Code]++
			mu.Unlock()
		}(empID)
	}
	wg.Wait()

	if codes[http.StatusCreated] != 1 {
		t.Fatalf("并发抢同一座位：期望仅 1 单成功，实际 %d（状态分布 %v）", codes[http.StatusCreated], codes)
	}
	if codes[http.StatusConflict] != 7 {
		t.Fatalf("其余 7 单应返回 409，实际状态分布 %v", codes)
	}
	// 数据库层面同样只有一条进行中记录
	var n int64
	env.db.Model(&models.Ticket{}).Where("to_seat_id = ? AND status IN ?", env.goodSeat, models.ActiveStatuses).Count(&n)
	if n != 1 {
		t.Fatalf("数据库中该座位应有且仅有 1 条进行中搬迁单，实际 %d", n)
	}
}

// ---------- 取消后释放预占，可重新分配 ----------

func TestCancelReleasesSeat(t *testing.T) {
	env := setup(t)
	rec, resp := createTicket(t, env, env.emp1ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("建单失败: %d %v", rec.Code, resp)
	}
	id := uint(resp["id"].(float64))
	if got := seatStatus(env, env.goodSeat); got != "reserved" {
		t.Fatalf("建单后座位应为预占，实际 %s", got)
	}

	// 取消前：他人不能占用同一座位
	rec, _ = createTicket(t, env, env.emp2ID, env.goodSeat)
	if rec.Code != 409 {
		t.Fatalf("座位已预占时应 409，实际 %d", rec.Code)
	}

	rec, _ = do(t, env, "POST", fmt.Sprintf("/api/tickets/%d/cancel", id), tokAdmin,
		map[string]any{"reason": "计划变更"})
	if rec.Code != 200 {
		t.Fatalf("取消失败: %d", rec.Code)
	}
	if got := seatStatus(env, env.goodSeat); got != "available" {
		t.Fatalf("取消后座位应释放为可用，实际 %s", got)
	}

	// 重新分配成功
	rec, resp = createTicket(t, env, env.emp2ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("取消后重新分配应成功: %d %v", rec.Code, resp)
	}

	// 变更经过已保留
	_, detail := do(t, env, "GET", fmt.Sprintf("/api/tickets/%d", id), tokAdmin, nil)
	events := detail["events"].([]any)
	var found bool
	for _, e := range events {
		if e.(map[string]any)["action"] == "cancelled" {
			found = true
		}
	}
	if !found {
		t.Fatal("取消后应保留 cancelled 事件")
	}
}

// ---------- 换座：旧座位释放、IT 结果重置 ----------

func TestChangeSeatReleasesOldAndResetsChecks(t *testing.T) {
	env := setup(t)
	rec, resp := createTicket(t, env, env.emp1ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("建单失败: %v", resp)
	}
	id := uint(resp["id"].(float64))

	// IT 先对旧座位做网口测试（通过）
	do(t, env, "POST", fmt.Sprintf("/api/tickets/%d/it/network-test", id), tokIT, nil)
	var tk models.Ticket
	env.db.First(&tk, id)
	if !tk.NetworkOK {
		t.Fatal("网口测试应通过")
	}

	// 换到备用座位
	rec, resp = do(t, env, "POST", fmt.Sprintf("/api/tickets/%d/change-seat", id), tokAdmin,
		map[string]any{"to_seat_id": env.extraSeat})
	if rec.Code != 200 {
		t.Fatalf("换座失败: %d %v", rec.Code, resp)
	}
	if got := seatStatus(env, env.goodSeat); got != "available" {
		t.Fatalf("换座后旧座位应释放，实际 %s", got)
	}
	if got := seatStatus(env, env.extraSeat); got != "reserved" {
		t.Fatalf("换座后新座位应预占，实际 %s", got)
	}
	env.db.First(&tk, id)
	if tk.NetworkOK || tk.Status != models.StatusPending {
		t.Fatalf("换座后 IT 结果应重置、状态回 pending，实际 network_ok=%v status=%s", tk.NetworkOK, tk.Status)
	}

	// 旧座位可分配给其他人
	rec, resp = createTicket(t, env, env.emp2ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("旧座位应可重新分配: %d %v", rec.Code, resp)
	}
}

// ---------- 角色权限矩阵 ----------

func TestRolePermissions(t *testing.T) {
	env := setup(t)
	rec, resp := createTicket(t, env, env.emp1ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("建单失败: %v", resp)
	}
	id := uint(resp["id"].(float64))
	base := fmt.Sprintf("/api/tickets/%d", id)

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		body   any
		want   int
	}{
		{"未登录访问", "GET", "/api/tickets", "", nil, 401},
		{"员工不能建单", "POST", "/api/tickets", tokEmp1, map[string]any{
			"employee_id": env.emp2ID, "to_seat_id": env.extraSeat, "move_date": "2026-10-15"}, 403},
		{"IT不能建单", "POST", "/api/tickets", tokIT, map[string]any{
			"employee_id": env.emp2ID, "to_seat_id": env.extraSeat, "move_date": "2026-10-15"}, 403},
		{"行政不能代填网口测试", "POST", base + "/it/network-test", tokAdmin, nil, 403},
		{"行政不能代填分机迁移", "POST", base + "/it/phone-migrate", tokAdmin, nil, 403},
		{"行政不能代填显示器核对", "POST", base + "/it/monitor-check", tokAdmin,
			map[string]any{"monitor_ok": true, "asset_tag": "X"}, 403},
		{"员工不能填IT结果", "POST", base + "/it/network-test", tokEmp1, nil, 403},
		{"行政不能确认接收", "POST", base + "/confirm", tokAdmin,
			map[string]any{"network_ok": true, "phone_ok": true, "monitor_ok": true}, 403},
		{"IT不能确认接收", "POST", base + "/confirm", tokIT,
			map[string]any{"network_ok": true, "phone_ok": true, "monitor_ok": true}, 403},
		{"员工不能替他人确认", "POST", base + "/confirm", tokEmp2,
			map[string]any{"network_ok": true, "phone_ok": true, "monitor_ok": true}, 403},
		{"员工不能看他人单据", "GET", base, tokEmp2, nil, 403},
		{"员工不能改期", "PATCH", base + "/reschedule", tokEmp1,
			map[string]any{"move_date": "2026-11-01"}, 403},
		{"员工不能取消", "POST", base + "/cancel", tokEmp1, map[string]any{"reason": "x"}, 403},
	}
	for _, tc := range cases {
		rec, resp := do(t, env, tc.method, tc.path, tc.token, tc.body)
		if rec.Code != tc.want {
			t.Errorf("%s: 期望 %d，实际 %d（%v）", tc.name, tc.want, rec.Code, resp)
		}
	}
}

// ---------- 网口不可用：不能完结，换座重测后完成 ----------

func TestNetworkFailBlocksCompletion(t *testing.T) {
	env := setup(t)
	rec, resp := createTicket(t, env, env.emp1ID, env.badSeat)
	if rec.Code != 201 {
		t.Fatalf("建单失败: %v", resp)
	}
	id := uint(resp["id"].(float64))
	base := fmt.Sprintf("/api/tickets/%d", id)

	// 网口测试失败 → blocked
	rec, resp = do(t, env, "POST", base+"/it/network-test", tokIT, nil)
	if rec.Code != 200 {
		t.Fatalf("测试请求失败: %d", rec.Code)
	}
	if resp["network_ok"].(bool) {
		t.Fatal("故障网口测试不应通过")
	}
	if resp["status"] != models.StatusBlocked {
		t.Fatalf("网口故障后状态应为 blocked，实际 %v", resp["status"])
	}

	// 员工不能确认完成
	rec, resp = do(t, env, "POST", base+"/confirm", tokEmp1,
		map[string]any{"network_ok": true, "phone_ok": true, "monitor_ok": true})
	if rec.Code != 409 {
		t.Fatalf("网口未通过时确认应 409，实际 %d %v", rec.Code, resp)
	}

	// 行政换到正常座位 → IT 重测 → 完成
	rec, resp = do(t, env, "POST", base+"/change-seat", tokAdmin, map[string]any{"to_seat_id": env.goodSeat})
	if rec.Code != 200 {
		t.Fatalf("换座失败: %v", resp)
	}
	passITChecks(t, env, id)
	if got := ticketStatus(t, env, id); got != models.StatusReady {
		t.Fatalf("IT 核对全部通过后应为 ready，实际 %s", got)
	}
	rec, resp = do(t, env, "POST", base+"/confirm", tokEmp1,
		map[string]any{"network_ok": true, "phone_ok": true, "monitor_ok": true})
	if rec.Code != 200 || resp["status"] != models.StatusCompleted {
		t.Fatalf("确认后应完成: %d %v", rec.Code, resp["status"])
	}
	if got := seatStatus(env, env.emp1Seat); got != "available" {
		t.Fatalf("完成后原座位应释放，实际 %s", got)
	}
	if got := seatStatus(env, env.goodSeat); got != "occupied" {
		t.Fatalf("完成后新座位应被占用，实际 %s", got)
	}
}

// ---------- 完整流程 + 事件时间线 ----------

func TestHappyPathWithTimeline(t *testing.T) {
	env := setup(t)
	rec, resp := createTicket(t, env, env.emp1ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("建单失败: %v", resp)
	}
	id := uint(resp["id"].(float64))
	base := fmt.Sprintf("/api/tickets/%d", id)

	// 改期留痕
	rec, _ = do(t, env, "PATCH", base+"/reschedule", tokAdmin, map[string]any{"move_date": "2026-11-01"})
	if rec.Code != 200 {
		t.Fatalf("改期失败: %d", rec.Code)
	}

	passITChecks(t, env, id)
	if got := ticketStatus(t, env, id); got != models.StatusReady {
		t.Fatalf("应为 ready，实际 %s", got)
	}

	rec, resp = do(t, env, "POST", base+"/confirm", tokEmp1,
		map[string]any{"network_ok": true, "phone_ok": true, "monitor_ok": true})
	if rec.Code != 200 || resp["status"] != models.StatusCompleted {
		t.Fatalf("应完成: %d %v", rec.Code, resp)
	}

	// 时间线包含全部关键节点
	events := resp["events"].([]any)
	want := []string{"created", "rescheduled", "network_test_passed", "phone_migrated",
		"monitor_checked", "it_ready", "employee_confirmed", "completed"}
	got := map[string]bool{}
	for _, e := range events {
		got[e.(map[string]any)["action"].(string)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("时间线缺少事件 %s", w)
		}
	}
}

// ---------- 资产标签不符：留痕、阻断、核实后放行 ----------

func TestAssetMismatchFlow(t *testing.T) {
	env := setup(t)
	rec, resp := createTicket(t, env, env.emp1ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("建单失败: %v", resp)
	}
	id := uint(resp["id"].(float64))
	base := fmt.Sprintf("/api/tickets/%d", id)

	do(t, env, "POST", base+"/it/network-test", tokIT, nil)
	do(t, env, "POST", base+"/it/phone-migrate", tokIT, nil)
	// 显示器资产标签与台账不符
	rec, resp = do(t, env, "POST", base+"/it/monitor-check", tokIT,
		map[string]any{"monitor_ok": true, "asset_tag": "MON-WRONG"})
	if rec.Code != 200 {
		t.Fatalf("核对请求失败: %d", rec.Code)
	}
	if !resp["asset_mismatch"].(bool) {
		t.Fatal("应标记资产异常")
	}
	if resp["status"] == models.StatusReady {
		t.Fatal("资产异常未解除前不能 ready")
	}

	// 员工此时不能确认
	rec, _ = do(t, env, "POST", base+"/confirm", tokEmp1,
		map[string]any{"network_ok": true, "phone_ok": true, "monitor_ok": true})
	if rec.Code != 409 {
		t.Fatalf("资产异常时应 409，实际 %d", rec.Code)
	}

	// IT 核实并更正台账 → ready
	rec, resp = do(t, env, "POST", base+"/it/resolve-asset", tokIT, map[string]any{"asset_tag": "MON-WRONG"})
	if rec.Code != 200 {
		t.Fatalf("解除异常失败: %v", resp)
	}
	if resp["status"] != models.StatusReady {
		t.Fatalf("解除后应 ready，实际 %v", resp["status"])
	}

	// 事件留痕
	events := resp["events"].([]any)
	actions := map[string]bool{}
	for _, e := range events {
		actions[e.(map[string]any)["action"].(string)] = true
	}
	if !actions["asset_mismatch"] || !actions["asset_resolved"] {
		t.Errorf("应保留 asset_mismatch 与 asset_resolved 事件，实际 %v", actions)
	}
}

// ---------- 其他约束 ----------

func TestEmployeeCannotConfirmBeforeReady(t *testing.T) {
	env := setup(t)
	rec, resp := createTicket(t, env, env.emp1ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("建单失败: %v", resp)
	}
	id := uint(resp["id"].(float64))
	// IT 未核对时直接确认 → 409
	rec, _ = do(t, env, "POST", fmt.Sprintf("/api/tickets/%d/confirm", id), tokEmp1,
		map[string]any{"network_ok": true, "phone_ok": true, "monitor_ok": true})
	if rec.Code != 409 {
		t.Fatalf("IT 未核对时确认应 409，实际 %d", rec.Code)
	}
}

func TestEmployeeIssueBlocksTicket(t *testing.T) {
	env := setup(t)
	rec, resp := createTicket(t, env, env.emp1ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("建单失败: %v", resp)
	}
	id := uint(resp["id"].(float64))
	base := fmt.Sprintf("/api/tickets/%d", id)
	passITChecks(t, env, id)

	// 员工搬入后发现网络不可用 → 反馈问题 → 打回 blocked
	rec, resp = do(t, env, "POST", base+"/confirm", tokEmp1,
		map[string]any{"network_ok": false, "phone_ok": true, "monitor_ok": true, "comment": "网口灯不亮"})
	if rec.Code != 200 {
		t.Fatalf("反馈问题应受理: %d", rec.Code)
	}
	if resp["status"] != models.StatusBlocked {
		t.Fatalf("反馈问题后应 blocked，实际 %v", resp["status"])
	}
}

func TestDuplicateActiveTicketPerEmployee(t *testing.T) {
	env := setup(t)
	rec, _ := createTicket(t, env, env.emp1ID, env.goodSeat)
	if rec.Code != 201 {
		t.Fatalf("首单应成功: %d", rec.Code)
	}
	rec, resp := createTicket(t, env, env.emp1ID, env.extraSeat)
	if rec.Code != 409 {
		t.Fatalf("同一员工第二张进行中单据应 409，实际 %d %v", rec.Code, resp)
	}
}

func TestPhoneMigrateFailsWithoutPort(t *testing.T) {
	env := setup(t)
	rec, resp := createTicket(t, env, env.emp1ID, env.noPhoneSeat)
	if rec.Code != 201 {
		t.Fatalf("建单失败: %v", resp)
	}
	id := uint(resp["id"].(float64))
	base := fmt.Sprintf("/api/tickets/%d", id)
	do(t, env, "POST", base+"/it/network-test", tokIT, nil)
	rec, resp = do(t, env, "POST", base+"/it/phone-migrate", tokIT, nil)
	if rec.Code != 200 || resp["phone_ok"].(bool) {
		t.Fatalf("无电话端口时分机迁移应失败: %d %v", rec.Code, resp)
	}
	// 分机未通过 → 不能 ready
	do(t, env, "POST", base+"/it/monitor-check", tokIT,
		map[string]any{"monitor_ok": true, "asset_tag": "MON-5F-B03"})
	if got := ticketStatus(t, env, id); got == models.StatusReady {
		t.Fatal("分机未迁移成功时不应 ready")
	}
}

func TestMockEndpoints(t *testing.T) {
	env := setup(t)
	rec, resp := do(t, env, "POST", "/api/mock/network-test", tokIT, map[string]any{"port_code": "P5F-B02"})
	if rec.Code != 200 || resp["ok"].(bool) {
		t.Fatalf("故障端口应返回 ok=false: %d %v", rec.Code, resp)
	}
	rec, resp = do(t, env, "POST", "/api/mock/network-test", tokIT, map[string]any{"port_code": "P5F-B01"})
	if rec.Code != 200 || !resp["ok"].(bool) {
		t.Fatalf("正常端口应返回 ok=true: %d %v", rec.Code, resp)
	}
	rec, resp = do(t, env, "POST", "/api/mock/extension-migrate", tokIT,
		map[string]any{"extension": "6001", "to_seat_id": env.noPhoneSeat})
	if rec.Code != 200 || resp["ok"].(bool) {
		t.Fatalf("无电话端口迁移应失败: %d %v", rec.Code, resp)
	}
}
