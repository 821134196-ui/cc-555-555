# 员工工位搬迁与网络端口开通系统

公司搬办公室时，**行政、IT、员工通过同一张搬迁单协作**的系统：

- **行政**：填写原座位、目标座位、搬迁日期；安排座位、改期、取消
- **IT**：核对目标桌位的**网口、电话分机、显示器**，核对**资产标签**；网络测试与分机迁移由**模拟接口**执行
- **员工**：在自己的页面确认新工位设备与网络是否可用，**网络测试通过**是完成的硬性条件

技术栈：Go 1.24 + Gin + GORM + SQLite（后端），Vue 3 + Vite（前端，构建产物 `go:embed` 进二进制）。

## 一条命令启动

```bash
./run.sh            # 自动构建前端并启动全部服务
./run.sh --reset    # 重置数据库（恢复种子数据）
```

启动后：

| 服务 | 地址 |
| --- | --- |
| 系统（页面 + API） | http://localhost:8080 |
| 模拟网络/分机接口 | http://127.0.0.1:9090 |

> 环境变量：`ADDR`、`MOCK_ADDR`、`DB_PATH`、`JWT_SECRET`。
> 前端开发模式：`cd web && npm run dev`（已配置 `/api`、`/mock` 代理）。

## 演示账号（免密码）

| 角色 | 账号 |
| --- | --- |
| 行政 | `admin`（林晓） |
| IT | `it`（周工） |
| 员工 | `wangwu` `zhaoliu` `sunqi` `chenba` `zhoujiu` `wushi` |

## 搬迁单状态机

```
行政建单(reserved, 目标座位预占)
   │
   ├─ 取消 ───────────────► cancelled（立即释放预占座位与分机占用）
   ├─ 改期/换座 ──────────► reserved（旧座位释放、新座位预占；IT 结论作废需重核）
   └─ IT 核对通过(it_checked)
          │  条件：模拟网络测试通过 + 分机迁移成功/无分机 + 显示器就位 + 资产标签一致
          └─ 员工本人确认(confirmed)
                 条件：网络测试通过 + 本人勾选网络可用 + 设备可用
                 目标座位 reserved→occupied，原座位腾空
```

## 关键业务规则与实现位置

| 规则 | 实现 |
| --- | --- |
| **同一座位不能分给两个人** | 创建/换座在事务内执行条件更新 `UPDATE seats SET status='reserved' WHERE id=? AND status='free'`，`RowsAffected=0` 返回 409（`internal/app/service.go: reserveSeat`） |
| **网口不可用不能结束搬迁单** | 座位无网口/已登记故障时 IT 核对直接 422；网络测试结果只能来自模拟接口，无法手填；员工确认前再次校验 `net_tested` |
| **改期/取消释放预占** | `Cancel` / `Reschedule` 事务内把座位置回 `free`，并调用模拟接口释放分机占用 |
| **资产标签不符保留经过** | 每次 IT 核对（含失败）写一条 `it_checks`；不符时单据停留在 `reserved`，事件流记录 `asset_mismatch`（期望/现场标签都留痕） |
| **行政不能代 IT 填测试结果** | 路由中间件 `RequireRole(it)` 限制 `/it-check`；测试结果由后端调用模拟接口获取，请求体中没有网络结果字段 |
| **员工不能代他人确认** | 路由限员工角色 + 服务层校验 `order.employee_id == 当前用户`，他人调用返回 403 |

## 模拟接口（`internal/mocksvc`，独立 HTTP 端口）

- `POST /mock/network/test` —— 网口连通性/DHCP/DNS 测试，故障网口返回 `ok:false`
- `POST /mock/network/set` —— IT 登记/解除网口故障
- `POST /mock/phone/migrate` —— 电话分机迁移（目标分机被其他工单占用时失败）
- `POST /mock/phone/release` —— 取消/改期时释放分机占用

种子数据中 `F3-C-05`、`F3-E-07` 为网口故障，可用于演示“故障阻断 → IT 修复 → 重新核对”。

## 测试

```bash
go test -race -count=1 ./...
```

覆盖（`internal/app/service_test.go`）：

1. **同时申请同一座位**：4 个并发请求恰好 1 单成功，其余 409，座位最终 `reserved`
2. **撤销后重新分配**：取消 → 座位 `free` → 可分配给另一人，并走完整核对/确认
3. **改期换座**：旧座释放、新座预占、IT 结论作废、事件留痕；抢新座失败时事务回滚旧预占不动
4. **网口故障阻断**：核对 422、员工不能确认；修复后可通过
5. **资产标签不符**：核对 422、不进入 `it_checked`、记录 `asset_mismatch`
6. **验收权限**：他人/行政/IT 代确认均 403；未勾选网络可用 400
7. **HTTP 角色隔离**：IT/员工建单 403、行政调 IT 核对 403、员工只能看到自己的单

## 目录结构

```
main.go                     # 启动：模拟接口 + API + 嵌入前端
internal/
  models/    数据模型（User/Seat/MoveOrder/ITCheck/MoveEvent）
  store/     SQLite 打开、迁移、种子数据
  mocksvc/   模拟网络测试 / 分机迁移 HTTP 服务
  mockclient/调用模拟接口的客户端
  auth/      JWT 与角色中间件
  app/       业务状态机（service.go）、HTTP handler、测试
web/         Vue 3 前端（楼层图、搬迁单、详情时间线）
run.sh       一条命令启动
```
