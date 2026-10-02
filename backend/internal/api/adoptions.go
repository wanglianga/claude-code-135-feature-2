package api

import (
	"fmt"
	"net/http"
	"strconv"
)

func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// handleAdoptions POST 认养付款（模拟支付），GET 列表（农场看全部 / 认养人看自己）
func (s *Server) handleAdoptions(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		q := `SELECT a.*, p.title AS project_title, p.kind, p.batch_no,
		             u.name AS user_name
		      FROM adoptions a
		      JOIN projects p ON p.id=a.project_id
		      JOIN users u ON u.id=a.user_id`
		args := []interface{}{}
		conds := []string{}
		if requireRole(u, "adopter") {
			conds = append(conds, fmt.Sprintf("a.user_id=$%d", len(args)+1))
			args = append(args, u.ID)
		} else if u.FarmID != nil && u.Role != "admin" {
			conds = append(conds, fmt.Sprintf("p.farm_id=$%d", len(args)+1))
			args = append(args, *u.FarmID)
		}
		if pid := r.URL.Query().Get("project_id"); pid != "" {
			conds = append(conds, fmt.Sprintf("a.project_id=$%d", len(args)+1))
			args = append(args, pid)
		}
		if len(conds) > 0 {
			q += " WHERE " + joinAnd(conds)
		}
		q += " ORDER BY a.id"
		rows, err := s.queryRows(ctx, q, args...)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, rows)
	case http.MethodPost:
		var in struct {
			ProjectID   int64  `json:"project_id"`
			Shares      int64  `json:"shares"`
			Recipient   string `json:"recipient_name"`
			Phone       string `json:"phone"`
			Address     string `json:"address"`
			IsCorporate bool   `json:"is_corporate"`
			CompanyName string `json:"company_name"`
			IsCharity   bool   `json:"is_charity"`
			Donee       string `json:"donee"`
			Message     string `json:"message"`
		}
		if err := decode(r, &in); err != nil || in.ProjectID == 0 || in.Shares <= 0 {
			writeErr(w, 400, "project_id 与 shares 必填")
			return
		}
		if in.Recipient == "" || in.Phone == "" || in.Address == "" {
			writeErr(w, 400, "收货人、手机号、收货地址必填")
			return
		}
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		defer tx.Rollback(ctx)

		var total, sold int64
		var price, guarantee float64
		var batchNo, kind string
		var farmID int64
		err = tx.QueryRow(ctx,
			`SELECT total_shares, sold_shares, price_per_share, guarantee_kg, batch_no, kind, farm_id
			 FROM projects WHERE id=$1 FOR UPDATE`, in.ProjectID).
			Scan(&total, &sold, &price, &guarantee, &batchNo, &kind, &farmID)
		if err != nil {
			writeErr(w, 404, "认养项目不存在")
			return
		}
		if int64(sold)+in.Shares > int64(total) {
			writeErr(w, 409, fmt.Sprintf("剩余份额不足：仅剩 %d 份", int64(total)-int64(sold)))
			return
		}
		amount := price * float64(in.Shares)
		var aid int64
		err = tx.QueryRow(ctx, `
			INSERT INTO adoptions(project_id, batch_no, user_id, shares, unit_price, amount,
				is_corporate, company_name, recipient_name, phone, address)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
			in.ProjectID, batchNo, u.ID, in.Shares, price, amount,
			in.IsCorporate, in.CompanyName, in.Recipient, in.Phone, in.Address).Scan(&aid)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		_, _ = tx.Exec(ctx, `UPDATE projects SET sold_shares=sold_shares+$1 WHERE id=$2`, in.Shares, in.ProjectID)

		// 认养证书随付款自动生成
		certNo := fmt.Sprintf("CERT-%s-%04d", batchNo, aid)
		_, _ = tx.Exec(ctx, `INSERT INTO certificates(adoption_id, cert_no) VALUES($1,$2)
		                     ON CONFLICT (adoption_id) DO NOTHING`, aid, certNo)

		// 公益认养同时登记受益方
		if in.IsCharity && in.Donee != "" {
			_, _ = tx.Exec(ctx,
				`INSERT INTO charity_adoptions(project_id, donor_name, shares, donee, message)
				 VALUES($1,$2,$3,$4,$5)`, in.ProjectID, u.Name, in.Shares, in.Donee, in.Message)
		}
		if err := tx.Commit(ctx); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		row, _ := s.queryOne(ctx, `SELECT * FROM adoptions WHERE id=$1`, aid)
		writeJSON(w, 201, map[string]interface{}{
			"adoption":    row,
			"cert_no":     certNo,
			"paid_amount": amount,
			"message":     "支付成功（模拟），认养证书已生成",
		})
	default:
		writeErr(w, 405, "方法不允许")
	}
}

func (s *Server) handleMyAdoptions(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	rows, err := s.queryRows(r.Context(), `
		SELECT a.*, p.title, p.kind, p.batch_no, p.guarantee_kg, p.rights_text,
		       f.name AS farm_name,
		       (SELECT cert_no FROM certificates WHERE adoption_id=a.id) AS cert_no
		FROM adoptions a JOIN projects p ON p.id=a.project_id
		JOIN farms f ON f.id=p.farm_id
		WHERE a.user_id=$1 ORDER BY a.id DESC`, u.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 附带每单的发货、赔付、回访、权益兑现情况
	result := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		aid, _ := row["id"].(int64)
		ships, _ := s.queryRows(r.Context(),
			`SELECT seq, plan_date, pack_spec, cold_route, weight_kg, status, tracking_no,
			        shipped_at, delivered_at, bad_fruit_rate
			 FROM shipments WHERE adoption_id=$1 ORDER BY seq`, aid)
		comps, _ := s.queryRows(r.Context(),
			`SELECT c.amount, c.kind, c.status, c.note, c.paid_at, i.title AS incident_title
			 FROM compensations c LEFT JOIN incidents i ON i.id=c.incident_id
			 WHERE c.adoption_id=$1 ORDER BY c.id`, aid)
		cbs, _ := s.queryRows(r.Context(),
			`SELECT * FROM callbacks WHERE adoption_id=$1`, aid)
		// 坏果赔付判定结论（同步认养档案）
		claims, _ := s.queryRows(r.Context(), `
			SELECT c.id, c.incident_id, c.bad_rate_pct, c.total_count, c.bad_count,
			       c.scheme, c.recommended_scheme, c.refund_amount, c.reship_cost, c.freight_cost,
			       c.cold_chain_responsible, c.conclusion, c.status,
			       c.reship_shipment_id, c.created_at, c.judged_at, c.closed_at,
			       sh.tracking_no AS reship_tracking_no, sh.status AS reship_status
			FROM bad_fruit_claims c
			LEFT JOIN shipments sh ON sh.id=c.reship_shipment_id
			WHERE c.adoption_id=$1 ORDER BY c.id`, aid)
		if claims == nil {
			claims = []map[string]interface{}{}
		}

		var shipped, plannedKg float64
		allDelivered := len(ships) > 0
		for _, sh := range ships {
			if v, ok := sh["weight_kg"].(float64); ok {
				st, _ := sh["status"].(string)
				if st == "planned" {
					plannedKg += v
					allDelivered = false
				} else {
					shipped += v
					if st != "delivered" {
						allDelivered = false
					}
				}
			} else {
				allDelivered = false
			}
		}
		sharesV, _ := row["shares"].(int64)
		if sharesV == 0 {
			if v, ok := asID(row["shares"]); ok {
				sharesV = v
			}
		}
		guarantee, _ := row["guarantee_kg"].(float64)
		need := guarantee * float64(sharesV)
		stage := "待发货"
		switch {
		case len(ships) == 0:
			stage = "待发货"
		case allDelivered:
			stage = "已完成"
		default:
			stage = "履约中"
		}

		// 已实现公平兑现：已发货量 + 赔付折算量（现金/抵扣按单价折回产量，补发按果价折算）
		// 预计公平兑现：再计入已排产未发出的批次
		var compKg, compAmount float64
		if up, ok := row["unit_price"].(float64); ok && up > 0 && guarantee > 0 {
			for _, c := range comps {
				amt, _ := c["amount"].(float64)
				kind, _ := c["kind"].(string)
				compAmount += amt
				if kind == "reship" {
					compKg += amt / (up / guarantee)
				} else {
					compKg += amt / up * guarantee
				}
			}
		}
		fair := shipped+compKg >= need-0.01
		projectedFair := shipped+plannedKg+compKg >= need-0.01
		fulfilled := map[string]interface{}{
			"stage":                stage,
			"guarantee_kg":         need,
			"shipped_kg":           shipped,
			"planned_kg":           plannedKg,
			"shortage_kg":          maxF(need-shipped-plannedKg-compKg, 0),
			"compensated_amount":   compAmount,
			"compensated_kg_equiv": roundF(compKg, 2),
			"fair":                 fair,
			"projected_fair":       projectedFair,
		}

		row["shipments"] = ships
		row["compensations"] = comps
		row["callbacks"] = cbs
		row["bad_fruit_claims"] = claims
		row["rights_fulfillment"] = fulfilled
		result = append(result, row)
	}
	writeJSON(w, 200, result)
}

func (s *Server) handleAdoptionByID(w http.ResponseWriter, r *http.Request) {
	id, action, ok := pathParts("/api/adoptions/", r.URL.Path)
	if !ok {
		writeErr(w, 404, "订单不存在")
		return
	}
	if action == "" {
		row, err := s.queryOne(r.Context(), `
			SELECT a.*, p.title, p.kind, p.guarantee_kg, f.name AS farm_name
			FROM adoptions a JOIN projects p ON p.id=a.project_id
			JOIN farms f ON f.id=p.farm_id WHERE a.id=$1`, id)
		if err != nil {
			writeErr(w, 404, "订单不存在")
			return
		}
		writeJSON(w, 200, row)
		return
	}
	if action == "address" {
		s.handleChangeAddress(w, r, id)
		return
	}
	writeErr(w, 404, "未知操作")
}

// handleChangeAddress 认养人临时改地址（未发货批次可改），并在工单流水中留痕
func (s *Server) handleChangeAddress(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "方法不允许")
		return
	}
	u := s.currentUser(r)
	var in struct {
		Recipient string `json:"recipient_name"`
		Phone     string `json:"phone"`
		Address   string `json:"address"`
		Reason    string `json:"reason"`
	}
	if err := decode(r, &in); err != nil || in.Address == "" {
		writeErr(w, 400, "新地址必填")
		return
	}
	ctx := r.Context()
	var userID, projectID int64
	var batchNo, oldAddr string
	err := s.Pool.QueryRow(ctx,
		`SELECT user_id, project_id, batch_no, address FROM adoptions WHERE id=$1`, id).
		Scan(&userID, &projectID, &batchNo, &oldAddr)
	if err != nil {
		writeErr(w, 404, "订单不存在")
		return
	}
	if u.ID != userID && u.Role != "service" && u.Role != "admin" {
		writeErr(w, 403, "只能修改本人订单地址")
		return
	}
	// 已进入 shipped/delivered 的批次不能改
	var blocked int
	_ = s.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM shipments WHERE adoption_id=$1 AND status IN ('shipped','delivered')`,
		id).Scan(&blocked)
	if blocked > 0 {
		writeErr(w, 409, "已有批次发出，不能在线改址，请联系客服")
		return
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx,
		`UPDATE adoptions SET recipient_name=COALESCE(NULLIF($2,''),recipient_name),
		                     phone=COALESCE(NULLIF($3,''),phone), address=$4 WHERE id=$1`,
		id, in.Recipient, in.Phone, in.Address)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var iid int64
	err = tx.QueryRow(ctx, `
		INSERT INTO incidents(project_id, batch_no, adoption_id, kind, title, description, reporter_id, status)
		VALUES($1,$2,$3,'address_change','认养人申请变更收货地址',$4,$5,'processing')
		RETURNING id`,
		projectID, batchNo, id,
		fmt.Sprintf("原地址：%s → 新地址：%s。原因：%s", oldAddr, in.Address, in.Reason), u.ID).Scan(&iid)
	if err == nil {
		_, _ = tx.Exec(ctx,
			`INSERT INTO incident_events(incident_id, actor_id, actor_role, action, note)
			 VALUES($1,$2,'adopter','申请改地址',$3)`, iid, u.ID, in.Reason)
	}
	if err := tx.Commit(ctx); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]interface{}{"ok": true, "incident_id": iid, "address": in.Address})
}

func joinAnd(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func roundF(v float64, n int) float64 {
	pow := 1.0
	for i := 0; i < n; i++ {
		pow *= 10
	}
	return float64(int64(v*pow+0.5)) / pow
}
