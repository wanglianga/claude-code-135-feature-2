package api

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleIncidents(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		q := `SELECT i.*, p.title AS project_title, p.batch_no,
		             ru.name AS reporter_name,
		             a.recipient_name, a.address
		      FROM incidents i
		      JOIN projects p ON p.id=i.project_id
		      LEFT JOIN users ru ON ru.id=i.reporter_id
		      LEFT JOIN adoptions a ON a.id=i.adoption_id`
		args := []interface{}{}
		conds := []string{}
		if r.URL.Query().Get("project_id") != "" {
			conds = append(conds, fmt.Sprintf("i.project_id=$%d", len(args)+1))
			args = append(args, r.URL.Query().Get("project_id"))
		}
		if k := r.URL.Query().Get("kind"); k != "" {
			conds = append(conds, fmt.Sprintf("i.kind=$%d", len(args)+1))
			args = append(args, k)
		}
		if st := r.URL.Query().Get("status"); st != "" {
			conds = append(conds, fmt.Sprintf("i.status=$%d", len(args)+1))
			args = append(args, st)
		}
		// 认养人只看与自己订单相关或批次级工单
		if u.Role == "adopter" {
			conds = append(conds, fmt.Sprintf(
				"(i.adoption_id IS NULL OR i.adoption_id IN (SELECT id FROM adoptions WHERE user_id=$%d))",
				len(args)+1))
			args = append(args, u.ID)
		} else if u.FarmID != nil && u.Role != "admin" && u.Role != "service" {
			conds = append(conds, fmt.Sprintf("p.farm_id=$%d", len(args)+1))
			args = append(args, *u.FarmID)
		}
		if len(conds) > 0 {
			q += " WHERE " + joinAnd(conds)
		}
		q += " ORDER BY i.created_at DESC"
		rows, err := s.queryRows(ctx, q, args...)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, rows)
	case http.MethodPost:
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		pid, _ := i64(in, "project_id")
		kind, _ := str(in, "kind")
		title, _ := str(in, "title")
		if pid == 0 || kind == "" || title == "" {
			writeErr(w, 400, "project_id/kind/title 必填")
			return
		}
		validKind := map[string]bool{"hail": true, "delay": true, "address_change": true,
			"size_uneven": true, "bad_fruit": true, "delivery_dispute": true,
			"rights_mismatch": true, "insurance": true, "other": true}
		if !validKind[kind] {
			writeErr(w, 400, "工单类型非法")
			return
		}
		var batchNo string
		_ = s.Pool.QueryRow(ctx, `SELECT batch_no FROM projects WHERE id=$1`, pid).Scan(&batchNo)
		var adoptionID interface{}
		if aid, ok := i64(in, "adoption_id"); ok {
			adoptionID = aid
		}
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		defer tx.Rollback(ctx)
		var iid int64
		err = tx.QueryRow(ctx, `
			INSERT INTO incidents(project_id, batch_no, adoption_id, kind, title, description, reporter_id)
			VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			pid, batchNo, adoptionID, kind, title, asString(in["description"]), u.ID).Scan(&iid)
		if err != nil {
			writeErr(w, 400, "创建失败："+err.Error())
			return
		}
		_, _ = tx.Exec(ctx, `
			INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
			VALUES($1,$2,$3,'提交工单',$4)`, iid, u.ID, u.Role, asString(in["description"]))
		if err := tx.Commit(ctx); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		row, _ := s.queryOne(ctx, `SELECT * FROM incidents WHERE id=$1`, iid)
		writeJSON(w, 201, row)
	default:
		writeErr(w, 405, "方法不允许")
	}
}

func (s *Server) handleIncidentByID(w http.ResponseWriter, r *http.Request) {
	id, action, ok := pathParts("/api/incidents/", r.URL.Path)
	if !ok {
		writeErr(w, 404, "工单不存在")
		return
	}
	if action != "" && action != "events" {
		writeErr(w, 404, "未知操作")
		return
	}
	ctx := r.Context()
	inc, err := s.queryOne(ctx, `
		SELECT i.*, p.title AS project_title, u.name AS reporter_name
		FROM incidents i JOIN projects p ON p.id=i.project_id
		LEFT JOIN users u ON u.id=i.reporter_id WHERE i.id=$1`, id)
	if err != nil {
		writeErr(w, 404, "工单不存在")
		return
	}
	events, _ := s.queryRows(ctx, `
		SELECT e.*, u.name AS actor_name FROM incident_events e
		LEFT JOIN users u ON u.id=e.actor_id
		WHERE e.incident_id=$1 ORDER BY e.created_at`, id)
	comps, _ := s.queryRows(ctx, `SELECT * FROM compensations WHERE incident_id=$1 ORDER BY id`, id)
	claim, _ := s.queryOne(ctx, `SELECT * FROM bad_fruit_claims WHERE incident_id=$1`, id)
	var claimDetail map[string]interface{}
	if claim != nil {
		claimID, _ := claim["id"].(int64)
		reships, _ := s.queryRows(ctx, `SELECT * FROM reship_orders WHERE claim_id=$1 ORDER BY id`, claimID)
		notices, _ := s.queryRows(ctx, `SELECT * FROM logistics_notices WHERE claim_id=$1 ORDER BY id`, claimID)
		claimDetail = claim
		claimDetail["reship_orders"] = reships
		claimDetail["logistics_notices"] = notices
	}
	// 关联运单的冷链温度曲线（坏果判定证据链）
	var coldRows []map[string]interface{}
	if claim != nil {
		if sid, ok := claim["shipment_id"].(int64); ok && sid > 0 {
			coldRows, _ = s.queryRows(ctx,
				`SELECT * FROM cold_chain_readings WHERE shipment_id=$1 ORDER BY recorded_at`, sid)
		}
	}

	if r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]interface{}{
			"incident":         inc,
			"events":           events,
			"compensations":    comps,
			"bad_fruit_claim":  claimDetail,
			"cold_chain_curve": coldRows,
		})
		return
	}

	// POST /api/incidents/{id}/events 追加角色处理结果
	if r.Method == http.MethodPost && action == "events" {
		u := s.currentUser(r)
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		act, _ := str(in, "action")
		if act == "" {
			writeErr(w, 400, "action 必填（如：核实情况/补发/更新冷链路线/回访确认）")
			return
		}
		note := asString(in["note"])
		// 角色可覆盖（管理员代录时允许指定 actor_role）
		actorRole := u.Role
		if r2, ok := str(in, "actor_role"); ok {
			actorRole = r2
		}
		row, err := s.insertRow(ctx, "incident_events", map[string]interface{}{
			"incident_id": id,
			"actor_id":    u.ID,
			"actor_role":  actorRole,
			"action":      act,
			"note":        note,
		})
		if err != nil {
			writeErr(w, 400, "记录失败："+err.Error())
			return
		}
		// 附带状态推进
		if st, ok := str(in, "status"); ok {
			_, _ = s.Pool.Exec(ctx,
				`UPDATE incidents SET status=$1, resolved_at=CASE WHEN $1 IN ('resolved','compensated') THEN now() ELSE resolved_at END WHERE id=$2`,
				st, id)
		}
		writeJSON(w, 201, row)
		return
	}

	// POST /api/incidents/{id} 关闭/解决工单
	if r.Method == http.MethodPost {
		u := s.currentUser(r)
		var in map[string]interface{}
		_ = decode(r, &in)
		status, _ := str(in, "status")
		if status == "" {
			status = "resolved"
		}
		if status != "processing" && status != "resolved" && status != "compensated" {
			writeErr(w, 400, "状态只能为 processing/resolved/compensated")
			return
		}
		_, err := s.Pool.Exec(ctx,
			`UPDATE incidents SET status=$1, resolved_at=CASE WHEN $1 IN ('resolved','compensated') THEN now() ELSE resolved_at END WHERE id=$2`,
			status, id)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		note, _ := str(in, "note")
		if note != "" {
			_, _ = s.Pool.Exec(ctx,
				`INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
				 VALUES($1,$2,$3,'工单状态更新',$4)`, id, u.ID, u.Role, note)
		}
		inc2, _ := s.queryOne(ctx, `SELECT * FROM incidents WHERE id=$1`, id)
		writeJSON(w, 200, inc2)
		return
	}
	writeErr(w, 405, "方法不允许")
}

// POST /api/compensations/incident/{id} 在工单下登记赔付
func (s *Server) handleCompensationByID(w http.ResponseWriter, r *http.Request) {
	// 支持 /api/compensations/incident/{incidentID}
	u := s.currentUser(r)
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	var incID int64
	if n := len("/api/compensations/incident/"); len(r.URL.Path) > n {
		v, err := parseID(r.URL.Path[n:])
		if err != nil {
			writeErr(w, 400, "工单ID非法")
			return
		}
		incID = v
	} else {
		writeErr(w, 404, "路径应为 /api/compensations/incident/{id}")
		return
	}
	var in map[string]interface{}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	amount, _ := f64(in, "amount")
	kind, _ := str(in, "kind")
	if !map[string]bool{"cash": true, "reship": true, "insurance": true, "credit": true}[kind] {
		writeErr(w, 400, "赔付方式只能是 cash/reship/insurance/credit")
		return
	}
	ctx := r.Context()
	var pid int64
	var adoptionID interface{}
	err := s.Pool.QueryRow(ctx, `SELECT project_id, adoption_id FROM incidents WHERE id=$1`, incID).
		Scan(&pid, &adoptionID)
	if err != nil {
		writeErr(w, 404, "工单不存在")
		return
	}
	if !(u.Role == "admin" || u.Role == "service" || u.Role == "farmer") {
		writeErr(w, 403, "只有农场或客服可以登记赔付")
		return
	}
	var cid int64
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO compensations(incident_id, adoption_id, project_id, amount, kind, note, status, paid_at)
		VALUES($1,$2,$3,$4,$5,$6,'paid',now()) RETURNING id`,
		incID, adoptionID, pid, amount, kind, asString(in["note"])).Scan(&cid)
	if err != nil {
		writeErr(w, 400, "登记失败："+err.Error())
		return
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE incidents SET status='compensated', resolved_at=now() WHERE id=$1`, incID)
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
		VALUES($1,$2,$3,'赔付已发放',$4)`,
		incID, u.ID, u.Role,
		fmt.Sprintf("赔付 ¥%.2f（%s）%s", amount, kind, asString(in["note"])))
	row, _ := s.queryOne(ctx, `SELECT * FROM compensations WHERE id=$1`, cid)
	writeJSON(w, 201, row)
}

// ---------- 保险理赔 ----------

func (s *Server) handleInsuranceClaims(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	ctx := r.Context()
	if r.Method == http.MethodGet {
		rows, err := s.queryRows(ctx, `
			SELECT ic.*, p.title AS project_title FROM insurance_claims ic
			JOIN projects p ON p.id=ic.project_id ORDER BY ic.applied_at DESC`)
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
	if !requireRole(u, "farmer", "admin") {
		writeErr(w, 403, "只有农场方可以报案")
		return
	}
	var in map[string]interface{}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	pid, _ := i64(in, "project_id")
	policy, _ := str(in, "policy_no")
	kind, _ := str(in, "kind")
	if pid == 0 || policy == "" || kind == "" {
		writeErr(w, 400, "project_id/policy_no/kind 必填")
		return
	}
	var batchNo string
	_ = s.Pool.QueryRow(ctx, `SELECT batch_no FROM projects WHERE id=$1`, pid).Scan(&batchNo)
	loss, _ := f64(in, "loss_kg")
	amount, _ := f64(in, "amount")
	row, err := s.insertRow(ctx, "insurance_claims", map[string]interface{}{
		"project_id": pid,
		"batch_no":   batchNo,
		"policy_no":  policy,
		"kind":       kind,
		"loss_kg":    loss,
		"amount":     amount,
		"note":       in["note"],
	})
	if err != nil {
		writeErr(w, 400, "报案失败："+err.Error())
		return
	}
	writeJSON(w, 201, row)
}

func (s *Server) handleInsuranceClaimByID(w http.ResponseWriter, r *http.Request) {
	id, _, ok := pathParts("/api/insurance-claims/", r.URL.Path)
	if !ok {
		writeErr(w, 404, "理赔不存在")
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)
	if !requireRole(u, "farmer", "admin", "service") {
		writeErr(w, 403, "无权推进理赔")
		return
	}
	var in map[string]interface{}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	status, _ := str(in, "status")
	valid := map[string]bool{"applied": true, "surveying": true, "approved": true, "settled": true, "rejected": true}
	if !valid[status] {
		writeErr(w, 400, "状态非法")
		return
	}
	ctx := r.Context()
	var incID interface{}
	q := `UPDATE insurance_claims SET status=$1`
	args := []interface{}{status}
	if status == "settled" {
		q += `, settled_at=now()`
		// 到账后自动生成保险赔付记录并挂到冰雹工单（如有）
		_ = s.Pool.QueryRow(ctx,
			`SELECT i.id FROM incidents i JOIN insurance_claims ic ON ic.project_id=i.project_id
			 WHERE ic.id=$1 AND i.kind='hail' ORDER BY i.id LIMIT 1`, id).Scan(&incID)
	}
	if amt, ok := f64(in, "amount"); ok {
		q += ", amount=$2"
		args = append(args, amt)
	}
	q += " WHERE id=$" + fmt.Sprint(len(args)+1)
	args = append(args, id)
	row, err := s.queryOne(ctx, q+" RETURNING *", args...)
	if err != nil {
		writeErr(w, 400, "推进失败："+err.Error())
		return
	}
	if status == "settled" && incID != nil {
		if iid, ok := incID.(int64); ok {
			if amt, ok := row["amount"].(float64); ok {
				_, _ = s.Pool.Exec(ctx, `
					INSERT INTO compensations(incident_id, project_id, amount, kind, note, status, paid_at)
					(SELECT $1, project_id, $2, 'insurance', '保险理赔到账', 'paid', now()
					 FROM insurance_claims WHERE id=$3)`, iid, amt, id)
				_, _ = s.Pool.Exec(ctx,
					`INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
					 VALUES($1,$2,'service','保险理赔到账',$3)`,
					iid, u.ID, fmt.Sprintf("到账 ¥%.2f，将按缺口折算发放给认养人", amt))
			}
		}
	}
	writeJSON(w, 200, row)
}

var _ = time.Now
