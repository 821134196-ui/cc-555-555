# 工位搬迁与网络端口开通系统

公司搬办公室时，行政、IT、员工围绕同一张搬迁单协作：行政安排座位，IT 核对目标工位的
网口 / 电话分机 / 显示器并记录测试结果，员工搬入后确认设备与网络可用，完成搬迁。

## 快速开始

```bash
./start.sh          # 一键启动（首次自动装依赖、构建前端），访问 http://localhost:8080
./start.sh test     # 运行后端测试
```

演示账号（登录页直接选择，免密）：

| 账号 | 角色 | 能做什么 |
|---|---|---|
| `admin` 王芳 | 行政 | 建单、改期、换座、取消 |
| `li.gong` 李工 / `zhao.gong` 赵工 | IT | 网口测试、分机迁移、显示器核对、资产异常处理 |
| `zhang.wei` 张伟 等 8 人 | 员工 | 查看自己的搬迁单、搬入确认 |

种子数据含 3F / 5F 两层座位；`5F-C03`、`5F-B02` 为故障网口（用于演示受阻流程），
`5F-D05` 无电话端口，`5F-E06` 无显示器。

## 技术栈与结构

- 后端：Go + Gin + GORM + SQLite（纯 Go 驱动 `glebarez/sqlite`，无需 CGO）
- 前端：Vue 3 + Vite + vue-router（构建产物由 Go 服务托管）
- 模拟接口：网口测试、分机迁移以 HTTP 接口提供（`/api/mock/*`），结果由座位台账决定，行为确定、可测试

```
server/                 Go 后端
  main.go               入口（DB_PATH / WEB_DIST / ADDR 可配）
  internal/models/      User / Seat / Ticket / TicketEvent
  internal/db/          连接、迁移（含部分唯一索引）、种子数据
  internal/api/         路由、认证、座位、搬迁单全流程
  internal/mock/        网口测试 / 分机迁移模拟接口
  internal/api/*_test.go 集成测试（并发、权限、状态机）
web/                    Vue 前端（搬迁单、楼层图、角色面板）
start.sh                一键启动 / 测试
```

## 业务流程与规则

```
行政建单(pending) ──IT 三项核对全通过──> ready ──员工确认全部可用──> completed
      │                   │                     │
      │              网口测试失败              员工反馈问题
      │                   │                     │
      │                   ▼                     ▼
      │                blocked ◀──换座/重测/核实资产──┘
      └──改期 / 换座（释放旧预占、重置 IT 结果）/ 取消（释放预占）──> cancelled
```

- **同一座位不两人**：`tickets(to_seat_id)` 上的部分唯一索引（仅 pending/ready/blocked
  生效）在数据库层兜底，并发抢座只有一单成功，其余 409；同一员工同样仅允许一张进行中单据。
- **网口不可用不能完结**：网口测试失败 → 单据 `blocked`；员工确认接口强制要求
  `status=ready 且 network_ok=true`，否则 409。
- **改期 / 取消 / 资产不符留痕**：所有变更写入 `ticket_events`（操作人、动作、明细）；
  取消或换座立即释放旧座位预占；换座会重置 IT 核对结果，需重新核对。
- **资产标签不符**：IT 扫描标签与台账不一致 → 标记异常并阻断 ready，IT 核实更正台账后放行。
- **角色隔离**：行政不能调 IT 核对接口（403），IT 不能建单/确认，员工只能看自己的单、
  只能确认本人的单（403），未登录 401。

## 主要 API

| 方法 | 路径 | 角色 | 说明 |
|---|---|---|---|
| POST | `/api/login` | 公开 | 演示登录（按用户名发令牌） |
| GET | `/api/floors/:floor/seats` | 登录 | 楼层座位（占用/预占/可用） |
| POST | `/api/tickets` | 行政 | 建单（员工、目标座位、日期） |
| PATCH | `/api/tickets/:id/reschedule` | 行政 | 改期 |
| POST | `/api/tickets/:id/change-seat` | 行政 | 换座（释放旧预占、重置 IT 结果） |
| POST | `/api/tickets/:id/cancel` | 行政 | 取消（释放预占） |
| POST | `/api/tickets/:id/it/network-test` | IT | 网口测试（调模拟接口） |
| POST | `/api/tickets/:id/it/phone-migrate` | IT | 分机迁移（调模拟接口） |
| POST | `/api/tickets/:id/it/monitor-check` | IT | 显示器核对 + 资产标签比对 |
| POST | `/api/tickets/:id/it/resolve-asset` | IT | 核实后更正台账、解除异常 |
| POST | `/api/tickets/:id/confirm` | 员工本人 | 搬入确认（全可用才完成） |
| POST | `/api/mock/network-test` | 登录 | 模拟接口：网口测试 |
| POST | `/api/mock/extension-migrate` | 登录 | 模拟接口：分机迁移 |

## 测试

```bash
./start.sh test    # 或 cd server && go test ./...
```

覆盖：并发申请同一座位（8 并发仅 1 成功）、取消/换座后座位释放再分配、
角色权限矩阵（行政代填 IT 结果 403、员工替他人确认 403 等 12 例）、
网口故障阻断完结、资产不符阻断与核实放行、员工反馈问题打回、
同一员工重复建单 409、无电话端口迁移失败、完整流程时间线。
