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

# ---- 坏果投诉：多角色处理流水串到同一工单 ----
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
st, _ = call("POST", f"/api/compensations/incident/{iid_new}", T["service"],
             {"amount": 8, "kind": "reship", "note": "补发油麦菜折值8元"})
st, detail = call("GET", f"/api/incidents/{iid_new}", T["service"])
roles = {e["actor_role"] for e in detail["events"]}
check("工单串联 认养人/物流/包装/农场/客服", {"adopter", "logistics", "packer", "farmer", "service"} <= roles, roles)
check("赔付后工单自动转 compensated", detail["incident"]["status"] == "compensated", detail["incident"])

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
check("生成最终交付结算", st == 200 and r["settlement"]["total_yield_kg"] == 96, r)
check("结算含赔付与满意度", r["settlement"]["total_compensation"] >= 8 and r["settlement"]["avg_satisfaction"] == 5,
      r["settlement"])
st, p5b = call("GET", "/api/projects/5")
check("结算后项目标记完成", p5b["project"]["status"] == "completed", p5b["project"]["status"])

# ---- 坏果赔付判定：证据关联 → 客服按坏果比例判定 → 补发/退款/物流追责 ----
def deliver_shipment(project_id, adoption_id, token_advance, curve=None, seq=1):
    st, r = call("POST", f"/api/projects/{project_id}/shipments", T["farmer"], {
        "adoption_id": adoption_id, "seq": seq, "plan_date": "2026-10-20",
        "pack_spec": "坏果赔付测试包装", "cold_route": "烟台-目的地冷链车", "weight_kg": 4})
    assert st == 201, r
    sid = r["id"]
    for st_ in ("picked", "packed"):
        call("POST", f"/api/projects/{project_id}/shipments/{sid}/advance", T["farmer"], {"status": st_})
    st, r = call("POST", f"/api/projects/{project_id}/shipments/{sid}/advance", token_advance,
                 {"status": "shipped", "logistics_company": "顺丰冷运", "tracking_no": f"CLAIM{sid}"})
    assert st == 200, r
    if curve is not None:
        st, r = call("POST", f"/api/shipments/{sid}/cold-chain", T["logistics"],
                     {"cold_chain_curve": curve})
        assert st == 200, r
    st, r = call("POST", f"/api/projects/{project_id}/shipments/{sid}/advance", T["logistics"],
                 {"status": "delivered"})
    assert st == 200 and r["status"] == "delivered", r
    return sid

# 包装员为本批次登记带照片的分拣记录（作为坏果核定证据）
st, r = call("POST", "/api/projects/1/sorting", T["packer"],
             {"spec": "坏果赔付证据分拣规格", "weight_kg": 12, "box_count": 4,
              "photos": [{"caption": "分拣台照片", "url": "/static/demo/sort-evidence.jpg"}]})
check("分拣记录支持上传分拣照片", st == 201 and len(r["photos"]) == 1, r)

# 流程A：冷链达标 + 坏果率 22.2% → 补发（运费与补发成本分开）
st, r = call("POST", "/api/adoptions", T["adopter2"], {
    "project_id": 1, "shares": 1, "recipient_name": "吴琳", "phone": "13900000008",
    "address": "杭州市西湖区文三路478号"})
aid_a = r["adoption"]["id"]
sid_a = deliver_shipment(1, aid_a, T["farmer"], [
    {"t": "09:00", "temp": 2.1}, {"t": "13:00", "temp": 3.8}, {"t": "18:00", "temp": 1.4}])
st, r = call("POST", f"/api/shipments/{sid_a}/cold-chain", T["adopter"],
             {"cold_chain_curve": [{"t": "x", "temp": 2}]})
check("非物流/农场不能登记冷链曲线", st == 403, r)
st, r = call("POST", "/api/claims/bad-fruit", T["adopter2"], {
    "adoption_id": aid_a, "shipment_id": sid_a, "total_count": 36, "bad_count": 8,
    "open_box_photos": [{"caption": "开箱照", "url": "/static/demo/open-a.jpg"}],
    "description": "8 枚碰压伤"})
check("坏果反馈建工单并生成判定单", st == 201 and r["incident_id"] and r["claim"]["id"], r)
cid_a, iid_a = r["claim"]["id"], r["incident_id"]
c = r["claim"]
check("坏果比例 22.22%", c["bad_rate_pct"] == 22.22, c["bad_rate_pct"])
check("系统建议方案=补发", c["recommended_scheme"] == "reship", c["recommended_scheme"])
check("关联采摘时间", c["harvest_at"] is not None, c["harvest_at"])
check("关联分拣照片", len(c["sorting_photos"]) >= 1, c["sorting_photos"])
check("关联签收时间/冷链路线/运单号", c["signed_at"] and "冷链车" in c["cold_route"] and c["tracking_no"] == f"CLAIM{sid_a}",
      (c["signed_at"], c["cold_route"], c["tracking_no"]))
check("保留开箱照片", len(c["open_box_photos"]) == 1, c["open_box_photos"])
check("冷链达标未脱冷", c["cold_breached"] is False and abs(c["cold_temp_max"] - 3.8) < 0.001, (c["cold_breached"], c["cold_temp_max"]))
st, _ = call("POST", "/api/claims/bad-fruit", T["adopter2"],
             {"adoption_id": aid_a, "shipment_id": sid_a, "total_count": 36, "bad_count": 1})
check("同一运单重复坏果赔付被拒", st == 409)
st, _ = call("POST", f"/api/claims/bad-fruit/{cid_a}/judge", T["adopter2"],
             {"scheme": "reship", "conclusion": "x"})
check("认养人不能出具判定", st == 403)
st, _ = call("POST", f"/api/claims/bad-fruit/{cid_a}/close", T["service"], {})
check("未判定先关闭被拒", st == 409)
st, r = call("POST", f"/api/claims/bad-fruit/{cid_a}/judge", T["service"], {
    "scheme": "reship", "reship_cost": 30, "freight_cost": 12, "cold_chain_responsible": False,
    "conclusion": "包装缓冲不足碰压伤，补发 8 枚；补发成本与运费分开核算。"})
check("客服出具补发判定", st == 200 and r["claim"]["status"] == "judged", r)
st, _ = call("POST", f"/api/claims/bad-fruit/{cid_a}/close", T["adopter2"], {})
check("认养人不能关闭赔付", st == 403)
st, r = call("POST", f"/api/claims/bad-fruit/{cid_a}/close", T["service"], {})
check("客服关闭补发赔付", st == 200 and r["claim"]["status"] == "closed", r)
reship_id = r["claim"]["reship_shipment_id"]
check("补发订单同步物流端(planned+is_reship)", reship_id is not None, reship_id)
st, p1n = call("GET", "/api/projects/1")
rs = next(s for s in p1n["shipments"] if s["id"] == reship_id)
check("补发运单标记 is_reship 并回链赔付单", rs["is_reship"] is True and rs["claim_id"] == cid_a and rs["status"] == "planned", rs)
st, det = call("GET", f"/api/incidents/{iid_a}", T["service"])
check("补发赔付记录按补发成本入账", any(x["kind"] == "reship" and x["amount"] == 30 for x in det["compensations"]), det["compensations"])
check("补发关闭后工单转已赔付", det["incident"]["status"] == "compensated", det["incident"])
st, ships = call("GET", "/api/shipments?reship=1", T["logistics"])
check("物流端可查补发运单", st == 200 and any(s["id"] == reship_id for s in ships), st)
st, lc = call("GET", "/api/claims/bad-fruit", T["logistics"])
check("非冷链责任单不同步给物流", all(x["id"] != cid_a for x in lc), [x["id"] for x in lc])
st, mine = call("GET", "/api/my/adoptions", T["adopter2"])
ca = next(c for a in mine if a["id"] == aid_a for c in a["bad_fruit_claims"] if c["id"] == cid_a)
check("赔付结论同步认养档案(补发成本/运费分列)",
      ca["scheme"] == "reship" and ca["reship_cost"] == 30 and ca["freight_cost"] == 12 and ca["status"] == "closed", ca)
st, led = call("GET", "/api/plots/1/ledger", T["farmer"])
check("地块台账聚合坏果赔付单", any(x["id"] == cid_a for x in led["bad_fruit_claims"]), led.keys())

# 流程B：冷链脱冷（9.4℃）+ 坏果率 50% → 物流追责
st, r = call("POST", "/api/adoptions", T["adopter2"], {
    "project_id": 1, "shares": 1, "recipient_name": "吴琳", "phone": "13900000008",
    "address": "杭州市西湖区文三路478号"})
aid_b = r["adoption"]["id"]
sid_b = deliver_shipment(1, aid_b, T["logistics"], seq=2, curve=[
    {"t": "08:00", "temp": 2.2}, {"t": "12:00", "temp": 7.8}, {"t": "16:00", "temp": 9.4},
    {"t": "20:00", "temp": 6.1}, {"t": "24:00", "temp": 3.0}])
st, r = call("POST", "/api/claims/bad-fruit", T["adopter2"], {
    "adoption_id": aid_b, "total_count": 10, "bad_count": 5,
    "open_box_photos": ["/static/demo/open-b1.jpg", "/static/demo/open-b2.jpg"]})
check("脱冷运单坏果反馈成功", st == 201, r)
cid_b = r["claim"]["id"]
check("脱冷→建议物流追责(优先于退款)", r["claim"]["recommended_scheme"] == "logistics_claim", r["claim"]["recommended_scheme"])
check("冷链曲线快照含脱冷极值", r["claim"]["cold_breached"] is True and abs(r["claim"]["cold_temp_max"] - 9.4) < 0.001
      and len(r["claim"]["cold_chain_curve"]) == 5 and len(r["claim"]["open_box_photos"]) == 2, r["claim"])
st, r = call("POST", f"/api/claims/bad-fruit/{cid_b}/judge", T["service"], {
    "scheme": "logistics_claim", "reship_cost": 200, "freight_cost": 80,
    "conclusion": "中转脱冷链致变质，物流担责，补发并向物流追偿运费。"})
check("物流追责判定(冷链责任自动置真)", st == 200 and r["claim"]["cold_chain_responsible"] is True, r["claim"])
st, r = call("POST", f"/api/claims/bad-fruit/{cid_b}/close", T["service"], {})
check("关闭物流追责赔付并同步物流", st == 200 and r["claim"]["cold_synced_at"] is not None
      and r["claim"]["reship_shipment_id"] is not None, r["claim"])
st, lc = call("GET", "/api/claims/bad-fruit", T["logistics"])
check("冷链责任判定同步给物流", any(x["id"] == cid_b and x["freight_cost"] == 80 for x in lc), [(x["id"], x["cold_chain_responsible"]) for x in lc])
st, led = call("GET", "/api/plots/1/ledger", T["farmer"])
check("运费(物流成本)与补发成本(赔付成本)分开入账",
      sum(x["amount"] for x in led["costs"] if x["cost_type"] == "logistics") >= 80
      and sum(x["amount"] for x in led["costs"] if x["cost_type"] == "compensation") >= 200, led["costs"])

# 流程C：坏果率 40% 且冷链达标 → 退款（不产生补发运单）
st, r = call("POST", "/api/adoptions", T["adopter2"], {
    "project_id": 4, "shares": 1, "recipient_name": "吴琳", "phone": "13900000008",
    "address": "杭州市西湖区文三路478号"})
aid_c = r["adoption"]["id"]
sid_c = deliver_shipment(4, aid_c, T["farmer"], seq=1)
st, r = call("POST", "/api/claims/bad-fruit", T["adopter2"],
             {"adoption_id": aid_c, "total_count": 10, "bad_count": 4})
check("坏果率40%冷链达标→建议退款", st == 201 and r["claim"]["recommended_scheme"] == "refund",
      (st, r.get("recommended_scheme")))
cid_c, iid_c = r["claim"]["id"], r["incident_id"]
st, r = call("POST", f"/api/claims/bad-fruit/{cid_c}/judge", T["service"],
             {"scheme": "refund", "refund_amount": 269, "conclusion": "坏果率40%，全额退款。"})
check("客服出具退款判定", st == 200 and r["claim"]["scheme"] == "refund", r)
st, r = call("POST", f"/api/claims/bad-fruit/{cid_c}/close", T["service"], {})
check("退款关闭不生成补发运单", st == 200 and r["claim"]["reship_shipment_id"] is None, r["claim"])
st, det = call("GET", f"/api/incidents/{iid_c}", T["service"])
check("退款按现金赔付记录到账", any(x["kind"] == "cash" and x["amount"] == 269 and x["status"] == "paid"
                                for x in det["compensations"]), det["compensations"])
st, all_claims = call("GET", "/api/claims/bad-fruit", T["service"])
check("客服可查全部坏果赔付判定单", st == 200 and len(all_claims) >= 3, len(all_claims or []))

# ---- 权限边界 ----
st, r = call("POST", "/api/projects", T["adopter"], {"plot_id": 1, "batch_no": "X", "kind": "apple",
                                                     "title": "x", "price_per_share": 1})
check("认养人不能发布项目", st == 403)
st, r = call("POST", "/api/projects/1/checks", T["logistics"], {"maturity_pct": 1})
check("非农场人员不能登记抽检(应403)", st == 403, r)

passed = sum(1 for _, c, _ in results if c)
print(f"\n==== {passed}/{len(results)} 通过 ====")
sys.exit(0 if passed == len(results) else 1)
