package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// 坏果赔付判定：
// 认养人反馈坏果较多 → 平台关联采摘时间/分拣照片/冷链温度曲线/签收时间/开箱照片 →
// 客服按坏果比例生成 补发/退款/物流追责 方案 → 关闭前保留开箱照片、冷链曲线、客服结论 →
// 运费与补发成本分开计算 → 结论同步认养档案、补发订单同步物流端、冷链责任同步物流。

const coldMaxStd = 6.0 // 生鲜冷链全程温度上限 ℃（0-6℃）
const coldMinStd = 0.0

// coldSnapshot 从运单测温点快照冷链曲线，并计算极值与是否脱冷
func (s *Server) coldSnapshot(ctx context.Context, shipmentID int64) (curve []byte, minC, maxC float64, breach bool, summary string, n int) {
	rows, err := s.queryRows(ctx,
		`SELECT point, temp_c, recorded_at FROM cold_chain_readings WHERE shipment_id=$1 ORDER BY recorded_at`, shipmentID)
	if err != nil || len(rows) == 0 {
		return []byte("[]"), 0, 0, false, "", 0
	}
	type pt struct {
		Point      string      `json:"point"`
		TempC      float64     `json:"temp_c"`
		RecordedAt interface{} `json:"recorded_at"`
	}
	pts := make([]pt, 0, len(rows))
	minC, maxC = 0, 0
	breachPt := ""
	for i, r := range rows {
		t, _ := r["temp_c"].(float64)
		p, _ := r["point"].(string)
		if i == 0 {
			minC, maxC = t, t
		}
		if t > maxC {
			maxC = t
		}
		if t < minC {
			minC = t
		}
		if t > coldMaxStd || t < coldMinStd {
			breach = true
			breachPt = p
		}
		pts = append(pts, pt{Point: p, TempC: t, RecordedAt: r["recorded_at"]})
	}
	b, _ := json.Marshal(pts)
	summary = fmt.Sprintf("全程 %.1f-%.1f℃，共 %d 个测温点", minC, maxC, len(pts))
	if breach {
		summary += fmt.Sprintf("；%s 时段温度超出 %.0f-%.0f℃ 标准，存在脱冷", breachPt, coldMinStd, coldMaxStd)
	} else {
		summary += "，均在标准区间内，未发现脱冷"
	}
	return b, minC, maxC, breach, summary, len(rows)
}

func (s *Server) handleBadFruitClaims(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	ctx := r.Context()
	if r.Method == http.MethodGet {
		q := `SELECT c.*, p.title AS project_title, i.title AS incident_title,
		              a.recipient_name, sh.tracking_no
		       FROM bad_fruit_claims c
		       JOIN projects p ON p.id=c.project_id
		       JOIN incidents i ON i.id=c.incident_id
		       LEFT JOIN adoptions a ON a.id=c.adoption_id
		       LEFT JOIN shipments sh ON sh.id=c.shipment_id`
		args := []interface{}{}
		conds := []string{}
		switch {
		case u.Role == "adopter":
			conds = append(conds, fmt.Sprintf("c.adoption_id IN (SELECT id FROM adoptions WHERE user_id=$%d)", len(args)+1))
			args = append(args, u.ID)
		case u.Role == "farmer" && u.FarmID != nil:
			conds = append(conds, fmt.Sprintf("p.farm_id=$%d", len(args)+1))
			args = append(args, *u.FarmID)
		}
		if len(conds) > 0 {
			q += " WHERE " + strings.Join(conds, " AND ")
		}
		q += " ORDER BY c.id DESC"
		rows, err := s.queryRows(ctx, q, args...)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, rows)
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	if !requireRole(u, "service", "admin", "farmer") {
		writeErr(w, 403, "只有客服/平台/农场可以建立坏果赔付判定")
		return
	}
	var in map[string]interface{}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	incID, _ := i64(in, "incident_id")
	if incID == 0 {
		writeErr(w, 400, "incident_id 必填")
		return
	}
	var pid int64
	var batchNo, kind string
	var adoptionID int64
	err := s.Pool.QueryRow(ctx,
		`SELECT project_id, batch_no, kind, COALESCE(adoption_id,0) FROM incidents WHERE id=$1`, incID).
		Scan(&pid, &batchNo, &kind, &adoptionID)
	if err != nil {
		writeErr(w, 404, "工单不存在")
		return
	}
	if kind != "bad_fruit" {
		writeErr(w, 400, "该工单不是坏果投诉类型，不能建立坏果赔付判定")
		return
	}
	if exist, _ := s.queryOne(ctx, `SELECT id FROM bad_fruit_claims WHERE incident_id=$1`, incID); exist != nil {
		writeErr(w, 409, "该工单已建立坏果赔付判定")
		return
	}

	// 关联运单：显式指定 > 认养订单最近一批已发出运单
	shipmentID, _ := i64(in, "shipment_id")
	if shipmentID == 0 && adoptionID > 0 {
		_ = s.Pool.QueryRow(ctx,
			`SELECT id FROM shipments WHERE adoption_id=$1 AND status<>'planned' ORDER BY id DESC LIMIT 1`,
			adoptionID).Scan(&shipmentID)
	}

	// 关联采摘时间：本批次最近一次已完成采摘
	var harvestTaskID int64
	var harvestAt interface{}
	_ = s.Pool.QueryRow(ctx,
		`SELECT id, harvested_at FROM harvest_tasks
		 WHERE project_id=$1 AND status='done' AND harvested_at IS NOT NULL
		 ORDER BY harvested_at DESC LIMIT 1`, pid).Scan(&harvestTaskID, &harvestAt)

	// 关联分拣照片：本批次带照片的最近一条分拣记录
	var sortingID int64
	var sortingPhotos []byte = []byte("[]")
	_ = s.Pool.QueryRow(ctx,
		`SELECT id, photos FROM sorting_records
		 WHERE project_id=$1 AND COALESCE(jsonb_array_length(photos),0)>0
		 ORDER BY id DESC LIMIT 1`, pid).Scan(&sortingID, &sortingPhotos)

	// 关联冷链曲线
	curve, minC, maxC, breach, coldSummary, nPoints := []byte("[]"), 0.0, 0.0, false, "", 0
	if shipmentID > 0 {
		curve, minC, maxC, breach, coldSummary, nPoints = s.coldSnapshot(ctx, shipmentID)
	}

	reported, _ := f64(in, "reported_bad_pct")
	total, _ := i64(in, "total_count")
	bad, _ := i64(in, "bad_count")
	if total > 0 && bad > 0 && reported == 0 {
		reported = roundF(float64(bad)/float64(total)*100, 1)
	}
	photos := in["open_box_photos"]
	if photos == nil {
		photos = []interface{}{}
	}

	var cid int64
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO bad_fruit_claims(incident_id, project_id, adoption_id, shipment_id, batch_no,
			reported_bad_pct, total_count, bad_count, open_box_photos, signed_at, opened_at,
			harvest_time, harvest_task_id, sorting_photos, sorting_record_id,
			cold_curve, cold_max_c, cold_min_c, cold_breach, cold_summary, service_id)
		VALUES($1,$2,$3,NULLIF($4,0),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,0),$16,$17,$18,$19,$20,$21)
		RETURNING id`,
		incID, pid, nullableID(adoptionID), shipmentID, batchNo,
		reported, total, bad, photos, in["signed_at"], in["opened_at"],
		harvestAt, nullableID(harvestTaskID), sortingPhotos, sortingID,
		curve, nullableF64(maxC), nullableF64(minC), breach, coldSummary, u.ID).Scan(&cid)
	if err != nil {
		writeErr(w, 400, "建立判定失败："+err.Error())
		return
	}
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
		VALUES($1,$2,$3,'建立坏果赔付判定',$4)`,
		incID, u.ID, u.Role,
		fmt.Sprintf("平台关联证据：采摘时间 %v、分拣照片 %s、冷链测温 %d 点（%s），等待客服按坏果比例判定。",
			harvestAt, strings.Trim(string(sortingPhotos), "[]"), nPoints, coldSummary))

	row, _ := s.queryOne(ctx, `SELECT * FROM bad_fruit_claims WHERE id=$1`, cid)
	writeJSON(w, 201, row)
}

func (s *Server) handleBadFruitClaimByID(w http.ResponseWriter, r *http.Request) {
	id, action, ok := pathParts("/api/bad-fruit-claims/", r.URL.Path)
	if !ok {
		writeErr(w, 404, "判定单不存在")
		return
	}
	if action != "" && action != "evidence" && action != "decide" && action != "close" {
		writeErr(w, 404, "未知操作")
		return
	}
	ctx := r.Context()
	claim, err := s.queryOne(ctx, `SELECT * FROM bad_fruit_claims WHERE id=$1`, id)
	if err != nil {
		writeErr(w, 404, "坏果赔付判定不存在")
		return
	}
	pid, _ := claim["project_id"].(int64)
	adoptionID, _ := claim["adoption_id"].(int64)
	incID, _ := claim["incident_id"].(int64)

	u := s.currentUser(r)
	farmRole := u.Role == "farmer" && u.FarmID != nil
	var farmID int64
	_ = s.Pool.QueryRow(ctx, `SELECT farm_id FROM projects WHERE id=$1`, pid).Scan(&farmID)
	ownerAdopter := u.Role == "adopter"
	if ownerAdopter {
		var owner int64
		if e := s.Pool.QueryRow(ctx, `SELECT COALESCE(user_id,0) FROM adoptions WHERE id=$1`, adoptionID).Scan(&owner); e != nil || owner != u.ID {
			writeErr(w, 403, "只能查看本人订单的赔付判定")
			return
		}
	}
	if farmRole && *u.FarmID != farmID {
		writeErr(w, 403, "只能处理本农场项目的赔付判定")
		return
	}

	if r.Method == http.MethodGet && action == "" {
		reships, _ := s.queryRows(ctx,
			`SELECT * FROM reship_orders WHERE claim_id=$1 ORDER BY id`, id)
		notices, _ := s.queryRows(ctx,
			`SELECT * FROM logistics_notices WHERE claim_id=$1 ORDER BY id`, id)
		notes, _ := s.queryRows(ctx,
			`SELECT * FROM adoption_archive_notes WHERE claim_id=$1 ORDER BY id`, id)
		writeJSON(w, 200, map[string]interface{}{
			"claim": claim, "reship_orders": reships,
			"logistics_notices": notices, "archive_notes": notes,
		})
		return
	}

	// POST /evidence 补充开箱照片/坏果比例/签收开箱时间（认养人本人或客服/农场/管理员）
	if r.Method == http.MethodPost && action == "evidence" {
		if !(requireRole(u, "service", "admin", "farmer") || ownerAdopter) {
			writeErr(w, 403, "无权补充证据")
			return
		}
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		sets := []string{}
		args := []interface{}{}
		add := func(col string, val interface{}) {
			args = append(args, val)
			sets = append(sets, fmt.Sprintf("%s=$%d", col, len(args)))
		}
		if v, ok := f64(in, "reported_bad_pct"); ok {
			add("reported_bad_pct", v)
		}
		if v, ok := i64(in, "total_count"); ok {
			add("total_count", v)
		}
		if v, ok := i64(in, "bad_count"); ok {
			add("bad_count", v)
		}
		if in["open_box_photos"] != nil {
			b, _ := json.Marshal(in["open_box_photos"])
			add("open_box_photos", string(b))
		}
		if in["signed_at"] != nil {
			add("signed_at", in["signed_at"])
		}
		if in["opened_at"] != nil {
			add("opened_at", in["opened_at"])
		}
		// 运单测温点有新增时重新快照冷链曲线（关闭前持续保留最新曲线）
		if sid, ok := i64(in, "shipment_id"); ok {
			curve, minC, maxC, breach, summary, n := s.coldSnapshot(ctx, sid)
			if n > 0 {
				add("shipment_id", sid)
				add("cold_curve", string(curve))
				add("cold_max_c", maxC)
				add("cold_min_c", minC)
				add("cold_breach", breach)
				add("cold_summary", summary)
			}
		}
		if len(sets) == 0 {
			writeErr(w, 400, "没有可更新的证据字段")
			return
		}
		sets = append(sets, "updated_at=now()")
		args = append(args, id)
		q := fmt.Sprintf("UPDATE bad_fruit_claims SET %s WHERE id=$%d RETURNING *",
			strings.Join(sets, ","), len(args))
		row, err := s.queryOne(ctx, q, args...)
		if err != nil {
			writeErr(w, 400, "补充失败："+err.Error())
			return
		}
		_, _ = s.Pool.Exec(ctx, `
			INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
			VALUES($1,$2,$3,'补充坏果证据',$4)`, incID, u.ID, u.Role, asString(in["note"]))
		writeJSON(w, 200, row)
		return
	}

	// POST /decide 客服按坏果比例生成/确认方案
	if r.Method == http.MethodPost && action == "decide" {
		if !requireRole(u, "service", "admin") {
			writeErr(w, 403, "只有客服可以生成赔付方案")
			return
		}
		var in map[string]interface{}
		_ = decode(r, &in)
		badPct, _ := f64(in, "bad_fruit_pct")
		if badPct == 0 {
			badPct, _ = claim["reported_bad_pct"].(float64)
		}
		if badPct <= 0 || badPct > 100 {
			writeErr(w, 400, "核定坏果比例需在 0-100 之间")
			return
		}
		breach, _ := claim["cold_breach"].(bool)
		plan, _ := str(in, "plan")
		if plan == "" {
			plan = suggestPlan(badPct, breach)
		}
		if !map[string]bool{"reship": true, "refund": true, "logistics_liability": true}[plan] {
			writeErr(w, 400, "方案只能是 reship/refund/logistics_liability")
			return
		}
		liability, _ := str(in, "liability_party")
		if liability == "" {
			if plan == "logistics_liability" {
				liability = "logistics"
			} else if breach {
				liability = "shared"
			} else {
				liability = "farm"
			}
		}
		if !map[string]bool{"farm": true, "logistics": true, "shared": true}[liability] {
			writeErr(w, 400, "责任方只能是 farm/logistics/shared")
			return
		}

		// 默认成本：按坏果比例 × 运单重量 × 果品单价（单价=每份价/保底kg）
		var unitPrice, guarantee, shipWeight float64
		_ = s.Pool.QueryRow(ctx, `
			SELECT a.unit_price, p.guarantee_kg, COALESCE(sh.weight_kg,0)
			FROM bad_fruit_claims c
			JOIN adoptions a ON a.id=c.adoption_id
			JOIN projects p ON p.id=c.project_id
			LEFT JOIN shipments sh ON sh.id=c.shipment_id
			WHERE c.id=$1`, id).Scan(&unitPrice, &guarantee, &shipWeight)
		perKg := 0.0
		if guarantee > 0 {
			perKg = unitPrice / guarantee
		}
		affected := roundF(badPct/100*shipWeight, 2)
		fruitDefault := roundF(affected*perKg, 2)
		refundDefault := fruitDefault

		shippingFee, _ := f64(in, "shipping_fee")
		freightBy, _ := str(in, "freight_borne_by")
		if freightBy == "" {
			if plan == "logistics_liability" {
				freightBy = "logistics"
			} else {
				freightBy = "farm"
			}
		}
		reshipCost, hasReship := f64(in, "reship_cost")
		if !hasReship {
			if plan == "refund" {
				reshipCost = 0
			} else {
				reshipCost = fruitDefault
			}
		}
		reshipFreight, _ := f64(in, "reship_freight")
		refundAmount, hasRefund := f64(in, "refund_amount")
		if !hasRefund {
			if plan == "reship" {
				refundAmount = 0
			} else {
				refundAmount = refundDefault
			}
		}
		conclusion, _ := str(in, "conclusion")
		if conclusion == "" {
			writeErr(w, 400, "客服结论必填（关闭前需保留）")
			return
		}
		row, err := s.queryOne(ctx, `
			UPDATE bad_fruit_claims SET plan=$1, liability_party=$2, bad_fruit_pct=$3, conclusion=$4,
			    shipping_fee=$5, freight_borne_by=$6, reship_cost=$7, reship_freight=$8,
			    refund_amount=$9, updated_at=now()
			WHERE id=$10 RETURNING *`,
			plan, liability, badPct, conclusion, shippingFee, freightBy,
			reshipCost, reshipFreight, refundAmount, id)
		if err != nil {
			writeErr(w, 400, "判定失败："+err.Error())
			return
		}
		_, _ = s.Pool.Exec(ctx, `UPDATE incidents SET status='processing' WHERE id=$1`, incID)
		_, _ = s.Pool.Exec(ctx, `
			INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
			VALUES($1,$2,'service','生成坏果赔付方案',$3)`, incID, u.ID,
			fmt.Sprintf("核定坏果比例 %.1f%% → 方案：%s；退款 ¥%.2f、补发果品 ¥%.2f、原单运费 ¥%.2f（%s承担）、补发运费 ¥%.2f，运费与补发成本分开计算。",
				badPct, planLabel(plan), refundAmount, reshipCost, shippingFee, freightPartyLabel(freightBy), reshipFreight))
		writeJSON(w, 200, map[string]interface{}{
			"claim": row, "suggested_plan": plan,
			"affected_kg": affected, "fruit_price_per_kg": roundF(perKg, 2),
		})
		return
	}

	// POST /close 关闭判定 → 同步认养档案 / 补发订单同步物流 / 冷链责任同步物流 / 生成赔付
	if r.Method == http.MethodPost && action == "close" {
		s.closeBadFruitClaim(w, r, id, claim)
		return
	}
	writeErr(w, 405, "方法不允许")
}

// closeBadFruitClaim 关闭坏果赔付判定并完成三向同步
func (s *Server) closeBadFruitClaim(w http.ResponseWriter, r *http.Request, id int64, claim map[string]interface{}) {
	u := s.currentUser(r)
	if !requireRole(u, "service", "admin") {
		writeErr(w, 403, "只有客服可以关闭赔付判定")
		return
	}
	ctx := r.Context()
	plan, _ := claim["plan"].(string)
	conclusion, _ := claim["conclusion"].(string)
	if plan == "" || plan == "pending" {
		writeErr(w, 400, "尚未生成赔付方案，不能关闭")
		return
	}
	if conclusion == "" {
		writeErr(w, 400, "客服结论缺失：赔付关闭前必须保留客服结论")
		return
	}
	// 关闭前证据保留校验：开箱照片 + 冷链曲线
	photos, _ := claim["open_box_photos"].([]interface{})
	curveRaw, _ := claim["cold_curve"].([]interface{})
	if len(photos) == 0 {
		writeErr(w, 400, "开箱照片缺失：赔付关闭前平台必须保留开箱照片")
		return
	}
	if len(curveRaw) == 0 {
		writeErr(w, 400, "冷链曲线缺失：赔付关闭前平台必须保留冷链温度曲线")
		return
	}
	if claim["closed_at"] != nil {
		writeErr(w, 409, "该赔付判定已关闭，证据已归档保留")
		return
	}

	pid, _ := claim["project_id"].(int64)
	batchNo, _ := claim["batch_no"].(string)
	incID, _ := claim["incident_id"].(int64)
	adoptionID, _ := claim["adoption_id"].(int64)
	shipmentID, _ := claim["shipment_id"].(int64)
	badPct, _ := claim["bad_fruit_pct"].(float64)
	shippingFee, _ := claim["shipping_fee"].(float64)
	freightBy, _ := claim["freight_borne_by"].(string)
	reshipCost, _ := claim["reship_cost"].(float64)
	reshipFreight, _ := claim["reship_freight"].(float64)
	refundAmount, _ := claim["refund_amount"].(float64)
	liability, _ := claim["liability_party"].(string)

	var in map[string]interface{}
	_ = decode(r, &in)
	reshipWeight, _ := f64(in, "reship_weight_kg")
	reshipSpec, _ := str(in, "reship_spec")
	if reshipSpec == "" {
		reshipSpec = fmt.Sprintf("坏果补发 %.2fkg（按核定坏果比例 %.1f%%）", reshipWeight, badPct)
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE bad_fruit_claims SET closed_at=now(), updated_at=now() WHERE id=$1`, id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	// 1) 赔付结论同步认养档案
	archiveSummary := fmt.Sprintf(
		"坏果赔付（批次 %s）：核定坏果比例 %.1f%%，方案【%s】，责任方 %s。退款 ¥%.2f；补发果品成本 ¥%.2f、补发运费 ¥%.2f；原单运费 ¥%.2f（%s承担），运费与补发成本分开计算。",
		batchNo, badPct, planLabel(plan), freightPartyLabel(liability),
		refundAmount, reshipCost, reshipFreight, shippingFee, freightPartyLabel(freightBy))
	detail, _ := json.Marshal(map[string]interface{}{
		"plan": plan, "liability_party": liability, "bad_fruit_pct": badPct,
		"refund_amount": refundAmount, "reship_cost": reshipCost,
		"reship_freight": reshipFreight, "shipping_fee": shippingFee,
		"freight_borne_by": freightBy, "reship_weight_kg": reshipWeight,
	})
	var noteID int64
	if adoptionID > 0 {
		if e := tx.QueryRow(ctx, `
			INSERT INTO adoption_archive_notes(adoption_id, project_id, claim_id, batch_no, kind, summary, detail)
			VALUES($1,$2,$3,$4,'bad_fruit',$5,$6) RETURNING id`,
			adoptionID, pid, id, batchNo, archiveSummary, string(detail)).Scan(&noteID); e != nil {
			writeErr(w, 400, "同步认养档案失败："+e.Error())
			return
		}
	}

	// 2) 赔付落账（退款现金 / 补发折值分开两条；备注注明运费另计）
	if refundAmount > 0 {
		if _, e := tx.Exec(ctx, `
			INSERT INTO compensations(incident_id, adoption_id, project_id, amount, kind, status, note, paid_at)
			VALUES($1,$2,$3,$4,'cash','paid',$5,now())`,
			incID, nullableID(adoptionID), pid, refundAmount,
			fmt.Sprintf("坏果 %.1f%% 对应货款退款（责任方：%s）", badPct, freightPartyLabel(liability))); e != nil {
			writeErr(w, 400, "退款登记失败："+e.Error())
			return
		}
	}
	var reshipNo, reshipTracking string
	if (plan == "reship" || plan == "logistics_liability") && reshipCost > 0 && reshipWeight > 0 && adoptionID > 0 {
		reshipNo = fmt.Sprintf("RSHIP-%s-%03d", batchNo, id)
		// 补发订单同步物流端：在发货表中新增补发运单（第 N+1 批，排产中），等待物流发车
		var nextSeq int64
		var recipient, phone, address string
		_ = tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(seq),0)+1 FROM shipments WHERE project_id=$1 AND adoption_id=$2`,
			pid, adoptionID).Scan(&nextSeq)
		_ = tx.QueryRow(ctx,
			`SELECT recipient_name, phone, address FROM adoptions WHERE id=$1`, adoptionID).
			Scan(&recipient, &phone, &address)
		var newShipID int64
		if e := tx.QueryRow(ctx, `
			INSERT INTO shipments(project_id, adoption_id, batch_no, seq, plan_date, pack_spec, cold_route,
				weight_kg, status, logistics_company, tracking_no, operator_id)
			VALUES($1,$2,$3,$4, current_date,$5,'',$6,'planned','顺丰冷运','',$7) RETURNING id`,
			pid, adoptionID, batchNo, nextSeq, reshipSpec, reshipWeight, u.ID).Scan(&newShipID); e != nil {
			writeErr(w, 400, "补发运单同步失败："+e.Error())
			return
		}
		if _, e := tx.Exec(ctx, `
			INSERT INTO reship_orders(claim_id, project_id, adoption_id, shipment_id, batch_no, reship_no,
				spec, weight_kg, fruit_cost, freight_cost, recipient_name, phone, address,
				status, synced_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'synced',now())`,
			id, pid, adoptionID, newShipID, batchNo, reshipNo, reshipSpec, reshipWeight,
			reshipCost, reshipFreight, recipient, phone, address); e != nil {
			writeErr(w, 400, "补发订单同步失败："+e.Error())
			return
		}
		if _, e := tx.Exec(ctx, `
			INSERT INTO compensations(incident_id, adoption_id, project_id, amount, kind, status, note, paid_at)
			VALUES($1,$2,$3,$4,'reship','paid',$5,now())`,
			incID, adoptionID, pid, reshipCost,
			fmt.Sprintf("补发果品 %.2fkg 折值 ¥%.2f（补发单 %s；补发运费 ¥%.2f 单列，由%s承担）",
				reshipWeight, reshipCost, reshipNo, reshipFreight, freightPartyLabel(freightBy))); e != nil {
			writeErr(w, 400, "补发登记失败："+e.Error())
			return
		}
	}

	// 3) 冷链责任同步物流（物流责任，或运费由物流承担时）
	var noticeNo string
	coldSummary, _ := claim["cold_summary"].(string)
	var trackingNo string
	if shipmentID > 0 {
		_ = tx.QueryRow(ctx, `SELECT COALESCE(tracking_no,'') FROM shipments WHERE id=$1`, shipmentID).Scan(&trackingNo)
	}
	if liability == "logistics" || freightBy == "logistics" {
		noticeNo = fmt.Sprintf("LTC-%s-%03d", batchNo, id)
		freightTotal := shippingFee + reshipFreight
		if _, e := tx.Exec(ctx, `
			INSERT INTO logistics_notices(claim_id, project_id, shipment_id, batch_no, tracking_no, notice_no,
				kind, cold_summary, freight_amount, conclusion, status, synced_at)
			VALUES($1,$2,$3,$4,$5,$6,'cold_chain_liability',$7,$8,$9,'synced',now())`,
			id, pid, nullableID(shipmentID), batchNo, trackingNo, noticeNo,
			coldSummary, freightTotal, conclusion); e != nil {
			writeErr(w, 400, "冷链责任同步失败："+e.Error())
			return
		}
	}

	// 4) 同步标记 + 工单关闭 + 流水
	if _, e := tx.Exec(ctx, `
		UPDATE bad_fruit_claims SET archive_synced=TRUE,
			reship_synced=CASE WHEN $2<>'' THEN TRUE ELSE reship_synced END,
			logistics_synced=CASE WHEN $3<>'' THEN TRUE ELSE logistics_synced END
		WHERE id=$1`, id, reshipNo, noticeNo); e != nil {
		writeErr(w, 500, e.Error())
		return
	}
	if _, e := tx.Exec(ctx, `UPDATE incidents SET status='compensated', resolved_at=now() WHERE id=$1`, incID); e != nil {
		writeErr(w, 500, e.Error())
		return
	}
	events := []string{
		fmt.Sprintf("客服关闭坏果赔付判定：%s", conclusion),
		"开箱照片、冷链温度曲线、客服结论已归档保留（赔付关闭前证据保留）",
	}
	if noteID > 0 {
		events = append(events, "赔付结论已同步认养档案")
	}
	if reshipNo != "" {
		reshipTracking = reshipNo
		events = append(events, fmt.Sprintf("补发订单 %s 已同步物流端（补发运费 ¥%.2f 与果品成本 ¥%.2f 分开列示）", reshipNo, reshipFreight, reshipCost))
	}
	if noticeNo != "" {
		events = append(events, fmt.Sprintf("冷链责任通知单 %s 已同步物流端，运费追责 ¥%.2f", noticeNo, shippingFee+reshipFreight))
	}
	for _, ev := range events {
		_, _ = tx.Exec(ctx, `
			INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
			VALUES($1,$2,'service','坏果判定闭环',$3)`, incID, u.ID, ev)
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row, _ := s.queryOne(ctx, `SELECT * FROM bad_fruit_claims WHERE id=$1`, id)
	writeJSON(w, 200, map[string]interface{}{
		"claim": row, "archive_note_id": noteID,
		"reship_no": reshipTracking, "notice_no": noticeNo,
	})
}

// ---------- 补发订单（物流端） ----------

func (s *Server) handleReshipOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)
	rows, err := s.queryRows(r.Context(), `
		SELECT ro.*, p.title AS project_title FROM reship_orders ro
		JOIN projects p ON p.id=ro.project_id ORDER BY ro.id DESC`)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if u.Role == "adopter" {
		filtered := rows[:0]
		for _, x := range rows {
			if aid, _ := x["adoption_id"].(int64); aid > 0 {
				var owner int64
				_ = s.Pool.QueryRow(r.Context(), `SELECT user_id FROM adoptions WHERE id=$1`, aid).Scan(&owner)
				if owner == u.ID {
					filtered = append(filtered, x)
				}
			}
		}
		rows = filtered
	}
	writeJSON(w, 200, rows)
}

// ---------- 冷链责任通知单（物流端确认） ----------

func (s *Server) handleLogisticsNotices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "方法不允许")
		return
	}
	rows, err := s.queryRows(r.Context(), `
		SELECT ln.*, p.title AS project_title FROM logistics_notices ln
		JOIN projects p ON p.id=ln.project_id ORDER BY ln.id DESC`)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) handleLogisticsNoticeByID(w http.ResponseWriter, r *http.Request) {
	id, action, ok := pathParts("/api/logistics-notices/", r.URL.Path)
	if !ok || action != "ack" {
		writeErr(w, 404, "通知单不存在")
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)
	if !requireRole(u, "logistics", "admin") {
		writeErr(w, 403, "只有物流方可以确认责任通知单")
		return
	}
	ctx := r.Context()
	row, err := s.queryOne(ctx, `
		UPDATE logistics_notices SET status='acknowledged', acknowledged_at=now()
		WHERE id=$1 RETURNING *`, id)
	if err != nil {
		writeErr(w, 400, "确认失败："+err.Error())
		return
	}
	var incID int64
	_ = s.Pool.QueryRow(ctx, `SELECT i.id FROM logistics_notices ln
		JOIN bad_fruit_claims c ON c.id=ln.claim_id JOIN incidents i ON i.id=c.incident_id
		WHERE ln.id=$1`, id).Scan(&incID)
	if incID > 0 {
		_, _ = s.Pool.Exec(ctx, `
			INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
			VALUES($1,$2,'logistics','确认冷链责任',$3)`, incID, u.ID,
			fmt.Sprintf("物流端已确认通知单，运费追责 ¥%.2f", row["freight_amount"]))
	}
	writeJSON(w, 200, row)
}

// ---------- 规则与文案 ----------

// suggestPlan 客服按坏果比例生成方案：
// 冷链脱冷 → 物流追责；≥10% → 退款；5%-10% → 补发；<5% → 补发协商
func suggestPlan(badPct float64, coldBreach bool) string {
	switch {
	case coldBreach:
		return "logistics_liability"
	case badPct >= 10:
		return "refund"
	default:
		return "reship"
	}
}

func planLabel(p string) string {
	return map[string]string{
		"reship":              "补发",
		"refund":              "退款",
		"logistics_liability": "物流追责",
	}[p]
}

func freightPartyLabel(p string) string {
	return map[string]string{"farm": "农场", "logistics": "物流", "shared": "农场与物流共同", "": "—"}[p]
}
