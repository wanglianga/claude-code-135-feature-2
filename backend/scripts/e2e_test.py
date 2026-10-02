#!/usr/bin/env python3
# 端到端 API 验证：覆盖认养付款/证书/农事/灾害/抽检/采摘/分拣/发货状态机/
# 改地址/坏果投诉/权益争议/多角色工单流水/赔付/保险/回访/直播/参观/团购/公益/台账/看板/结算
import json
import os
import sys
import urllib.request
import urllib.error

B = os.environ.get('FARMCROWD_BASE', 'http://127.0.0.1:8080')
results = []

def call(method, path, token=None, body=None):
    url = B + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req) as r:
            return r.status, json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read().decode())
        except Exception:
            return e.code, {}

def near(a, b, tol=0.01):
    try:
        return abs(float(a) - float(b)) <= tol
    except (TypeError, ValueError):
        return False

def check(name, cond, detail=""):
    results.append((name, cond, detail))
    print(("PASS" if cond else "FAIL") + ": " + name + ("" if cond else "  -> " + str(detail)[:200]))

def login(phone):
    st, r = call("POST", "/api/auth/login", body={"phone": phone, "password": "farm123"})
    assert st == 200, r
    return r["token"]

T = {
    "adopter":  login("13900000002"),  # 陈曦
    "adopter2": login("13900000008"),  # 吴琳
    "farmer":   login("13900000001"),  # 李远山
    "picker":   login("13900000003"),
    "packer":   login("13900000004"),
    "logistics":login("13900000005"),
    "service":  login("13900000006"),
    "admin":    login("13800000000"),
}
check("7 个角色全部登录成功", all(T.values()))

# ---- 公开浏览 ----
st, projs = call("GET", "/api/projects")
check("公开浏览 5 个认养项目", st == 200 and len(projs) == 5, len(projs))
st, p1 = call("GET", "/api/projects/1")
need_keys = {"project", "logs", "maturity_checks", "harvest_tasks", "sorting",
             "shipments", "incidents", "insurance_claims", "settlement"}
check("项目详情聚合全链路", need_keys.issubset(p1.keys()), p1.keys())
check("项目农事日志含冰雹", any("冰雹" in l.get("title", "") for l in p1["logs"]))
st, live = call("GET", "/api/live")
check("农场直播公开可见", any("红富士" in x["title"] for x in live))
st, charity = call("GET", "/api/charity")
check("公益认养公开榜", any("敬老院" in x["donee"] for x in charity))
st, _ = call("POST", "/api/adoptions", body={"project_id": 5})
check("未登录认养被拒", st == 401)

# ---- 认养付款（吴琳购买蔬菜 1 份，走全新订单）----
st, r = call("POST", "/api/adoptions", T["adopter2"], {
    "project_id": 5, "shares": 1, "recipient_name": "吴琳", "phone": "13900000008",
    "address": "杭州市西湖区文三路478号华星时代广场"})
check("付款成功并自动生成证书", st == 201 and r["cert_no"].startswith("CERT-VEG-2026-A"), r)
new_aid = r["adoption"]["id"]

st, r = call("POST", "/api/adoptions", T["adopter2"], {
    "project_id": 2, "shares": 999, "recipient_name": "x", "phone": "13900000000", "address": "x"})
check("超卖拦截", st == 409 and "不足" in r.get("error", ""), r)

# ---- 我的认养 · 权益公平兑现（陈曦苹果订单 id=1：保底40kg）----
st, mine = call("GET", "/api/my/adoptions", T["adopter"])
apple = next(a for a in mine if a["kind"] == "apple")
rf = apple["rights_fulfillment"]
print("   苹果权益核对:", rf)
check("保底量=40kg(20×2)", rf["guarantee_kg"] == 40, rf)
check("已发货 20kg（首批12+二批8计划）", rf["shipped_kg"] == 12 and rf["planned_kg"] == 8, rf)
# 补发39.9折1.33kg + 保险560折18.70kg；履约中已实现32kg，计入二批在途8kg后达40kg
check("履约中已实现尚不足(在途批次未发)", rf["fair"] is False, rf)
check("含在途批次后预计公平兑现=true", rf["projected_fair"] is True, rf)
check("赔付含保险560", rf["compensated_amount"] >= 599.9, rf)
check("证书号展示", apple["cert_no"] == "CERT-APPLE-2026-A-0001")
check("分批发货计划与冷链路线", len(apple["shipments"]) == 2 and "冷链车" in apple["shipments"][0]["cold_route"])
check("回访记录可见", len(apple["callbacks"]) == 1 and apple["callbacks"][0]["would_renew"])

# ---- 临时改地址（未发货可改，已发货拦截）----
st, r = call("POST", f"/api/adoptions/{new_aid}/address", T["adopter2"], {
    "recipient_name": "吴父", "phone": "13100000000", "address": "宁波市鄞州区首南街道9号",
    "reason": "出差改寄父母家"})
check("未发货订单改址成功并建工单", st == 200 and r["ok"] and r["incident_id"], r)
st, r = call("POST", "/api/adoptions/1/address", T["adopter"], {"address": "x路1号"})
check("已有发出批次改址被拦截", st == 409, r)

# ---- 农场端：灾害日志自动建工单 ----
st, r = call("POST", "/api/projects/5/logs", T["farmer"], {
    "log_type": "disaster", "title": "暖棚短时大风掀膜",
    "content": "连夜修补，叶菜延后3天上市", "disaster_kind": "大风", "yield_impact_pct": 5})
check("登记灾害农事日志", st == 201 and r["disaster_kind"] == "大风", r)
st, p5 = call("GET", "/api/projects/5")
check("大风灾害自动生成工单并入同一批次",
      any(i["kind"] in ("delay", "other", "hail") and "大风" in i["title"] for i in p5["incidents"]),
      p5["incidents"])

# ---- 成熟度抽检（糖度/分级/坏果率/预估产量）----
st, r = call("POST", "/api/projects/5/checks", T["farmer"], {
    "brix": 6.2, "sample_count": 20, "maturity_pct": 80, "est_yield_kg": 300,
    "bad_rate_pct": 1.2, "grade_dist": {"大": 50, "中": 40, "小": 10}, "notes": "叶菜达标"})
check("成熟度抽检登记", st == 201 and r["maturity_pct"] == 80 and r["grade_dist"]["大"] == 50, r)

# ---- 采摘安排（优先级）+ 采摘工回填 ----
st, r = call("POST", "/api/projects/5/harvests", T["farmer"], {
    "picker_id": 4, "priority": 10, "planned_date": "2026-10-16", "target_kg": 100})
check("农场安排采摘任务", st == 201, r)
hid = r["id"]
st, r = call("POST", f"/api/projects/5/harvests/{hid}/complete", T["picker"],
             {"actual_kg": 96, "bad_rate_pct": 2.1})
check("采摘工回填实际产量与坏果率", st == 200 and r["status"] == "done" and r["actual_kg"] == 96, r)
st, r = call("POST", f"/api/projects/5/harvests/{hid}/complete", T["adopter"],
             {"actual_kg": 1, "bad_rate_pct": 0})
check("非授权人员不能回填采摘", st == 403, r)

# ---- 分拣（包装规格）----
st, r = call("POST", "/api/projects/5/sorting", T["packer"],
             {"spec": "有机叶菜标准箱 8斤", "weight_kg": 96, "box_count": 24})
check("包装员登记分拣规格", st == 201 and r["box_count"] == 24, r)

# ---- 发货状态机 ----
st, r = call("POST", "/api/projects/5/shipments", T["farmer"], {
    "adoption_id": new_aid, "seq": 1, "plan_date": "2026-10-17",
    "pack_spec": "叶菜8斤箱", "cold_route": "烟台-杭州冷链", "weight_kg": 4})
check("编排序批发货计划", st == 201 and r["status"] == "planned", r)
sid = r["id"]
for st_ in ("picked", "packed"):
    st, r = call("POST", f"/api/projects/5/shipments/{sid}/advance", T["farmer"], {"status": st_})
    check(f"发货流转->{st_}", st == 200 and r["status"] == st_, r)
st, r = call("POST", f"/api/projects/5/shipments/{sid}/advance", T["logistics"],
             {"status": "shipped", "logistics_company": "顺丰冷运", "tracking_no": "SFVEG001",
              "bad_fruit_rate": 1.5})
check("物流发车填运单与坏果率", st == 200 and r["tracking_no"] == "SFVEG001", r)
st, r = call("POST", f"/api/projects/5/shipments/{sid}/advance", T["logistics"], {"status": "planned"})
check("非法状态跳转被拒", st == 400, r)
st, r = call("POST", f"/api/projects/5/shipments/{sid}/advance", T["logistics"], {"status": "delivered"})
check("物流确认送达", st == 200 and r["status"] == "delivered", r)
st, ad = call("GET", f"/api/adoptions/{new_aid}", T["adopter2"])
check("全部送达后订单自动完成", ad["status"] == "completed", ad)

# ---- 坏果投诉：多角色处理流水 + 坏果赔付判定（证据链/比例方案/三向同步）----
st, r = call("POST", "/api/incidents", T["adopter2"], {
    "project_id": 5, "adoption_id": new_aid, "kind": "bad_fruit",
    "title": "蔬菜箱有两株压伤", "description": "开箱发现最下层两株油麦菜压伤"})
check("认养人提交坏果工单", st == 201, r)
iid_new = r["id"]
st, _ = call("POST", f"/api/incidents/{iid_new}/events", T["logistics"],
             {"action": "提供冷链温控记录", "note": "全程2-6℃，无脱冷"})
st, _ = call("POST", f"/api/incidents/{iid_new}/events", T["packer"],
             {"action": "包装复查", "note": "叶菜筐底部缓冲不足，加垫泡沫垫"})
st, _ = call("POST", f"/api/incidents/{iid_new}/events", T["farmer"],
             {"action": "同意补发", "note": "下批补发同价叶菜"})

# 建立坏果赔付判定：平台自动关联采摘时间
st, r = call("POST", "/api/bad-fruit-claims", T["service"], {"incident_id": iid_new})
check("建立坏果判定并自动关联采摘时间", st == 201 and r["harvest_time"] is not None, r)
new_claim = r["id"]
st, r = call("POST", "/api/bad-fruit-claims", T["service"], {"incident_id": iid_new})
check("同一工单不能重复建立判定", st == 409, r)
st, r = call("POST", "/api/bad-fruit-claims", T["adopter2"], {"incident_id": iid_new})
check("认养人不能建立判定", st == 403, r)

# 物流补测温点（含超温点）→ 曲线证据
for pt, tc in (("烟台仓装车", 3.2), ("济南分拨中转停电", 8.5), ("末端网点", 5.1)):
    st, rr = call("POST", f"/api/projects/5/shipments/{sid}/readings", T["logistics"],
                  {"point": pt, "temp_c": tc})
check("冷链测温点登记（温度曲线证据）", st == 201 and near(rr["temp_c"], 5.1), rr)

# 认养人补充开箱照片/坏果比例/签收开箱时间，并重新快照冷链曲线
st, r = call("POST", f"/api/bad-fruit-claims/{new_claim}/evidence", T["adopter2"], {
    "reported_bad_pct": 12, "total_count": 24, "bad_count": 3,
    "open_box_photos": [{"caption": "开箱压伤两株"}, {"caption": "近景出水"}, {"caption": "外箱完好"}],
    "signed_at": "2026-10-17T15:00:00+08:00", "opened_at": "2026-10-17T18:00:00+08:00",
    "shipment_id": sid})
check("开箱证据补充并快照冷链曲线",
      st == 200 and r["cold_breach"] is True and near(r["cold_max_c"], 8.5)
      and len(r["open_box_photos"]) == 3 and r["bad_count"] == 3, r)

# 客服按坏果比例生成方案（脱冷 → 建议物流追责；运费与补发成本分开）
st, r = call("POST", f"/api/bad-fruit-claims/{new_claim}/decide", T["adopter2"],
             {"bad_fruit_pct": 12, "conclusion": "x"})
check("认养人不能生成赔付方案", st == 403, r)
st, r = call("POST", f"/api/bad-fruit-claims/{new_claim}/decide", T["service"], {
    "bad_fruit_pct": 12, "plan": "logistics_liability", "liability_party": "logistics",
    "shipping_fee": 10, "freight_borne_by": "logistics",
    "reship_cost": 9.6, "reship_freight": 8, "refund_amount": 9.6,
    "conclusion": "开箱照片与分拣照片比对包装无异常，冷链曲线证实中转脱冷8.5℃，坏果12%责任在物流；运费与补发成本分开计责。"})
check("客服按坏果比例生成物流追责方案",
      st == 200 and r["claim"]["plan"] == "logistics_liability", r)

# 关闭校验：缺补发重量不能完成补发同步（先给 0 验证补发单不生成、通知仍生成？——直接正常关闭）
st, r = call("POST", f"/api/bad-fruit-claims/{new_claim}/close", T["service"],
             {"reship_weight_kg": 0.6, "reship_spec": "叶菜补发0.6kg"})
check("关闭判定：证据归档+认养档案/补发/冷链责任三向同步",
      st == 200 and r["reship_no"].startswith("RSHIP-VEG-2026-A") and r["notice_no"].startswith("LTC-VEG-2026-A"), r)
st, r = call("POST", f"/api/bad-fruit-claims/{new_claim}/close", T["service"], {"reship_weight_kg": 0.6})
check("重复关闭被拒（证据已归档）", st == 409, r)

st, cd = call("GET", f"/api/bad-fruit-claims/{new_claim}", T["service"])
check("判定单保留开箱照片/冷链曲线/客服结论",
      len(cd["claim"]["open_box_photos"]) == 3 and len(cd["claim"]["cold_curve"]) == 3
      and cd["claim"]["conclusion"] and cd["claim"]["closed_at"], cd["claim"])
check("运费与补发成本分开：果品9.6/补发运费8/原运费10",
      cd["claim"]["reship_cost"] == 9.6 and cd["claim"]["reship_freight"] == 8
      and cd["claim"]["shipping_fee"] == 10, cd["claim"])
check("补发订单已同步物流端（生成补发运单）",
      len(cd["reship_orders"]) == 1 and cd["reship_orders"][0]["status"] == "synced"
      and cd["reship_orders"][0]["freight_cost"] == 8
      and cd["reship_orders"][0]["shipment_id"] is not None, cd["reship_orders"])
check("冷链责任通知单运费追责=原运费+补发运费=18",
      len(cd["logistics_notices"]) == 1 and cd["logistics_notices"][0]["freight_amount"] == 18,
      cd["logistics_notices"])
notice_new = cd["logistics_notices"][0]["id"]
st, r = call("POST", f"/api/logistics-notices/{notice_new}/ack", T["logistics"], {})
check("物流端确认冷链责任", st == 200 and r["status"] == "acknowledged", r)
st, r = call("POST", f"/api/logistics-notices/{notice_new}/ack", T["adopter2"], {})
check("非物流不能确认责任通知单", st == 403, r)

st, detail = call("GET", f"/api/incidents/{iid_new}", T["service"])
roles = {e["actor_role"] for e in detail["events"]}
check("工单串联 认养人/物流/包装/农场/客服", {"adopter", "logistics", "packer", "farmer", "service"} <= roles, roles)
check("关闭判定后工单自动转 compensated", detail["incident"]["status"] == "compensated", detail["incident"])
check("工单详情内嵌坏果判定与冷链曲线",
      detail["bad_fruit_claim"]["plan"] == "logistics_liability"
      and len(detail["cold_chain_curve"]) == 3, detail["bad_fruit_claim"])

st, mine2 = call("GET", "/api/my/adoptions", T["adopter2"])
veg_order = next(a for a in mine2 if a["id"] == new_aid)
check("赔付结论同步认养档案",
      any(n["kind"] == "bad_fruit" and n["detail"]["plan"] == "logistics_liability"
          for n in veg_order["archive_notes"]), veg_order.get("archive_notes"))
check("补发订单在认养档案可见（物流同步状态）",
      len(veg_order["reship_orders"]) == 1 and veg_order["reship_orders"][0]["status"] == "synced",
      veg_order.get("reship_orders"))

# ---- 种子故事线：蓝莓批次 BERRY-2026-A 坏果判定（陈曦订单#4）----
st, berry = call("GET", "/api/incidents/10", T["adopter"])
bc = berry["bad_fruit_claim"]
check("蓝莓坏果判定存在且已关闭归档",
      bc and bc["plan"] == "logistics_liability" and bc["closed_at"] and bc["archive_synced"], bc)
check("蓝莓判定关联采摘/分拣/冷链/签收开箱证据",
      bc["harvest_time"] and len(bc["sorting_photos"]) == 2 and bc["cold_breach"] is True
      and near(bc["cold_max_c"], 9.2) and len(bc["open_box_photos"]) == 3
      and bc["signed_at"] and bc["opened_at"], bc)
check("蓝莓判定运费/补发成本分开且三向同步",
      bc["shipping_fee"] == 18 and bc["reship_cost"] == 26 and bc["reship_freight"] == 12
      and bc["refund_amount"] == 15 and bc["reship_synced"] and bc["logistics_synced"], bc)
check("蓝莓补发单已送达且物流已确认冷链责任",
      bc["reship_orders"][0]["status"] == "delivered"
      and bc["logistics_notices"][0]["status"] == "acknowledged", bc)
check("工单冷链曲线 5 个测温点含脱冷点",
      len(berry["cold_chain_curve"]) == 5
      and any(near(p["temp_c"], 9.2) for p in berry["cold_chain_curve"]), berry["cold_chain_curve"])
st, mine_berry = call("GET", "/api/my/adoptions", T["adopter"])
berry_order = next(a for a in mine_berry if a["id"] == 4)
check("蓝莓认养档案含坏果赔付结论与补发单",
      len(berry_order["archive_notes"]) >= 1 and len(berry_order["reship_orders"]) == 1
      and berry_order["reship_orders"][0]["reship_no"] == "RSHIP-BERRY-2026-A-010",
      berry_order.get("reship_orders"))
st, claims = call("GET", "/api/bad-fruit-claims", T["adopter"])
check("认养人只能看到本人订单的坏果判定", all(c["adoption_id"] in (1, 4) for c in claims), [c["adoption_id"] for c in claims])
st, reships = call("GET", "/api/reship-orders", T["logistics"])
check("物流端可查全部补发订单", any(x["reship_no"] == "RSHIP-BERRY-2026-A-010" for x in reships), reships)
st, notices = call("GET", "/api/logistics-notices", T["logistics"])
check("物流端可查冷链责任通知单", any(x["notice_no"] == "LTC-BERRY-2026-A-010" for x in notices), notices)

# ---- 既有工单：坏果单 id=2 追加客服事件并解决 ----
st, r = call("POST", "/api/incidents/2/events", T["service"],
             {"action": "二次回访", "note": "客户满意", "status": "resolved"})
check("客服推进工单状态", st == 201 and r["action"] == "二次回访", r)
st, detail2 = call("GET", "/api/incidents/2", T["adopter"])
check("认养人可查看本批次完整处理流水", len(detail2["events"]) >= 6 and detail2["incident"]["status"] == "resolved")

# ---- 保险理赔：新建报案并推进到到账 ----
st, r = call("POST", "/api/insurance-claims", T["farmer"], {
    "project_id": 5, "policy_no": "PICC-WIND-2026-009", "kind": "大风",
    "loss_kg": 30, "amount": 600, "note": "大风掀膜损失"})
check("农场保险报案", st == 201 and r["status"] == "applied", r)
claim_id = r["id"]
for st_ in ("surveying", "approved", "settled"):
    st, r = call("POST", f"/api/insurance-claims/{claim_id}", T["service"], {"status": st_})
check("理赔推进到到账", st == 200 and r["status"] == "settled", r)

# ---- 回访（含下一季续认建议）----
st, r = call("POST", "/api/callbacks", T["adopter2"], {
    "adoption_id": new_aid, "satisfaction": 5, "quality_score": 5, "delivery_score": 4,
    "would_renew": True, "comment": "蔬菜新鲜、处理问题很快", "renew_suggestion": "希望增加水果胡萝卜"})
check("成熟度回访+续认建议", st == 201 and r["satisfaction"] == 5, r)
st, r = call("POST", "/api/callbacks", T["adopter2"], {
    "adoption_id": new_aid, "satisfaction": 9, "quality_score": 5, "delivery_score": 4})
check("回访评分越界被拒", st == 400, r)

# ---- 直播 / 参观 / 团购 ----
st, r = call("POST", "/api/projects/1/live", T["farmer"], {
    "title": "分拣车间夜场直播", "stream_url": "https://live.example.com/sort",
    "starts_at": "2026-10-21T19:00:00+08:00"})
check("农场创建直播", st == 201 and r["status"] == "scheduled", r)
live_id = r["id"]
st, r = call("POST", f"/api/live/{live_id}", T["farmer"], {"status": "live"})
check("开播状态流转", st == 200 and r["status"] == "live", r)
st, r = call("POST", "/api/visits", T["adopter"], {
    "plot_id": 2, "visit_date": "2026-11-01", "visitors": 2, "note": "蓝莓园参观"})
check("地块参观预约", st == 201 and r["status"] == "pending", r)
vid = r["id"]
st, r = call("POST", f"/api/visits/{vid}", T["farmer"], {"status": "approved", "note": "欢迎，安排午餐"})
check("农场审核参观预约", st == 200 and r["status"] == "approved", r)
st, r = call("POST", "/api/group-buys", T["adopter"], {
    "project_id": 4, "company_name": "明德教育集团", "contact": "刘老师",
    "phone": "13900000009", "shares": 80, "discount_pct": 12})
check("企业团购需求提交", st == 201 and r["status"] == "pending", r)
gbid = r["id"]
st, r = call("POST", f"/api/group-buys/{gbid}", T["farmer"], {"status": "confirmed"})
check("农场确认团购", st == 200 and r["status"] == "confirmed", r)

# ---- 公益认养下单（代捐）----
st, r = call("POST", "/api/adoptions", T["adopter"], {
    "project_id": 4, "shares": 2, "recipient_name": "西山敬老院", "phone": "13900000010",
    "address": "栖霞市西山镇敬老院", "is_charity": True, "donee": "西山敬老院",
    "message": "祝老人们秋收安康"})
check("公益代捐认养成功", st == 201 and r["cert_no"], r)
st, ch = call("GET", "/api/charity")
check("公益榜新增受益方记录", any(x["donee"] == "西山敬老院" for x in ch))

# ---- 地块完整账 ----
st, led = call("GET", "/api/plots/1/ledger", T["farmer"])
check("地块台账聚合完整链路",
      all(k in led for k in ["costs", "logs", "disasters", "maturity_checks",
                             "harvest_tasks", "sorting", "shipments", "incidents",
                             "compensations", "callbacks"]), led.keys())
check("地块成本台账>0", led["total_cost"] > 0, led["total_cost"])
check("地块台账含冰雹灾害", any(d.get("disaster_kind") == "冰雹" for d in led["disasters"]))
check("地块台账含赔付与回访", len(led["compensations"]) >= 2 and len(led["callbacks"]) >= 1)
check("地块平均满意度计算", led["avg_satisfaction"] >= 4, led["avg_satisfaction"])

# ---- 农场看板 / 平台总览 ----
st, dash = call("GET", "/api/farm/dashboard", T["farmer"])
check("农场看板含项目/地块/工单/发货",
      all(k in dash for k in ["projects", "plots", "open_incidents", "active_shipments", "stats"]))
check("看板统计销售额", dash["stats"]["total_sales"] > 0, dash["stats"])
st, r = call("GET", "/api/admin/overview", T["adopter"])
check("非管理员禁止平台总览", st == 403)
st, ov = call("GET", "/api/admin/overview", T["admin"])
check("平台总览含分类项目与结算", "projects_by_kind" in ov and "recent_settlements" in ov, ov.keys())
check("平台总览含公益份额/保险到账/团购数",
      ov["stats"]["charity_shares"] >= 12 and ov["stats"]["insurance_settled"] >= 6000, ov["stats"])

# ---- 最终结算（蔬菜季，沉淀产量/成本/满意度/续认率）----
st, r = call("POST", "/api/projects/5/settle", T["farmer"],
             {"summary": "蔬菜季按时交付，坏果投诉快速闭环，客户满意，建议下季增水果胡萝卜。"})
check("生成最终交付结算", st == 200 and r["settlement"]["total_yield_kg"] >= 96, r)
check("结算含赔付与满意度", r["settlement"]["total_compensation"] >= 8 and r["settlement"]["avg_satisfaction"] == 5,
      r["settlement"])
st, p5b = call("GET", "/api/projects/5")
check("结算后项目标记完成", p5b["project"]["status"] == "completed", p5b["project"]["status"])

# ---- 权限边界 ----
st, r = call("POST", "/api/projects", T["adopter"], {"plot_id": 1, "batch_no": "X", "kind": "apple",
                                                     "title": "x", "price_per_share": 1})
check("认养人不能发布项目", st == 403)
st, r = call("POST", "/api/projects/1/checks", T["logistics"], {"maturity_pct": 1})
check("非农场人员不能登记抽检(应403)", st == 403, r)

passed = sum(1 for _, c, _ in results if c)
print(f"\n==== {passed}/{len(results)} 通过 ====")
sys.exit(0 if passed == len(results) else 1)
