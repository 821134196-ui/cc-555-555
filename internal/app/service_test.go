package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"seatmove/internal/auth"
	"seatmove/internal/mockclient"
	"seatmove/internal/mocksvc"
	"seatmove/internal/models"
	"seatmove/internal/store"
)

type fixture struct {
	db    *gorm.DB
	svc   *Service
	mock  *mocksvc.Service
	users map[string]*models.User
	seats map[string]*models.Seat
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	mock := mocksvc.New(addr)
	srv := mock.Run()
	t.Cleanup(func() { _ = srv.Close() })

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	store.SyncMockPorts(db, mock)

	f := &fixture{
		db:    db,
		svc:   NewService(db, mockclient.New("http://"+addr)),
		mock:  mock,
		users: map[string]*models.User{},
		seats: map[string]*models.Seat{},
	}
	var us []models.User
	db.Find(&us)
	for i := range us {
		f.users[us[i].Username] = &us[i]
	}
	var ss []models.Seat
	db.Find(&ss)
	for i := range ss {
		f.seats[ss[i].Code] = &ss[i]
	}
	return f
}

func (f *fixture) mustCreate(t *testing.T, admin *models.User, empUsername, toCode string, date string) *models.MoveOrder {
	t.Helper()
	emp := f.users[empUsername]
	to := f.seats[toCode]
	d, _ := time.ParseInLocation("2006-01-02", date, time.Local)
	o, err := f.svc.CreateOrder(admin, CreateOrderInput{
		EmployeeID: emp.ID, ToSeatID: to.ID, MoveDate: d,
	})
	if err != nil {
		t.Fatalf("创建搬迁单失败: %v", err)
	}
	return o
}

func isConflict(err error) bool {
	var be *BizError
	return errAs(err, &be) && be.Status == 409
}

func errAs(err error, target **BizError) bool {
	for e := err; e != nil; {
		if be, ok := e.(*BizError); ok {
			*target = be
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

func statusOf(err error) int {
	var be *BizError
	if errAs(err, &be) {
		return be.Status
	}
	return -1
}

// 1) 同时申请同一座位：并发 N 单只有一单成功，其余收到 409；座位最终为 reserved
func TestConcurrentReserveSameSeat(t *testing.T) {
	f := setup(t)
	admin := f.users["admin"]
	target := f.seats["F3-D-02"]
	d, _ := time.ParseInLocation("2006-01-02", "2026-10-20", time.Local)

	emps := []string{"chenba", "zhoujiu", "wushi", "sunqi"}
	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.CreateOrder(admin, CreateOrderInput{
				EmployeeID: f.users[emps[i]].ID, ToSeatID: target.ID, MoveDate: d,
			})
		}()
	}
	wg.Wait()

	ok, conflict := 0, 0
	for _, e := range errs {
		switch {
		case e == nil:
			ok++
		case isConflict(e):
			conflict++
		default:
			t.Fatalf("出现非预期错误: %v", e)
		}
	}
	if ok != 1 || conflict != n-1 {
		t.Fatalf("期望恰好 1 单成功、%d 单冲突，实际成功 %d 冲突 %d（错误集: %v）", n-1, ok, conflict, errs)
	}

	var s models.Seat
	f.db.First(&s, target.ID)
	if s.Status != models.SeatReserved {
		t.Fatalf("座位状态应为 reserved，实际 %s", s.Status)
	}
}

// 2) 撤销后释放预占，座位可被重新分配；再走完整 IT 核对 + 员工确认流程
func TestCancelReleasesAndReassign(t *testing.T) {
	f := setup(t)
	admin, it := f.users["admin"], f.users["it"]

	o := f.mustCreate(t, admin, "chenba", "F3-D-03", "2026-10-21")
	if got := f.seatStatus(o.ToSeatID); got != models.SeatReserved {
		t.Fatalf("创建后应为 reserved，实际 %s", got)
	}

	if err := f.svc.Cancel(admin, o.ID, "员工出差延期"); err != nil {
		t.Fatal(err)
	}
	if got := f.seatStatus(o.ToSeatID); got != models.SeatFree {
		t.Fatalf("取消后应为 free，实际 %s", got)
	}
	var co models.MoveOrder
	f.db.First(&co, o.ID)
	if co.Status != models.StatusCancelled {
		t.Fatalf("单据应为 cancelled，实际 %s", co.Status)
	}

	// 原员工的单取消后，新员工可分配同一座位
	o2 := f.mustCreate(t, admin, "zhoujiu", "F3-D-03", "2026-10-25")
	if got := f.seatStatus(o2.ToSeatID); got != models.SeatReserved {
		t.Fatalf("重新分配后应为 reserved，实际 %s", got)
	}

	// 完整核对（资产标签必须正确）+ 本人确认
	out, err := f.svc.ITCheck(context.Background(), it, o2.ID, ITCheckInput{
		MonitorOK: true, ActualAssetTag: f.users["zhoujiu"].AssetTag,
	})
	if err != nil || !out.Passed {
		t.Fatalf("IT 核对应通过: err=%v out=%+v", err, out)
	}
	if _, err := f.svc.Confirm(f.users["zhoujiu"], o2.ID, ConfirmInput{NetUsable: true, DeviceUsable: true}); err != nil {
		t.Fatal(err)
	}
	if got := f.seatStatus(o2.ToSeatID); got != models.SeatOccupied {
		t.Fatalf("确认后应为 occupied，实际 %s", got)
	}
}

// 3) 改期换座：旧座位释放、新座位预占、IT 结论作废需重核
func TestRescheduleReleasesOldSeat(t *testing.T) {
	f := setup(t)
	admin, it := f.users["admin"], f.users["it"]
	o := f.mustCreate(t, admin, "chenba", "F3-D-04", "2026-10-21")

	if _, err := f.svc.ITCheck(context.Background(), it, o.ID, ITCheckInput{
		MonitorOK: true, ActualAssetTag: f.users["chenba"].AssetTag,
	}); err != nil {
		t.Fatal(err)
	}

	newDate, _ := time.ParseInLocation("2006-01-02", "2026-11-01", time.Local)
	if err := f.svc.Reschedule(admin, o.ID, RescheduleInput{
		NewMoveDate: newDate, NewToSeatID: f.seats["F3-D-05"].ID,
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.seatStatus(f.seats["F3-D-04"].ID); got != models.SeatFree {
		t.Fatalf("旧座位应释放为 free，实际 %s", got)
	}
	if got := f.seatStatus(f.seats["F3-D-05"].ID); got != models.SeatReserved {
		t.Fatalf("新座位应为 reserved，实际 %s", got)
	}
	var ro models.MoveOrder
	f.db.First(&ro, o.ID)
	if ro.Status != models.StatusReserved || ro.NetTested {
		t.Fatalf("改期后状态应回到 reserved 且 IT 结论作废，实际 status=%s net=%v", ro.Status, ro.NetTested)
	}
	var evs []models.MoveEvent
	f.db.Where("order_id = ? AND action = ?", o.ID, "rescheduled").Find(&evs)
	if len(evs) != 1 {
		t.Fatalf("应记录 1 条改期事件，实际 %d", len(evs))
	}
}

// 4) 网口不可用时 IT 核对被阻断、搬迁单不能结束；修复后可继续
func TestBrokenNetPortBlocksCompletion(t *testing.T) {
	f := setup(t)
	admin, it, emp := f.users["admin"], f.users["it"], f.users["wushi"]

	// F3-C-05 种子中即为网口故障
	o := f.mustCreate(t, admin, "wushi", "F3-C-05", "2026-10-21")
	_, err := f.svc.ITCheck(context.Background(), it, o.ID, ITCheckInput{
		MonitorOK: true, ActualAssetTag: emp.AssetTag,
	})
	if statusOf(err) != 422 {
		t.Fatalf("网口故障时核对应返回 422，实际 %v", err)
	}
	var co models.MoveOrder
	f.db.First(&co, o.ID)
	if co.Status != models.StatusReserved {
		t.Fatalf("单据应停留在 reserved，实际 %s", co.Status)
	}
	// 员工此时不能确认
	if _, err := f.svc.Confirm(emp, o.ID, ConfirmInput{true, true, ""}); statusOf(err) != 422 {
		t.Fatalf("IT 未通过时员工确认应被拒(422)，实际 %v", err)
	}

	// IT 修复网口后核对通过
	if err := f.svc.SetSeatNetEnabled(it, f.seats["F3-C-05"].ID, true, "配线已恢复"); err != nil {
		t.Fatal(err)
	}
	out, err := f.svc.ITCheck(context.Background(), it, o.ID, ITCheckInput{
		MonitorOK: true, ActualAssetTag: emp.AssetTag,
	})
	if err != nil || !out.Passed {
		t.Fatalf("修复后核对应通过: %v %+v", err, out)
	}
}

// 5) 资产标签不符：核对不通过、单据不能结束，且留痕 asset_mismatch
func TestAssetTagMismatch(t *testing.T) {
	f := setup(t)
	it := f.users["it"]
	o := f.mustCreate(t, f.users["admin"], "chenba", "F3-D-06", "2026-10-21")

	_, err := f.svc.ITCheck(context.Background(), it, o.ID, ITCheckInput{
		MonitorOK: true, ActualAssetTag: "AST-9999",
	})
	if statusOf(err) != 422 {
		t.Fatalf("标签不符应返回 422，实际 %v", err)
	}
	var evs []models.MoveEvent
	f.db.Where("order_id = ? AND action = ?", o.ID, "asset_mismatch").Find(&evs)
	if len(evs) != 1 {
		t.Fatalf("应记录 asset_mismatch 事件，实际 %d 条", len(evs))
	}
	var co models.MoveOrder
	f.db.First(&co, o.ID)
	if co.Status == models.StatusITChecked {
		t.Fatal("标签不符不得进入 it_checked")
	}
}

// 6) 权限：员工不能替他人确认；网络/设备未确认可用不能完成
func TestConfirmPermissions(t *testing.T) {
	f := setup(t)
	admin, it := f.users["admin"], f.users["it"]
	o := f.mustCreate(t, admin, "chenba", "F3-D-07", "2026-10-21")
	if _, err := f.svc.ITCheck(context.Background(), it, o.ID, ITCheckInput{
		MonitorOK: true, ActualAssetTag: f.users["chenba"].AssetTag,
	}); err != nil {
		t.Fatal(err)
	}

	// 另一名员工替确认 → 403
	if _, err := f.svc.Confirm(f.users["zhoujiu"], o.ID, ConfirmInput{true, true, ""}); statusOf(err) != 403 {
		t.Fatalf("他人代确认应 403，实际 %v", err)
	}
	// 行政/IT 代员工确认（服务层规则：非本人）→ 403
	if _, err := f.svc.Confirm(admin, o.ID, ConfirmInput{true, true, ""}); statusOf(err) != 403 {
		t.Fatalf("行政代确认应 403，实际 %v", err)
	}
	if _, err := f.svc.Confirm(it, o.ID, ConfirmInput{true, true, ""}); statusOf(err) != 403 {
		t.Fatalf("IT 代确认应 403，实际 %v", err)
	}
	// 本人但未勾选网络可用 → 400
	if _, err := f.svc.Confirm(f.users["chenba"], o.ID, ConfirmInput{NetUsable: false, DeviceUsable: true}); statusOf(err) != 400 {
		t.Fatalf("未确认网络可用应 400，实际 %v", err)
	}
	// 本人且全部可用 → 完成
	if _, err := f.svc.Confirm(f.users["chenba"], o.ID, ConfirmInput{true, true, ""}); err != nil {
		t.Fatalf("本人确认应成功: %v", err)
	}
}

// 7) 改期抢新座失败时旧预占不受影响（事务回滚）
func TestRescheduleRollbackKeepsReservation(t *testing.T) {
	f := setup(t)
	admin := f.users["admin"]
	o := f.mustCreate(t, admin, "chenba", "F3-D-08", "2026-10-21")
	_ = f.mustCreate(t, admin, "zhoujiu", "F3-F-01", "2026-10-21") // 占住新目标

	newDate, _ := time.ParseInLocation("2006-01-02", "2026-11-02", time.Local)
	err := f.svc.Reschedule(admin, o.ID, RescheduleInput{
		NewMoveDate: newDate, NewToSeatID: f.seats["F3-F-01"].ID,
	})
	if !isConflict(err) {
		t.Fatalf("改到已预占座位应 409，实际 %v", err)
	}
	if got := f.seatStatus(o.ToSeatID); got != models.SeatReserved {
		t.Fatalf("回滚后原座位仍应 reserved，实际 %s", got)
	}
}

// 8) HTTP 层角色隔离：行政不能调 IT 核对接口，IT 不能建单，员工看不到别人的单
func TestHTTPRoleGuards(t *testing.T) {
	f := setup(t)
	mgr := auth.NewManager("test-secret")
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	NewHandler(f.db, f.svc, mgr).Register(r)

	tokenFor := func(username string) string {
		var u models.User
		f.db.Where("username = ?", username).First(&u)
		tk, err := mgr.Issue(&u)
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}

	chenba := f.users["chenba"]
	to := f.seats["F3-F-02"].ID
	d := "2026-10-28"
	body := fmt.Sprintf(`{"employee_id":%d,"to_seat_id":%d,"move_date":%q}`, chenba.ID, to, d)

	doReq := func(method, path, token, bodystr string) int {
		var reader io.Reader
		if bodystr != "" {
			reader = strings.NewReader(bodystr)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	// 未登录 → 401
	if code := doReq("GET", "/api/orders", "", ""); code != 401 {
		t.Fatalf("未登录应 401，实际 %d", code)
	}
	// IT 不能建单 → 403
	if code := doReq("POST", "/api/orders", tokenFor("it"), body); code != 403 {
		t.Fatalf("IT 建单应 403，实际 %d", code)
	}
	// 员工不能建单 → 403
	if code := doReq("POST", "/api/orders", tokenFor("chenba"), body); code != 403 {
		t.Fatalf("员工建单应 403，实际 %d", code)
	}
	// 行政建单成功 → 201
	var orderID uint
	func() {
		req := httptest.NewRequest("POST", "/api/orders", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tokenFor("admin"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 201 {
			t.Fatalf("行政建单应 201，实际 %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Order struct {
				ID uint `json:"id"`
			} `json:"order"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		orderID = resp.Order.ID
	}()

	// 行政不能调 IT 核对接口 → 403
	chkBody := fmt.Sprintf(`{"monitor_ok":true,"actual_asset_tag":%q}`, chenba.AssetTag)
	if code := doReq("POST", fmt.Sprintf("/api/orders/%d/it-check", orderID), tokenFor("admin"), chkBody); code != 403 {
		t.Fatalf("行政调 IT 核对应 403，实际 %d", code)
	}
	// IT 核对成功
	if code := doReq("POST", fmt.Sprintf("/api/orders/%d/it-check", orderID), tokenFor("it"), chkBody); code != 200 {
		t.Fatalf("IT 核对应 200，实际 %d", code)
	}
	// 别人不能确认 chenba 的单 → 403
	confirmBody := `{"net_usable":true,"device_usable":true}`
	if code := doReq("POST", fmt.Sprintf("/api/orders/%d/confirm", orderID), tokenFor("zhoujiu"), confirmBody); code != 403 {
		t.Fatalf("他人代确认应 403，实际 %d", code)
	}
	// 本人确认成功
	if code := doReq("POST", fmt.Sprintf("/api/orders/%d/confirm", orderID), tokenFor("chenba"), confirmBody); code != 200 {
		t.Fatalf("本人确认应 200，实际 %d", code)
	}
	// 员工列表只看到自己的单
	req := httptest.NewRequest("GET", "/api/orders", nil)
	req.Header.Set("Authorization", "Bearer "+tokenFor("zhoujiu"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("列表应 200，实际 %d", w.Code)
	}
	var list struct {
		Orders []struct {
			EmployeeID uint `json:"employee_id"`
		} `json:"orders"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, o := range list.Orders {
		if o.EmployeeID != f.users["zhoujiu"].ID {
			t.Fatal("员工列表中出现了他人的搬迁单")
		}
	}
}

func (f *fixture) seatStatus(id uint) string {
	var s models.Seat
	f.db.First(&s, id)
	return s.Status
}
