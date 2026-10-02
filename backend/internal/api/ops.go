package api

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) projectSubresource(w http.ResponseWriter, r *http.Request, id int64, parts []string) {
	action := parts[0]
	u := s.currentUser(r)
	ctx := r.Context()

	// 校验项目存在并取 farm_id
	var farmID int64
	var batchNo string
	err := s.Pool.QueryRow(ctx, `SELECT farm_id, batch_no FROM projects WHERE id=$1`, id).Scan(&farmID, &batchNo)
	if err != nil {
		writeErr(w, 404, "认养项目不存在")
		return
	}
	ownFarm := u.Role == "admin" || (u.FarmID != nil && *u.FarmID == farmID)

	// 两段子路径：harvests/{hid}/complete、shipments/{sid}/advance
	if len(parts) == 3 {
		subID, err1 := parseID(parts[1])
		if err1 != nil {
			writeErr(w, 404, "资源不存在")
			return
		}
		switch {
		case parts[0] == "harvests" && parts[2] == "complete":
			s.completeHarvest(w, r, id, subID, ownFarm)
		case parts[0] == "shipments" && parts[2] == "advance":
			s.advanceShipment(w, r, id, subID, ownFarm)
		default:
			writeErr(w, 404, "未知操作")
		}
		return
	}
	if len(parts) != 1 {
		writeErr(w, 404, "未知资源")
		return
	}

	switch action {
	case "logs":
		// POST 新增农事日志 / 灾害记录（农场主、本农场采摘/包装人员、管理员）
		if r.Method != http.MethodPost {
			writeErr(w, 405, "方法不允许")
			return
		}
		if !ownFarm && u.Role != "service" {
			writeErr(w, 403, "只能为本农场项目记录农事")
			return
		}
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		logType, _ := str(in, "log_type")
		if logType == "" {
			logType = "farming"
		}
		title, _ := str(in, "title")
		if title == "" {
			writeErr(w, 400, "标题必填")
			return
		}
		impact, _ := f64(in, "yield_impact_pct")
		photos := in["photos"]
		if photos == nil {
			photos = []interface{}{}
		}
		row, err := s.insertRow(ctx, "farm_logs", map[string]interface{}{
			"project_id":       id,
			"batch_no":         batchNo,
			"author_id":        u.ID,
			"log_type":         logType,
			"title":            title,
			"content":          in["content"],
			"photos":           photos,
			"disaster_kind":    in["disaster_kind"],
			"yield_impact_pct": impact,
		}, "photos")
		if err != nil {
			writeErr(w, 400, "记录失败："+err.Error())
			return
		}
		// 灾害自动建工单，保证“灾害→处置”进入同一批次
		if logType == "disaster" {
			dk, _ := str(in, "disaster_kind")
			kind := "other"
			switch {
			case strings.Contains(dk, "雹"):
				kind = "hail"
			case strings.Contains(dk, "雨") || strings.Contains(dk, "寒") || strings.Contains(dk, "旱"):
				kind = "delay"
			}
			if _, e := s.Pool.Exec(ctx,
				`INSERT INTO incidents(project_id, batch_no, kind, title, description, reporter_id)
				 VALUES($1,$2,$3,$4,$5,$6)`,
				id, batchNo, kind, title, asString(in["content"]), u.ID); e == nil {
				// 关联事件在 incident 详情中补记
			}
		}
		writeJSON(w, 201, row)

	case "checks":
		if r.Method != http.MethodPost {
			writeErr(w, 405, "方法不允许")
			return
		}
		if !ownFarm && u.Role != "admin" {
			writeErr(w, 403, "只有农场方可以登记成熟度抽检")
			return
		}
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		samples, _ := i64(in, "sample_count")
		maturity, _ := f64(in, "maturity_pct")
		estYield, _ := f64(in, "est_yield_kg")
		badRate, _ := f64(in, "bad_rate_pct")
		grade := in["grade_dist"]
		if grade == nil {
			grade = map[string]interface{}{}
		}
		row, err := s.insertRow(ctx, "maturity_checks", map[string]interface{}{
			"project_id":   id,
			"batch_no":     batchNo,
			"inspector_id": u.ID,
			"brix":         in["brix"],
			"sample_count": samples,
			"maturity_pct": maturity,
			"est_yield_kg": estYield,
			"grade_dist":   grade,
			"bad_rate_pct": badRate,
			"notes":        in["notes"],
		}, "grade_dist")
		if err != nil {
			writeErr(w, 400, "登记失败："+err.Error())
			return
		}
		writeJSON(w, 201, row)

	case "harvests":
		if r.Method != http.MethodPost {
			writeErr(w, 405, "方法不允许")
			return
		}
		if !ownFarm && u.Role != "admin" {
			writeErr(w, 403, "只有农场方可以安排采摘")
			return
		}
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		pickerID, _ := i64(in, "picker_id")
		priority, _ := i64(in, "priority")
		if priority == 0 {
			priority = 100
		}
		target, _ := f64(in, "target_kg")
		date, _ := str(in, "planned_date")
		if date == "" {
			writeErr(w, 400, "planned_date 必填（YYYY-MM-DD）")
			return
		}
		var plotID int64
		_ = s.Pool.QueryRow(ctx, `SELECT plot_id FROM projects WHERE id=$1`, id).Scan(&plotID)
		row, err := s.insertRow(ctx, "harvest_tasks", map[string]interface{}{
			"project_id":   id,
			"batch_no":     batchNo,
			"plot_id":      plotID,
			"picker_id":    nullableID(pickerID),
			"priority":     priority,
			"planned_date": date,
			"target_kg":    target,
		})
		if err != nil {
			writeErr(w, 400, "安排失败："+err.Error())
			return
		}
		writeJSON(w, 201, row)

	case "shipments":
		if r.Method != http.MethodPost {
			writeErr(w, 405, "方法不允许")
			return
		}
		if !ownFarm && u.Role != "admin" {
			writeErr(w, 403, "只有农场方可以编排发货计划")
			return
		}
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		adoptionID, _ := i64(in, "adoption_id")
		seq, _ := i64(in, "seq")
		if seq == 0 {
			seq = 1
		}
		weight, _ := f64(in, "weight_kg")
		row, err := s.insertRow(ctx, "shipments", map[string]interface{}{
			"project_id":  id,
			"adoption_id": nullableID(adoptionID),
			"batch_no":    batchNo,
			"seq":         seq,
			"plan_date":   in["plan_date"],
			"pack_spec":   in["pack_spec"],
			"cold_route":  in["cold_route"],
			"weight_kg":   nullableF64(weight),
		})
		if err != nil {
			writeErr(w, 400, "编排失败："+err.Error())
			return
		}
		writeJSON(w, 201, row)

	case "sorting":
		if r.Method != http.MethodPost {
			writeErr(w, 405, "方法不允许")
			return
		}
		if !ownFarm && u.Role != "admin" {
			writeErr(w, 403, "只有农场方可以登记分拣")
			return
		}
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		spec, _ := str(in, "spec")
		weight, _ := f64(in, "weight_kg")
		boxCount, _ := i64(in, "box_count")
		if spec == "" {
			writeErr(w, 400, "包装规格必填")
			return
		}
		photos := in["photos"]
		if photos == nil {
			photos = []interface{}{}
		}
		row, err := s.insertRow(ctx, "sorting_records", map[string]interface{}{
			"project_id": id,
			"batch_no":   batchNo,
			"packer_id":  u.ID,
			"spec":       spec,
			"weight_kg":  weight,
			"box_count":  boxCount,
			"photos":     photos,
		}, "photos")
		if err != nil {
			writeErr(w, 400, "登记失败："+err.Error())
			return
		}
		writeJSON(w, 201, row)

	case "costs":
		if r.Method != http.MethodPost {
			writeErr(w, 405, "方法不允许")
			return
		}
		if !ownFarm && u.Role != "admin" {
			writeErr(w, 403, "只有农场主可以记账")
			return
		}
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		costType, _ := str(in, "cost_type")
		item, _ := str(in, "item")
		amount, hasAmount := f64(in, "amount")
		if costType == "" || item == "" || !hasAmount {
			writeErr(w, 400, "cost_type/item/amount 必填")
			return
		}
		var plotID int64
		_ = s.Pool.QueryRow(ctx, `SELECT plot_id FROM projects WHERE id=$1`, id).Scan(&plotID)
		row, err := s.insertRow(ctx, "plot_costs", map[string]interface{}{
			"plot_id":    plotID,
			"project_id": id,
			"season":     "2026",
			"cost_type":  costType,
			"item":       item,
			"amount":     amount,
		})
		if err != nil {
			writeErr(w, 400, "记账失败："+err.Error())
			return
		}
		writeJSON(w, 201, row)

	case "live":
		if r.Method != http.MethodPost {
			writeErr(w, 405, "方法不允许")
			return
		}
		if !ownFarm {
			writeErr(w, 403, "只有农场方可以开播")
			return
		}
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		title, _ := str(in, "title")
		startsAt, _ := str(in, "starts_at")
		if title == "" || startsAt == "" {
			writeErr(w, 400, "title/starts_at 必填")
			return
		}
		row, err := s.insertRow(ctx, "live_streams", map[string]interface{}{
			"project_id": id,
			"farm_id":    farmID,
			"title":      title,
			"stream_url": in["stream_url"],
			"starts_at":  startsAt,
		})
		if err != nil {
			writeErr(w, 400, "创建直播失败："+err.Error())
			return
		}
		writeJSON(w, 201, row)

	case "settle":
		s.handleSettle(w, r, id, farmID, batchNo, ownFarm)

	default:
		writeErr(w, 404, "未知资源: "+action)
	}
}

func (s *Server) completeHarvest(w http.ResponseWriter, r *http.Request, projectID, taskID int64, ownFarm bool) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)
	var pickerID int64
	_ = s.Pool.QueryRow(r.Context(), `SELECT COALESCE(picker_id,0) FROM harvest_tasks WHERE id=$1 AND project_id=$2`,
		taskID, projectID).Scan(&pickerID)
	if !ownFarm && !(u.Role == "picker" && pickerID == u.ID) {
		writeErr(w, 403, "只有负责该任务的采摘工或农场方可回填")
		return
	}
	var in map[string]interface{}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	actual, _ := f64(in, "actual_kg")
	badRate, _ := f64(in, "bad_rate_pct")
	row, err := s.queryOne(r.Context(),
		`UPDATE harvest_tasks SET actual_kg=$1, bad_rate_pct=$2, status='done', picker_id=COALESCE(picker_id,$3)
		 WHERE id=$4 RETURNING *`,
		actual, badRate, u.ID, taskID)
	if err != nil {
		writeErr(w, 400, "回填失败："+err.Error())
		return
	}
	writeJSON(w, 200, row)
}

// advanceShipment 发货状态机：planned→picked→packed→shipped→delivered
func (s *Server) advanceShipment(w http.ResponseWriter, r *http.Request, projectID, shipID int64, ownFarm bool) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)
	var in map[string]interface{}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	to, _ := str(in, "status")
	flow := map[string]string{"picked": "planned", "packed": "picked", "shipped": "packed", "delivered": "shipped"}
	from, valid := flow[to]
	if !valid {
		writeErr(w, 400, "status 只能是 picked/packed/shipped/delivered")
		return
	}
	if !(ownFarm || u.Role == "logistics" || u.Role == "admin") {
		writeErr(w, 403, "只有农场或物流方可以推进发货")
		return
	}
	var cur string
	_ = s.Pool.QueryRow(r.Context(), `SELECT status FROM shipments WHERE id=$1 AND project_id=$2`,
		shipID, projectID).Scan(&cur)
	if cur != from {
		writeErr(w, 409, fmt.Sprintf("当前状态为 %s，不能直接流转到 %s", cur, to))
		return
	}
	q := `UPDATE shipments SET status=$1, operator_id=$2`
	args := []interface{}{to, u.ID}
	if to == "shipped" {
		q += `, shipped_at=now()`
		if v, ok := str(in, "logistics_company"); ok {
			q += fmt.Sprintf(`, logistics_company=$%d`, len(args)+1)
			args = append(args, v)
		}
		if v, ok := str(in, "tracking_no"); ok {
			q += fmt.Sprintf(`, tracking_no=$%d`, len(args)+1)
			args = append(args, v)
		}
		if v, ok := f64(in, "bad_fruit_rate"); ok {
			q += fmt.Sprintf(`, bad_fruit_rate=$%d`, len(args)+1)
			args = append(args, v)
		}
	}
	if to == "delivered" {
		q += `, delivered_at=now()`
	}
	q += fmt.Sprintf(` WHERE id=$%d AND project_id=$%d RETURNING *`, len(args)+1, len(args)+2)
	args = append(args, shipID, projectID)
	row, err := s.queryOne(r.Context(), q, args...)
	if err != nil {
		writeErr(w, 400, "推进失败："+err.Error())
		return
	}
	// 全部运单送达后，订单自动完成
	if to == "delivered" {
		_, _ = s.Pool.Exec(r.Context(), `
			UPDATE adoptions a SET status='completed'
			WHERE id=(SELECT adoption_id FROM shipments WHERE id=$1)
			  AND NOT EXISTS (SELECT 1 FROM shipments
			                  WHERE adoption_id=a.id AND status<>'delivered')`, shipID)
	}
	writeJSON(w, 200, row)
}

// handleSettle 生成最终交付与季度结算（产量/发货/赔付/满意度/续认建议）
func (s *Server) handleSettle(w http.ResponseWriter, r *http.Request, projectID int64, farmID int64, batchNo string, ownFarm bool) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)
	if !ownFarm {
		writeErr(w, 403, "只有农场方可以结算")
		return
	}
	var in map[string]interface{}
	_ = decode(r, &in)
	ctx := r.Context()

	var totalYield, totalShipped, totalSales, totalComp float64
	_ = s.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(actual_kg),0) FROM harvest_tasks WHERE project_id=$1`, projectID).Scan(&totalYield)
	_ = s.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(weight_kg),0) FROM shipments WHERE project_id=$1 AND status<>'planned'`, projectID).Scan(&totalShipped)
	_ = s.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM adoptions WHERE project_id=$1`, projectID).Scan(&totalSales)
	_ = s.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM compensations WHERE project_id=$1 AND status='paid'`, projectID).Scan(&totalComp)

	var totalCost float64
	_ = s.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(pc.amount),0) FROM plot_costs pc JOIN projects p ON p.plot_id=pc.plot_id
		 WHERE p.id=$1`, projectID).Scan(&totalCost)

	var avgSat interface{}
	_ = s.Pool.QueryRow(ctx,
		`SELECT ROUND(AVG(satisfaction),2) FROM callbacks WHERE project_id=$1`, projectID).Scan(&avgSat)
	var renewRate interface{}
	_ = s.Pool.QueryRow(ctx,
		`SELECT ROUND(AVG(CASE WHEN would_renew THEN 100 ELSE 0 END),2) FROM callbacks WHERE project_id=$1`,
		projectID).Scan(&renewRate)

	summary, _ := str(in, "summary")
	if summary == "" {
		summary = fmt.Sprintf("本季实际采摘 %.1fkg，发货 %.1fkg，销售额 ¥%.2f，成本 ¥%.2f，赔付 ¥%.2f。",
			totalYield, totalShipped, totalSales, totalCost, totalComp)
	}
	row, err := s.queryOne(ctx, `
		INSERT INTO settlements(project_id, batch_no, total_yield_kg, total_shipped_kg, total_sales,
			total_cost, total_compensation, avg_satisfaction, renew_rate_pct, summary)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (project_id) DO UPDATE SET
			total_yield_kg=EXCLUDED.total_yield_kg,
			total_shipped_kg=EXCLUDED.total_shipped_kg,
			total_sales=EXCLUDED.total_sales,
			total_cost=EXCLUDED.total_cost,
			total_compensation=EXCLUDED.total_compensation,
			avg_satisfaction=EXCLUDED.avg_satisfaction,
			renew_rate_pct=EXCLUDED.renew_rate_pct,
			summary=EXCLUDED.summary
		RETURNING *`,
		projectID, batchNo, totalYield, totalShipped, totalSales, totalCost, totalComp,
		avgSat, renewRate, summary)
	if err != nil {
		writeErr(w, 400, "结算失败："+err.Error())
		return
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE projects SET status='completed' WHERE id=$1`, projectID)
	writeJSON(w, 200, map[string]interface{}{"settlement": row, "operator": u.Name})
}

// ---------- 小工具 ----------

func nullableID(id int64) interface{} {
	if id == 0 {
		return nil
	}
	return id
}

func nullableF64(v float64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
