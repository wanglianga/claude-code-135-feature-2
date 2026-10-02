package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v4"
)

// 坏果赔付判定方案
const (
	schemeReship         = "reship"          // 补发
	schemeRefund         = "refund"          // 退款
	schemeLogisticsClaim = "logistics_claim" // 物流追责
)

var schemeLabel = map[string]string{
	schemeReship:         "补发",
	schemeRefund:         "退款",
	schemeLogisticsClaim: "物流追责",
}

// recommendClaimScheme 按坏果比例与冷链是否脱冷给出建议方案：
// 冷链脱冷 → 物流追责；坏果率≥30% → 退款；其余（>0）→ 补发。
func recommendClaimScheme(badRate float64, coldBreached bool) string {
	switch {
	case coldBreached:
		return schemeLogisticsClaim
	case badRate >= 30:
		return schemeRefund
	case badRate > 0:
		return schemeReship
	default:
		return ""
	}
}

// asPhotoList 兼容 [{caption,url}] / ["url"] 两种照片入参
func asPhotoList(v interface{}) []interface{} {
	out := []interface{}{}
	arr, ok := v.([]interface{})
	if !ok {
		return out
	}
	for _, x := range arr {
		switch t := x.(type) {
		case string:
			if t != "" {
				out = append(out, map[string]interface{}{"caption": "开箱照片", "url": t})
			}
		case map[string]interface{}:
			out = append(out, t)
		}
	}
	return out
}

// handleBadFruitClaims 坏果赔付判定单
// POST  认养人反馈坏果（自动建工单并快照采摘/分拣/冷链/签收证据）
// GET   列表（认养人看本人；物流看冷链责任单；其余按农场/平台范围）
func (s *Server) handleBadFruitClaims(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	ctx := r.Context()
	if r.Method == http.MethodGet {
		q := `SELECT c.*, p.title AS project_title,
		             a.recipient_name,
		             sh.tracking_no AS reship_tracking_no, sh.status AS reship_status
		      FROM bad_fruit_claims c
		      JOIN projects p ON p.id=c.project_id
		      LEFT JOIN adoptions a ON a.id=c.adoption_id
		      LEFT JOIN shipments sh ON sh.id=c.reship_shipment_id`
		args := []interface{}{}
		conds := []string{}
		if u.Role == "adopter" {
			conds = append(conds, fmt.Sprintf("c.adoption_id IN (SELECT id FROM adoptions WHERE user_id=$%d)", len(args)+1))
			args = append(args, u.ID)
		} else if u.Role == "logistics" {
			// 仅同步给物流：判定为冷链责任的赔付单
			conds = append(conds, "c.cold_chain_responsible=TRUE")
		} else if u.FarmID != nil && u.Role != "admin" && u.Role != "service" {
			conds = append(conds, fmt.Sprintf("p.farm_id=$%d", len(args)+1))
			args = append(args, *u.FarmID)
		}
		if v := r.URL.Query().Get("project_id"); v != "" {
			conds = append(conds, fmt.Sprintf("c.project_id=$%d", len(args)+1))
			args = append(args, v)
		}
		if v := r.URL.Query().Get("status"); v != "" {
			conds = append(conds, fmt.Sprintf("c.status=$%d", len(args)+1))
			args = append(args, v)
		}
		if v := r.URL.Query().Get("incident_id"); v != "" {
			conds = append(conds, fmt.Sprintf("c.incident_id=$%d", len(args)+1))
			args = append(args, v)
		}
		if len(conds) > 0 {
			q += " WHERE " + joinAnd(conds)
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

	var in map[string]interface{}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	adoptionID, _ := i64(in, "adoption_id")
	if adoptionID == 0 {
		writeErr(w, 400, "adoption_id 必填（反馈哪一笔认养订单）")
		return
	}
	total, _ := i64(in, "total_count")
	bad, _ := i64(in, "bad_count")
	if total <= 0 || bad <= 0 || bad > total {
		writeErr(w, 400, "签收总数与坏果数必填，且 0 < 坏果数 ≤ 总数")
		return
	}

	// 订单与项目校验
	var pid int64
	var batchNo, ownerName, title string
	var ownerID int64
	err := s.Pool.QueryRow(ctx, `
		SELECT a.project_id, a.batch_no, a.user_id, a.recipient_name, p.title
		FROM adoptions a JOIN projects p ON p.id=a.project_id WHERE a.id=$1`,
		adoptionID).Scan(&pid, &batchNo, &ownerID, &ownerName, &title)
	if err != nil {
		writeErr(w, 404, "认养订单不存在")
		return
	}
	if u.Role == "adopter" && u.ID != ownerID {
		writeErr(w, 403, "只能为本人订单反馈坏果")
		return
	}

	// 关联运单：显式指定或取该单最近一次已签收运单
	shipmentID, _ := i64(in, "shipment_id")
	if shipmentID == 0 {
		_ = s.Pool.QueryRow(ctx, `
			SELECT id FROM shipments WHERE adoption_id=$1 AND status='delivered'
			ORDER BY delivered_at DESC NULLS LAST, id DESC LIMIT 1`, adoptionID).Scan(&shipmentID)
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)

	// 同笔运单只能建一次坏果赔付
	var dup int
	if shipmentID != 0 {
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM bad_fruit_claims WHERE shipment_id=$1`, shipmentID).Scan(&dup)
	}
	if dup > 0 {
		writeErr(w, 409, "该运单已发起过坏果赔付")
		return
	}

	desc := asString(in["description"])
	if desc == "" {
		desc = fmt.Sprintf("%s 反馈签收 %d 件中 %d 件坏果（坏果比例 %.1f%%）", ownerName, total, bad, float64(bad)/float64(total)*100)
	}
	var iid int64
	err = tx.QueryRow(ctx, `
		INSERT INTO incidents(project_id, batch_no, adoption_id, kind, title, description, reporter_id, status)
		VALUES($1,$2,$3,'bad_fruit',$4,$5,$6,'open') RETURNING id`,
		pid, batchNo, nullableID(adoptionID),
		fmt.Sprintf("坏果较多反馈 · %s", title), desc, u.ID).Scan(&iid)
	if err != nil {
		writeErr(w, 400, "创建工单失败："+err.Error())
		return
	}
	photos := asPhotoList(in["open_box_photos"])
	_, _ = tx.Exec(ctx, `
		INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
		VALUES($1,$2,'adopter','开箱反馈坏果',$3)`,
		iid, u.ID, fmt.Sprintf("开箱照片 %d 张；%s", len(photos), desc))

	claimID, err := s.insertClaimEvidence(ctx, tx, in, iid, adoptionID, pid, batchNo, shipmentID, total, bad, photos, u.ID)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row, _ := s.queryOne(r.Context(), `SELECT * FROM bad_fruit_claims WHERE id=$1`, claimID)
	writeJSON(w, 201, map[string]interface{}{
		"claim":       row,
		"incident_id": iid,
		"message":     "坏果反馈已提交，平台已关联采摘时间、分拣照片、冷链温度、签收时间与开箱照片",
	})
}

// curveSummary 从冷链温度点 [{t,temp}, ...] 计算极值
func curveSummary(curve []interface{}) (lo, hi float64, n int) {
	lo, hi = 0, 0
	for _, p := range curve {
		m, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		temp, ok := numVal(m["temp"])
		if !ok {
			continue
		}
		if n == 0 {
			lo, hi = temp, temp
		} else {
			lo, hi = minF(lo, temp), maxF(hi, temp)
		}
		n++
	}
	return lo, hi, n
}

// insertClaimEvidence 快照采摘时间/分拣照片/签收时间/冷链曲线（赔付关闭后仍保留）
func (s *Server) insertClaimEvidence(
	ctx context.Context, tx pgx.Tx, in map[string]interface{},
	iid, adoptionID, pid int64, batchNo string, shipmentID int64,
	total, bad int64, photos []interface{}, reporterID int64,
) (int64, error) {
	badRate := roundF(float64(bad)/float64(total)*100, 2)

	// 运单证据：冷链路线/运单号/签收时间/冷链曲线
	var (
		coldRoute, compName, trackingNo string
		signedAt                        interface{}
		curve                           []interface{}
		tempMin, tempMax                *float64
		coldBreached                    bool
	)
	if shipmentID != 0 {
		var curveRaw []byte
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(cold_route,''), COALESCE(logistics_company,''), COALESCE(tracking_no,''),
			       delivered_at, cold_chain_curve, cold_temp_min, cold_temp_max, cold_breached
			FROM shipments WHERE id=$1`, shipmentID).
			Scan(&coldRoute, &compName, &trackingNo, &signedAt, &curveRaw, &tempMin, &tempMax, &coldBreached)
		if err != nil {
			return 0, fmt.Errorf("关联运单不存在：%w", err)
		}
		if len(curveRaw) > 0 {
			_ = json.Unmarshal(curveRaw, &curve)
		}
	}

	// 认养人/客服提交时可补录冷链曲线（运单尚未登记的场景）
	if cv, ok := in["cold_chain_curve"].([]interface{}); ok && len(cv) > 0 {
		curve = cv
	}
	if lo, hi, n := curveSummary(curve); n > 0 {
		tempMin, tempMax = &lo, &hi
		if _, set := in["cold_breached"].(bool); !set && hi > 6 {
			coldBreached = true // 冷链标准 0-6℃，最高温超 6℃ 判脱冷
		}
	}
	if v, ok := in["cold_breached"].(bool); ok {
		coldBreached = v
	}

	// 采摘时间：本批次最近一次已完成采摘（可由入参覆盖）
	var harvestAt interface{}
	if v, ok := str(in, "harvest_at"); ok {
		harvestAt = v
	} else {
		_ = tx.QueryRow(ctx, `
			SELECT planned_date::timestamptz FROM harvest_tasks
			WHERE project_id=$1 AND status='done'
			ORDER BY planned_date DESC, id DESC LIMIT 1`, pid).Scan(&harvestAt)
	}

	// 分拣照片：汇总本批次分拣记录照片（可由入参覆盖）
	sortingPhotos := []interface{}{}
	if sp, ok := in["sorting_photos"].([]interface{}); ok && len(sp) > 0 {
		sortingPhotos = asPhotoList(sp)
	} else {
		var raw []byte
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(jsonb_agg(e), '[]'::jsonb) FROM (
				SELECT jsonb_array_elements(photos) AS e FROM sorting_records
				WHERE project_id=$1 AND jsonb_array_length(photos) > 0) t`, pid).Scan(&raw); err == nil {
			_ = json.Unmarshal(raw, &sortingPhotos)
		}
	}
	if v, ok := str(in, "signed_at"); ok && signedAt == nil {
		signedAt = v
	}

	recommended := recommendClaimScheme(badRate, coldBreached)
	var claimID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO bad_fruit_claims(
			incident_id, adoption_id, project_id, shipment_id, batch_no,
			total_count, bad_count, bad_rate_pct, open_box_photos,
			harvest_at, sorting_photos, signed_at, cold_route, logistics_company, tracking_no,
			cold_temp_min, cold_temp_max, cold_breached, cold_chain_curve, recommended_scheme, reporter_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		RETURNING id`,
		iid, nullableID(adoptionID), pid, nullableID(shipmentID), batchNo,
		total, bad, badRate, photosJSON(photos),
		harvestAt, photosJSON(sortingPhotos), signedAt, coldRoute, compName, trackingNo,
		tempMin, tempMax, coldBreached, photosJSON(curve), recommended, reporterID).Scan(&claimID)
	if err != nil {
		return 0, fmt.Errorf("赔付单创建失败：%w", err)
	}
	return claimID, nil
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// photosJSON 保证照片/曲线字段以合法 JSON 落库（JSONB 以文本发送）
func photosJSON(v []interface{}) string {
	if v == nil {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// handleBadFruitClaimByID 判定单详情 / 客服判定 / 关闭
func (s *Server) handleBadFruitClaimByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/claims/bad-fruit/"), "/")
	parts := strings.Split(rest, "/")
	id, err := parseID(parts[0])
	if err != nil {
		writeErr(w, 404, "赔付单不存在")
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if action != "" && action != "judge" && action != "close" {
		writeErr(w, 404, "未知操作")
		return
	}
	ctx := r.Context()

	if r.Method == http.MethodGet && action == "" {
		u := s.currentUser(r)
		claim, err := s.queryOne(ctx, `
			SELECT c.*, a.user_id AS owner_id
			FROM bad_fruit_claims c LEFT JOIN adoptions a ON a.id=c.adoption_id
			WHERE c.id=$1`, id)
		if err != nil {
			writeErr(w, 404, "赔付单不存在")
			return
		}
		if u.Role == "adopter" {
			if owner, ok := asID(claim["owner_id"]); !ok || owner != u.ID {
				writeErr(w, 403, "只能查看本人订单的坏果赔付")
				return
			}
		}
		delete(claim, "owner_id")
		inc, _ := s.queryOne(ctx, `
			SELECT i.*, p.title AS project_title, u.name AS reporter_name
			FROM incidents i JOIN projects p ON p.id=i.project_id
			LEFT JOIN users u ON u.id=i.reporter_id WHERE i.id=$1`, claim["incident_id"])
		events, _ := s.queryRows(ctx, `
			SELECT e.*, u.name AS actor_name FROM incident_events e
			LEFT JOIN users u ON u.id=e.actor_id
			WHERE e.incident_id=$1 ORDER BY e.created_at`, claim["incident_id"])
		var reship interface{}
		if rid, ok := asID(claim["reship_shipment_id"]); ok {
			reship, _ = s.queryOne(ctx, `SELECT * FROM shipments WHERE id=$1`, rid)
		}
		writeJSON(w, 200, map[string]interface{}{
			"claim": claim, "incident": inc, "events": events, "reship_shipment": reship,
		})
		return
	}

	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)

	if action == "judge" {
		s.judgeBadFruitClaim(w, r, id, u)
		return
	}
	s.closeBadFruitClaim(w, r, id, u)
}

// judgeBadFruitClaim 客服根据坏果比例生成补发/退款/物流追责方案
func (s *Server) judgeBadFruitClaim(w http.ResponseWriter, r *http.Request, id int64, u *User) {
	if !requireRole(u, "service", "admin", "farmer") {
		writeErr(w, 403, "只有客服/平台/农场可以出具赔付判定")
		return
	}
	var in map[string]interface{}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	scheme, _ := str(in, "scheme")
	if !map[string]bool{schemeReship: true, schemeRefund: true, schemeLogisticsClaim: true}[scheme] {
		writeErr(w, 400, "方案只能是 reship(补发)/refund(退款)/logistics_claim(物流追责)")
		return
	}
	refund, _ := f64(in, "refund_amount")
	reshipCost, _ := f64(in, "reship_cost")
	freight, _ := f64(in, "freight_cost")
	if refund < 0 || reshipCost < 0 || freight < 0 {
		writeErr(w, 400, "金额不能为负")
		return
	}
	coldResp := false
	if v, ok := in["cold_chain_responsible"]; v != nil && ok {
		coldResp, _ = v.(bool)
	}
	conclusion, _ := str(in, "conclusion")
	if conclusion == "" {
		writeErr(w, 400, "conclusion 必填（客服结论）")
		return
	}
	ctx := r.Context()

	var iid, adoptionID, pid int64
	var badRate float64
	var breached bool
	var curStatus string
	err := s.Pool.QueryRow(ctx, `
		SELECT incident_id, COALESCE(adoption_id,0), project_id, bad_rate_pct, cold_breached, status
		FROM bad_fruit_claims WHERE id=$1`, id).
		Scan(&iid, &adoptionID, &pid, &badRate, &breached, &curStatus)
	if err != nil {
		writeErr(w, 404, "赔付单不存在")
		return
	}
	if curStatus == "closed" {
		writeErr(w, 409, "赔付已关闭，不能修改判定")
		return
	}
	// 物流追责方案必须建立在冷链责任判定上
	if scheme == schemeLogisticsClaim && !coldResp {
		coldResp = breached // 证据已判定脱冷时默认勾上
	}

	row, err := s.queryOne(ctx, `
		UPDATE bad_fruit_claims
		SET scheme=$1, refund_amount=$2, reship_cost=$3, freight_cost=$4,
		    cold_chain_responsible=$5, conclusion=$6, handler_id=$7,
		    status='judged', judged_at=now()
		WHERE id=$8 RETURNING *`,
		scheme, refund, reshipCost, freight, coldResp, conclusion, u.ID, id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE incidents SET status='processing' WHERE id=$1`, iid)
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
		VALUES($1,$2,'service','客服出具坏果赔付判定',$3)`,
		iid, u.ID, fmt.Sprintf(
			"坏果比例 %.1f%%，方案：%s；退款 ¥%.2f、补发成本 ¥%.2f、运费 ¥%.2f（运费与补发成本分开计算）。冷链责任=%v。%s",
			badRate, schemeLabel[scheme], refund, reshipCost, freight, coldResp, conclusion))

	writeJSON(w, 200, map[string]interface{}{
		"claim":              row,
		"recommended_scheme": recommendClaimScheme(badRate, breached),
		"message":            "判定已记录，待关闭后执行补发/退款并同步物流与认养档案",
	})
}

// closeBadFruitClaim 关闭赔付：执行退款/补发，补发订单同步物流端，冷链责任同步物流，结论同步认养档案
func (s *Server) closeBadFruitClaim(w http.ResponseWriter, r *http.Request, id int64, u *User) {
	if !requireRole(u, "service", "admin") {
		writeErr(w, 403, "只有客服可以关闭坏果赔付")
		return
	}
	ctx := r.Context()
	var (
		iid, adoptionID, pid                             int64
		scheme, conclusion, batchNo, coldRoute, compName string
		refund, reshipCost, freight                      float64
		coldResp                                         bool
		curStatus                                        string
	)
	err := s.Pool.QueryRow(ctx, `
		SELECT incident_id, COALESCE(adoption_id,0), project_id, COALESCE(scheme,''),
		       refund_amount, reship_cost, freight_cost, cold_chain_responsible,
		       conclusion, status, batch_no, cold_route
		FROM bad_fruit_claims WHERE id=$1`, id).
		Scan(&iid, &adoptionID, &pid, &scheme, &refund, &reshipCost, &freight,
			&coldResp, &conclusion, &curStatus, &batchNo, &coldRoute)
	if err != nil {
		writeErr(w, 404, "赔付单不存在")
		return
	}
	if curStatus != "judged" {
		writeErr(w, 409, "需先完成客服判定（judge）才能关闭赔付")
		return
	}
	_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(recipient_name,'') FROM adoptions WHERE id=$1`,
		adoptionID).Scan(&compName)

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)

	// 1) 退款 / 补发 → 赔付记录（结论同步认养档案 /api/my/adpositions）
	switch scheme {
	case schemeRefund:
		if refund > 0 {
			_, _ = tx.Exec(ctx, `
				INSERT INTO compensations(incident_id, adoption_id, project_id, amount, kind, note, status, paid_at)
				VALUES($1,$2,$3,$4,'cash',$5,'paid',now())`,
				iid, nullableID(adoptionID), pid, refund,
				fmt.Sprintf("坏果退款 ¥%.2f（客服结论：%s）", refund, conclusion))
		}
	case schemeReship, schemeLogisticsClaim:
		if reshipCost > 0 {
			_, _ = tx.Exec(ctx, `
				INSERT INTO compensations(incident_id, adoption_id, project_id, amount, kind, note, status, paid_at)
				VALUES($1,$2,$3,$4,'reship',$5,'paid',now())`,
				iid, nullableID(adoptionID), pid, reshipCost,
				fmt.Sprintf("坏果补发果品成本 ¥%.2f（与运费分开核算）。%s", reshipCost, conclusion))
		}
		// 2) 补发订单同步物流端：生成补发货运单（is_reship，进入物流待发货列表）
		var maxSeq int64
		_ = tx.QueryRow(ctx, `SELECT COALESCE(MAX(seq),0) FROM shipments WHERE project_id=$1 AND adoption_id=$2`,
			pid, nullableID(adoptionID)).Scan(&maxSeq)
		var reshipID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO shipments(project_id, adoption_id, batch_no, seq, plan_date, pack_spec,
			                      cold_route, status, is_reship, claim_id, operator_id)
			VALUES($1,$2,$3,$4,CURRENT_DATE,$5,$6,'planned',TRUE,$7,$8) RETURNING id`,
			pid, nullableID(adoptionID), batchNo, maxSeq+1,
			fmt.Sprintf("坏果补发（赔付单 #%d，补发成本 ¥%.2f，运费 ¥%.2f 分开核算）", id, reshipCost, freight),
			coldRoute, id, u.ID).Scan(&reshipID)
		if err == nil {
			_, _ = tx.Exec(ctx, `UPDATE bad_fruit_claims SET reship_shipment_id=$1 WHERE id=$2`, reshipID, id)
			_, _ = tx.Exec(ctx, `
				INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
				VALUES($1,$2,'service','补发订单已同步物流端',$3)`,
				iid, u.ID, fmt.Sprintf("补发货运单 #%d 已推送到物流待发货（第 %d 批），运费 ¥%.2f 单独核算",
					reshipID, maxSeq+1, freight))
		}
	}

	// 3) 运费与补发成本分开入账：运费列物流成本（物流追责时向物流追偿）
	var plotID int64
	_ = tx.QueryRow(ctx, `SELECT plot_id FROM projects WHERE id=$1`, pid).Scan(&plotID)
	if freight > 0 {
		_, _ = tx.Exec(ctx, `
			INSERT INTO plot_costs(plot_id, project_id, season, cost_type, item, amount)
			VALUES($1,$2,'2026','logistics',$3,$4)`,
			plotID, pid,
			fmt.Sprintf("坏果补发运费（赔付单 #%d，向物流追偿）", id), freight)
	}
	if reshipCost > 0 {
		_, _ = tx.Exec(ctx, `
			INSERT INTO plot_costs(plot_id, project_id, season, cost_type, item, amount)
			VALUES($1,$2,'2026','compensation',$3,$4)`,
			plotID, pid,
			fmt.Sprintf("坏果补发果品成本（赔付单 #%d）", id), reshipCost)
	}

	// 4) 冷链责任同步物流
	if coldResp {
		_, _ = tx.Exec(ctx, `UPDATE bad_fruit_claims SET cold_synced_at=now() WHERE id=$1`, id)
		_, _ = tx.Exec(ctx, `
			INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
			VALUES($1,$2,'service','冷链责任已同步物流',$3)`,
			iid, u.ID, fmt.Sprintf(
				"冷链脱冷导致坏果，责任判定同步物流端，运费 ¥%.2f 由物流承担/追偿，补发成本 ¥%.2f 分开核算",
				freight, reshipCost))
	}

	// 5) 赔付结论同步认养档案 + 关闭（开箱照片、冷链曲线、客服结论保留在赔付单内）
	_, _ = tx.Exec(ctx, `
		UPDATE bad_fruit_claims SET status='closed', closed_at=now() WHERE id=$1`, id)
	_, _ = tx.Exec(ctx, `UPDATE incidents SET status='compensated', resolved_at=now() WHERE id=$1`, iid)
	_, _ = tx.Exec(ctx, `
		INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
		VALUES($1,$2,'service','坏果赔付关闭，结论同步认养档案',$3)`,
		iid, u.ID, fmt.Sprintf("方案：%s；证据（开箱照片/冷链曲线/分拣照片）保留备查。%s",
			schemeLabel[scheme], conclusion))

	if err := tx.Commit(ctx); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	row, _ := s.queryOne(ctx, `SELECT * FROM bad_fruit_claims WHERE id=$1`, id)
	writeJSON(w, 200, map[string]interface{}{"claim": row, "message": "坏果赔付已关闭并同步物流、认养档案"})
}

// handleShipmentByID 运单子资源：冷链温度曲线登记（物流端）
// POST /api/shipments/{id}/cold-chain
func (s *Server) handleShipmentByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/shipments/"), "/")
	parts := strings.Split(rest, "/")
	sid, err := parseID(parts[0])
	if err != nil || len(parts) != 2 || parts[1] != "cold-chain" {
		writeErr(w, 404, "未知运单资源")
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)
	if !requireRole(u, "logistics", "admin", "farmer") {
		writeErr(w, 403, "只有物流方可以登记冷链温度")
		return
	}
	var in struct {
		Curve        []interface{} `json:"cold_chain_curve"`
		ColdBreached *bool         `json:"cold_breached"`
	}
	if err := decode(r, &in); err != nil || len(in.Curve) == 0 {
		writeErr(w, 400, "cold_chain_curve 必填（温度点 [{t, temp}, ...]）")
		return
	}
	lo, hi, n := curveSummary(in.Curve)
	if n == 0 {
		writeErr(w, 400, "温度点需包含数值字段 temp")
		return
	}
	breached := hi > 6 // 生鲜冷链标准 0-6℃
	if in.ColdBreached != nil {
		breached = *in.ColdBreached
	}
	b := photosJSON(in.Curve)
	ctx := r.Context()
	row, err := s.queryOne(ctx, `
		UPDATE shipments
		SET cold_chain_curve=$1, cold_temp_min=$2, cold_temp_max=$3,
		    cold_breached=$4, cold_logged_by=$5, cold_logged_at=now()
		WHERE id=$6 RETURNING *`, b, lo, hi, breached, u.ID, sid)
	if err != nil {
		writeErr(w, 400, "登记失败："+err.Error())
		return
	}
	note := fmt.Sprintf("冷链温度曲线已同步：最低 %.1f℃ / 最高 %.1f℃，%s", lo, hi,
		map[bool]string{true: "存在脱冷（超 6℃）", false: "全程 0-6℃ 达标"}[breached])
	// 若该运单已关联坏果赔付单，在工单流水中留痕
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
		SELECT c.incident_id, $1, 'logistics', '补充冷链温度曲线', $2
		FROM bad_fruit_claims c WHERE c.shipment_id=$3`, u.ID, note, sid)
	writeJSON(w, 200, row)
}

// handleShipments 补发运单列表（补发订单同步物流端后，物流在此看待发货任务）
// GET /api/shipments?reship=1
func (s *Server) handleShipments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)
	q := `SELECT sh.*, p.title AS project_title, p.farm_id,
	             a.recipient_name, a.address
	      FROM shipments sh
	      JOIN projects p ON p.id=sh.project_id
	      LEFT JOIN adoptions a ON a.id=sh.adoption_id`
	args := []interface{}{}
	conds := []string{}
	if u.Role == "logistics" {
		// 物流可见：补发运单（补发订单同步物流端）与坏果赔付关联的原始运单
		conds = append(conds, "(sh.is_reship=TRUE OR sh.claim_id IS NOT NULL "+
			"OR sh.id IN (SELECT shipment_id FROM bad_fruit_claims WHERE shipment_id IS NOT NULL))")
	} else {
		if r.URL.Query().Get("reship") != "0" {
			conds = append(conds, "sh.is_reship=TRUE")
		}
		if u.FarmID != nil && u.Role != "admin" && u.Role != "service" {
			conds = append(conds, fmt.Sprintf("p.farm_id=$%d", len(args)+1))
			args = append(args, *u.FarmID)
		}
	}
	if len(conds) > 0 {
		q += " WHERE " + joinAnd(conds)
	}
	q += " ORDER BY sh.id DESC"
	rows, err := s.queryRows(r.Context(), q, args...)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}
