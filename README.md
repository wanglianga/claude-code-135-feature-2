# 🌱 谷香认养 · 农产品众筹认养配送与成熟度回访平台

围绕"同一认养批次"把 **农场、认养人、采摘工、包装员、物流、客服** 的全部处理结果串成一条链：
地块 → 农事投入 → 施肥用药 → 天气灾害 → 成熟度抽检 → 采摘 → 分拣 → 分批发货（冷链）→
坏果反馈与赔付判定（采摘时间/分拣照片/冷链温度曲线/签收时间/开箱照片五证关联）→
补发/退款/物流追责 → 保险理赔 → 成熟度回访 → 最终交付结算 → 地块成本与客户满意度沉淀。

技术栈：**React 18（Vite）+ Go 1.19（net/http + pgx）+ PostgreSQL 15**，单镜像部署、`docker compose` 一键启动。

---

## 一、一键启动（Docker Compose，推荐）

前置：已安装 Docker 与 Docker Compose。

```bash
cd /workspace/135-feature-2-b
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

首次启动时后端会自动执行数据库迁移并写入演示数据（含冰雹减产、苹果坏果投诉、蓝莓坏果赔付判定
（补发 + 冷链脱冷物流追责两条故事线）、临时改地址、企业团购权益口径争议等完整场景），无需手工初始化。停止用 `docker compose down`，
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
6. **坏果赔付判定（蓝莓批次 `BERRY-2026-A` 两条完整故事线）**：
   - 用 **认养人陈曦 `13900000002`** 打开"我的认养"中的蓝莓订单，可见两笔已关闭的坏果赔付结论：
     散单坏果率 25%、冷链达标 → **补发**（补发成本 ¥45 与运费 ¥18 分开核算）；
     在"异常工单 #5"可核对关联的采摘时间、分拣照片、冷链温度曲线、签收时间、开箱照片五证与补发运单。
   - 用 **物流 `13900000005`** 登录"异常工单"，顶部"物流协同"显示**冷链责任已同步物流**
     （星辰科技团购坏果率 40%、中转脱冷最高 9.4℃ → **物流追责**，运费 ¥260 向物流追偿）
     与两条**补发运单（补发订单同步物流端）**。
   - 全新走一遍：任一已送达订单在"我的认养"点"🫐 反馈坏果"上传开箱照 →
     客服在工单页查看五证与冷链曲线图，系统按"脱冷→物流追责 / ≥30%→退款 / 其余→补发"给建议 →
     "出具赔付判定"（退款/补发成本/运费分列）→"关闭赔付"，观察补发运单进入物流待发货、结论回写认养档案。
7. 用 **管理员 `13800000000`** 查看平台总览：品类销售、公益代捐份额、保险到账、结季成本与满意度。

已内置的完整故事线（批次 `APPLE-2026-A`）：

- 6 月冰雹 → 农场拍照报灾、保险报案与到账、客服向全部份额推送三选一方案；
- 首批坏果率偏高 → 认养人投诉、物流出示温控记录、包装员整改、农场补发、客服回访闭环；
- 蓝莓季坏果赔付判定 → 认养人开箱反馈（坏果比例+开箱照）→ 平台关联采摘时间、分拣照片、
  冷链温度曲线、签收时间 → 客服按坏果比例出具补发/退款/物流追责方案 →
  补发订单同步物流端、冷链责任同步物流、运费与补发成本分开核算、结论同步认养档案；
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
# 2. 运行 101 项端到端断言（默认目标 http://127.0.0.1:8080，可用 FARMCROWD_BASE 覆盖）
python3 backend/scripts/e2e_test.py
# 容器场景：FARMCROWD_BASE=http://localhost:8080 python3 backend/scripts/e2e_test.py
```

覆盖：七角色登录鉴权、认养付款/超卖、保底权益量化核对（已发货+赔付折算）、
改地址拦截与建工单、灾害自动工单、成熟度抽检、采摘回填权限、分拣（含分拣照片）、发货状态机与
非法跳转拦截、全部送达订单自动完成、坏果工单 5 角色流水、赔付自动结案、
**坏果赔付判定全链路**（五证关联：采摘时间/分拣照片/冷链曲线/签收时间/开箱照片；
坏果比例建议补发/退款/物流追责；运费与补发成本分开；补发运单同步物流端；
冷链责任同步物流；结论同步认养档案）、
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
| `POST /api/compensations/incident/{id}` | 登记现金/补发/保险/下季抵扣赔付 |
| `POST/GET /api/claims/bad-fruit` `GET /api/claims/bad-fruit/{id}` `POST .../{id}/judge` `POST .../{id}/close` | **坏果赔付判定**：反馈+五证快照 / 详情 / 客服按坏果比例判定补发·退款·物流追责 / 关闭执行同步 |
| `POST /api/shipments/{id}/cold-chain` `GET /api/shipments?reship=1` | 物流登记冷链温度曲线（>6℃ 自动判脱冷）/ 补发运单同步物流端 |
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
- `bad_fruit_claims` 是坏果赔付判定主表：自动快照采摘时间、分拣照片、签收时间、
  冷链温度曲线（`shipments.cold_chain_curve` 温度点序列 + 极值 + `cold_breached` 脱冷标记）
  与认养人开箱照片；客服按坏果比例选择 `reship / refund / logistics_claim`，
  `refund_amount / reship_cost / freight_cost` 三笔金额分列，赔付关闭后证据与结论仍保留；
  补发会写入带 `is_reship/claim_id` 标记的运单同步物流端，冷链责任由 `cold_synced_at` 留痕同步物流。

## 七、目录结构

```
.
├── docker-compose.yml          # db + app 一键启动
├── Dockerfile                  # 三阶段：node 构建前端 → go 构建后端 → alpine 运行
├── backend/
│   ├── cmd/server/main.go      # 入口（API + SPA 托管 + CORS + 自动迁移）
│   ├── internal/api/           # 路由与全部业务处理器（claims.go = 坏果赔付判定）
│   └── internal/db/
│       ├── db.go               # 连接池、迁移运行器
│       └── migrations/         # 001 建表 / 002 演示数据 / 003 坏果赔付判定（go:embed）
└── frontend/
    └── src/
        ├── App.jsx             # 路由与角色化导航
        ├── api.js auth.jsx
        ├── components/BadFruitClaim.jsx # 坏果证据链/冷链曲线图/判定弹窗/反馈弹窗
        └── pages/              # 项目/详情/我的认养/工单/农场工作台/地块账/服务/平台总览
```

## 八、实际执行过的验证（本环境）

- `go build ./...`、`go vet ./...`、`gofmt`：通过；`go test ./...`：通过（数值转换/折算单测）。
- 对真实 PostgreSQL 15 依次加载 `001/002/003` 迁移：表结构创建成功，蓝莓坏果赔付两条
  演示故事线（补发 / 冷链脱冷物流追责）落库正确，种子数据可重复执行（幂等）。
- 启动 Go 服务后执行了 **101 项**端到端接口断言（真实 HTTP + 全新初始化的真实数据库），**全部通过**，
  覆盖：七角色登录鉴权、公开浏览、认养付款与证书自动生成、超卖拦截、保底权益量化核对、
  改地址拦截与建工单、灾害日志自动建工单、成熟度抽检、采摘任务与回填权限、分拣（含分拣照片）、
  发货状态机与非法跳转拦截、全部送达后订单自动完成、坏果工单 5 角色流水串联、
  赔付自动结案、
  **坏果赔付判定全链路**（五证关联、坏果比例建议补发/退款/物流追责、越权与重复提交拦截、
  运费与补发成本分开入账、补发运单同步物流端、冷链责任同步物流、结论同步认养档案、地块台账聚合）、
  保险报案→到账、回访评分校验、直播/参观/团购/公益、地块台账聚合、
  农场看板、平台总览、季末结算。
- 前端 `npm install` + `npm run build`（Vite）：构建成功（47 模块，JS gzip ≈ 77KB）。
- 用 Go 后端 `STATIC_DIR` 托管前端产物验证：`/`、SPA 深链路 `/my`、静态资源、`/api/*` 同源均正常。
- 局限：本验证环境未安装 Docker 守护进程与无头浏览器，因此**未实际执行**
  `docker compose up` 的容器内联调与浏览器点击；Dockerfile 各构建步骤已分别用
  等价命令（npm ci/build、CGO 构建、alpine 健康检查命令）验证。
