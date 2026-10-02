# 🌱 谷香认养 · 农产品众筹认养配送与成熟度回访平台

围绕"同一认养批次"把 **农场、认养人、采摘工、包装员、物流、客服** 的全部处理结果串成一条链：
地块 → 农事投入 → 施肥用药 → 天气灾害 → 成熟度抽检 → 采摘 → 分拣 → 分批发货（冷链）→
坏果/争议/赔付 → 保险理赔 → 成熟度回访 → 最终交付结算 → 地块成本与客户满意度沉淀。

技术栈：**React 18（Vite）+ Go 1.19（net/http + pgx）+ PostgreSQL 15**，单镜像部署、`docker compose` 一键启动。

---

## 一、一键启动（Docker Compose，推荐）

前置：已安装 Docker 与 Docker Compose。

```bash
cd /workspace/135-feature-2-a
docker compose up --build -d
```

启动后访问：

- 平台首页（认养项目）：<http://localhost:8080>
- 健康检查：<http://localhost:8080/api/health>

服务说明：

| 服务 | 内容 | 端口 |
|---|---|---|
| `app` | Go 后端 API + 托管 React 构建产物（单端口同源） | 8080 |
| `db`  | PostgreSQL 15，库名 `farmcrowd`，用户/密码 `farm/farm123` | 5432 |

首次启动时后端会自动执行数据库迁移并写入演示数据（含冰雹减产、坏果投诉、临时改地址、
企业团购权益口径争议、蓝莓冷链脱冷坏果赔付判定等完整场景），无需手工初始化。停止用 `docker compose down`，
清除数据卷用 `docker compose down -v`。

---

## 二、演示账号（密码均为 `farm123`）

| 角色 | 姓名 | 手机号 | 登录后主要视图 |
|---|---|---|---|
| 平台管理员 | 平台管理员 | `13800000000` | 平台运营总览、品类与结季数据沉淀 |
| 农场主 | 李远山 | `13900000001` | 农场工作台、地块完整账、发布项目/农事/采摘/发货/结算 |
| 认养人 | 陈曦 | `13900000002` | 我的认养、权益兑现核对、证书、改地址、回访 |
| 认养人（企业） | 周凯 | `13900000007` | 星辰科技 20 棵企业团购订单 |
| 认养人 | 吴琳 | `13900000008` | 可用于下一笔全新认养全流程体验 |
| 采摘工 | 赵秀兰 | `13900000003` | 回填采摘实际产量与坏果率 |
| 包装员 | 王梅 | `13900000004` | 登记分拣与包装规格 |
| 冷链物流 | 马建国 | `13900000005` | 推进发货状态（采摘→包装→发车→送达） |
| 客服 | 孙悦 | `13900000006` | 工单协调、赔付登记、回访 |

登录页点击任意演示账号即可自动填充手机号。

---

## 三、推荐验证路径（对应题目要求）

1. **不登录**浏览首页 5 个认养项目（🍎苹果 / 🫐蓝莓 / 🍯蜂蜜 / 🌾稻田 / 🥬蔬菜）与"直播·公益·团购"。
2. 用 **认养人陈曦 `13900000002`** 登录"我的认养"：
   - 苹果订单可看到 **保底 40kg、分两批发货、冷链路线、认养证书**；
   - "权益兑现"区展示已收到 / 在途 / 赔付折算，冰雹保险赔付+补发按金额折回产量，
     区分"已公平兑现"与"含在途批次的预计兑现"；
   - 可查看坏果投诉工单中 6 个角色的完整处理流水。
3. 用 **农场主 `13900000001`** 登录"农场工作台" → 点击"东坡3号苹果园"查看**地块完整账**：
   六类成本、冰雹灾害、成熟度抽检、采摘、分拣、发货、赔付、回访一页到底。
4. 在任一进行中项目页用农场操作栏走一遍：**记灾害（自动建工单）→ 成熟度抽检 → 安排采摘 →
   采摘工账号回填 → 包装员分拣 → 编批发货 → 物流发车/送达 → 季末结算**。
5. 用 **客服 `13900000006`** 打开异常工单，追加各角色处理事件并"登记赔付并结案"，
   观察工单状态自动变为"已赔付"、赔付折算回认养人权益。
6. **坏果赔付判定（蓝莓故事线，工单 #10）**：用客服打开"异常工单 → 蓝莓开箱坏果较多（约18%）"，
   可看到平台自动关联的完整证据链——**采摘时间（6/25 06:40）、分拣照片、冷链温度曲线（北京分拨脱冷至 9.2℃）、
   签收时间、开箱照片与坏果比例**；客服结论、运费与补发成本分列（原单运费 ¥18 / 补发果品 ¥26 / 补发运费 ¥12 / 退款 ¥15）、
   ①赔付结论同步认养档案（陈曦"我的认养"蓝莓订单可见）、②补发单 `RSHIP-BERRY-2026-A-010` 已同步物流端（第 2 批补发运单）、
   ③冷链责任通知单 `LTC-BERRY-2026-A-010` 已同步物流并被物流确认。
   也可对新工单点"建立坏果赔付判定 → 补充开箱证据 → 按坏果比例生成方案（脱冷→物流追责 / ≥10%→退款 / 其余→补发）→
   关闭并三向同步"，关闭时系统强制校验开箱照片、冷链曲线、客服结论均已保留。
7. 用 **管理员 `13800000000`** 查看平台总览：品类销售、公益代捐份额、保险到账、结季成本与满意度。

已内置的完整故事线（批次 `APPLE-2026-A`）：

- 6 月冰雹 → 农场拍照报灾、保险报案与到账、客服向全部份额推送三选一方案；
- 首批坏果率偏高 → 认养人投诉、物流出示温控记录、包装员整改、农场补发、客服回访闭环；
- 蓝莓批次分拨停电脱冷（9.2℃）→ 认养人开箱反馈坏果 18%，平台关联采摘时间/分拣照片/冷链曲线/签收开箱时间与照片，
  客服按坏果比例生成**物流追责方案**：开箱照片、冷链曲线、客服结论关闭前归档保留；原单运费、补发果品成本、
  补发运费分开计责；赔付结论同步认养档案，补发订单与冷链责任同步物流端并被物流确认；
- 认养人出差 → 第二批发货前改地址，客服审核、物流更新冷链路线；
- 企业团购"净重/毛重"口径争议 → 包装员核对规格、农场发下季抵扣券、平台修订标注口径；
- 最终交付记录沉淀产量、发货、赔付、满意度、续认率与下一季建议。

---

## 四、本地开发（不用 Docker）

需要：Go 1.19+、Node.js 20+、PostgreSQL 15。

### 1. 准备数据库

```bash
createdb farmcrowd
# 数据库表结构与演示数据由后端启动时自动迁移（backend/internal/db/migrations/*.sql）
```

### 2. 启动后端（:8080）

```bash
cd backend
export DATABASE_URL="postgres://用户:密码@127.0.0.1:5432/farmcrowd?sslmode=disable"
go run ./cmd/server
```

后端在无 `STATIC_DIR` 时只提供 API，适合前后端分离开发。

### 3. 启动前端（:5173）

```bash
cd frontend
npm install
npm run dev        # 已在 vite.config.js 中把 /api 代理到 http://127.0.0.1:8080
```

打开 <http://localhost:5173>。生产模式下也可只跑后端并让 Go 托管前端构建产物：

```bash
cd frontend && npm run build
cd ../backend && STATIC_DIR=../frontend/dist go run ./cmd/server   # 然后访问 :8080
```

### 4. 测试

```bash
cd backend && go test ./...        # 单元测试
```

端到端 API 验证脚本 `backend/scripts/e2e_test.py`（仅依赖 Python3 标准库，无第三方包），
**需在全新初始化的数据库上运行**（脚本会新建认养、工单、结算等数据）：

```bash
# 1. 用空库启动后端（迁移+种子会自动执行）
# 2. 运行 92 项端到端断言（默认目标 http://127.0.0.1:8080，可用 FARMCROWD_BASE 覆盖）
python3 backend/scripts/e2e_test.py
# 容器场景：FARMCROWD_BASE=http://localhost:8080 python3 backend/scripts/e2e_test.py
```

覆盖：七角色登录鉴权、认养付款/超卖、保底权益量化核对（已发货+赔付折算）、
改地址拦截与建工单、灾害自动工单、成熟度抽检、采摘回填权限、分拣、发货状态机与
非法跳转拦截、全部送达订单自动完成、坏果工单 5 角色流水、赔付自动结案、
**坏果赔付判定全流程（证据链自动关联、物流测温、开箱照片/坏果比例/签收开箱时间、
按比例生成补发/退款/物流追责方案、关闭前证据保留校验、运费与补发成本分开、
认养档案/补发订单/冷链责任三向同步、物流确认责任、蓝莓种子故事线）**、
保险报案→到账、回访评分校验、直播/参观/团购/公益、地块台账、看板、季末结算。

---

## 五、主要 API（JSON，除登录/浏览外均需 `Authorization: Bearer <token>`）

| 方法 & 路径 | 说明 |
|---|---|
| `POST /api/auth/login` `POST /api/auth/register` `GET /api/auth/me` | 登录/注册/当前用户 |
| `GET /api/projects` `GET /api/projects/{id}` `POST /api/projects` | 项目列表/聚合详情（农事+抽检+采摘+分拣+发货+工单+理赔+结算）/发布 |
| `POST /api/projects/{id}/logs` | 农事/施肥/用药/**灾害**日志（灾害自动建工单） |
| `POST /api/projects/{id}/checks` | 成熟度抽检（糖度、分级、坏果率、预估产量） |
| `POST /api/projects/{id}/harvests` `POST .../harvests/{hid}/complete` | 安排采摘 / 采摘工回填 |
| `POST /api/projects/{id}/sorting` | 分拣与包装规格 |
| `POST /api/projects/{id}/shipments` `POST .../shipments/{sid}/advance` | 分批发货编排 / 状态机 `planned→picked→packed→shipped→delivered` |
| `POST /api/projects/{id}/costs` `POST .../settle` | 地块记账 / 季末最终结算 |
| `POST /api/adoptions` `GET /api/my/adoptions` `POST /api/adoptions/{id}/address` | 付款（自动发证书，支持公益代捐）/ 我的权益核对 / 临时改地址 |
| `GET/POST /api/incidents` `GET/POST /api/incidents/{id}` `POST /api/incidents/{id}/events` | 异常工单与多角色处理流水 |
| `POST /api/projects/{id}/shipments/{sid}/readings` | 物流登记冷链测温点（形成冷链温度曲线证据） |
| `POST /api/bad-fruit-claims` `GET /api/bad-fruit-claims/{id}` `POST .../{id}/evidence` `POST .../{id}/decide` `POST .../{id}/close` | 坏果赔付判定：关联证据链 / 补开箱证据 / 客服按坏果比例生成方案 / 关闭并三向同步 |
| `GET /api/reship-orders` | 补发订单（同步物流端的补发运单与果品/运费分列成本） |
| `GET /api/logistics-notices` `POST /api/logistics-notices/{id}/ack` | 冷链责任通知单（同步物流端，物流确认） |
| `POST /api/compensations/incident/{id}` | 登记现金/补发/保险/下季抵扣赔付 |
| `GET/POST /api/insurance-claims` `POST /api/insurance-claims/{id}` | 保险报案/查勘/到账（到账自动生成赔付） |
| `GET/POST /api/callbacks` | 成熟度回访（满意度+续认建议） |
| `GET /api/live` `POST /api/live/{id}` | 农场直播与开播流转 |
| `GET/POST /api/visits` `POST /api/visits/{id}` | 地块参观预约与审核 |
| `GET/POST /api/group-buys` `POST /api/group-buys/{id}` | 企业团购 |
| `GET /api/charity` | 公益认养榜 |
| `GET /api/plots/{id}/ledger` | **地块完整账**（投入→灾害→成熟→采摘→分拣→发货→售后→成本/满意度） |
| `GET /api/farm/dashboard` `GET /api/admin/overview` | 农场看板 / 平台总览 |

> 本项目为含前端业务页面的全栈应用，按任务约定不单独提供 Swagger/OpenAPI UI；
> 接口契约以上表与 `backend/internal/api` 源码为准。

---

## 六、数据模型要点

- 所有业务记录都携带 `project_id + batch_no`（认养批次号），这是把各方处理结果
  "串到同一认养批次"的主线；`incident_events` 以 `actor_role` 记录 6 类角色的每一步处置。
- `projects.guarantee_kg` 为每份保底产量；认养人权益 = 已发货量 + 赔付折算量（现金/抵扣按
  单价折回产量，补发按果品单价折算），在 `/api/my/adoptions` 的 `rights_fulfillment` 中量化呈现。
- `plot_costs` 六类成本（input/disaster/harvest/sorting/logistics/compensation）+
  `callbacks` 满意度/续认意愿 + `settlements` 最终交付，构成结季后的数据沉淀。
- 坏果赔付判定以 `bad_fruit_claims` 为核心：关联 `harvest_tasks.harvested_at`（采摘时间）、
  `sorting_records.photos`（分拣照片）、`cold_chain_readings`（冷链温度曲线，超 6℃ 判定脱冷）、
  签收/开箱时间与 `open_box_photos`（开箱照片）；`plan` 按坏果比例给出
  reship/refund/logistics_liability，`shipping_fee`（原单运费）、`reship_cost`（补发果品）、
  `reship_freight`（补发运费）、`refund_amount` 分开列示。关闭时强制证据保留并写入
  `adoption_archive_notes`（认养档案）、`reship_orders`（补发订单→生成补发 shipment 同步物流端）、
  `logistics_notices`（冷链责任同步物流端，物流 ack 确认）。

## 七、目录结构

```
.
├── docker-compose.yml          # db + app 一键启动
├── Dockerfile                  # 三阶段：node 构建前端 → go 构建后端 → alpine 运行
├── backend/
│   ├── cmd/server/main.go      # 入口（API + SPA 托管 + CORS + 自动迁移）
│   ├── internal/api/           # 路由与全部业务处理器
│   └── internal/db/
│       ├── db.go               # 连接池、迁移运行器
│       └── migrations/         # 001 建表 / 002 演示数据（go:embed）
└── frontend/
    └── src/
        ├── App.jsx             # 路由与角色化导航
        ├── api.js auth.jsx
        └── pages/              # 项目/详情/我的认养/工单/农场工作台/地块账/服务/平台总览
```

## 八、实际执行过的验证（本环境）

- `go build ./...`、`go vet ./...`、`gofmt`：通过；`go test ./...`：通过（数值转换/折算单测）。
- 对真实 PostgreSQL 15 从零库依次加载 `001/002/003` 迁移：业务表 28 张（另含迁移记录表）创建成功；
  003 迁移（坏果赔付判定）可重复执行（幂等），种子故事线不产生重复数据。
- 启动 Go 服务后执行了 92 项端到端接口断言（真实 HTTP + 真实数据库，全新空库），**全部通过**，
  覆盖：七角色登录鉴权、公开浏览、认养付款与证书自动生成、超卖拦截、保底权益量化核对、
  改地址拦截与建工单、灾害日志自动建工单、成熟度抽检、采摘任务与回填权限、分拣、
  发货状态机与非法跳转拦截、全部送达后订单自动完成、坏果工单 5 角色流水串联、
  **坏果赔付判定全流程（自动关联采摘时间/分拣照片/冷链曲线、物流测温登记、开箱照片与坏果比例、
  按比例生成补发/退款/物流追责方案、关闭前缺开箱照片或冷链曲线被强制拦截、
  运费与补发成本分开计算、认养档案/补发订单/冷链责任三向同步、物流端确认责任、权限边界）**、
  **蓝莓种子故事线（工单 #10：18% 坏果、9.2℃ 脱冷、补发单已送达、责任通知已确认）**、
  赔付自动结案、保险报案→到账、回访评分校验、直播/参观/团购/公益、地块台账聚合、
  农场看板、平台总览、季末结算。
- 前端 `npm install` + `npm run build`（Vite）：构建成功（46 模块，JS gzip ≈ 77KB）。
- 用 Go 后端 `STATIC_DIR` 托管前端产物验证：`/`、SPA 深链路 `/my`、静态资源、`/api/*` 同源均正常。
- 局限：本验证环境未安装 Docker 守护进程与无头浏览器，因此**未实际执行**
  `docker compose up` 的容器内联调与浏览器点击；Dockerfile 各构建步骤已分别用
  等价命令（npm ci/build、CGO 构建、alpine 健康检查命令）验证。
