package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// pathParts 解析 /api/<resource>/<id>/<action...>
func pathParts(prefix, path string) (id int64, action string, ok bool) {
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return 0, "", false
	}
	parts := strings.Split(rest, "/")
	n, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	if len(parts) > 1 {
		action = parts[1]
	}
	return n, action, true
}

// ---------- 农场 ----------

func (s *Server) handleFarms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, "方法不允许")
		return
	}
	rows, err := s.queryRows(r.Context(), `SELECT * FROM farms ORDER BY id`)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}

// ---------- 地块 ----------

func (s *Server) handlePlots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := `SELECT p.*, f.name AS farm_name FROM plots p JOIN farms f ON f.id=p.farm_id`
		args := []interface{}{}
		if fid := r.URL.Query().Get("farm_id"); fid != "" {
			q += ` WHERE p.farm_id=$1 ORDER BY p.id`
			args = append(args, fid)
		} else {
			q += ` ORDER BY p.id`
		}
		rows, err := s.queryRows(r.Context(), q, args...)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, rows)
	case http.MethodPost:
		s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
			u := s.currentUser(r)
			if !requireRole(u, "farmer", "admin") {
				writeErr(w, 403, "只有农场主可以创建地块")
				return
			}
			var in map[string]interface{}
			if err := decode(r, &in); err != nil {
				writeErr(w, 400, "请求格式错误")
				return
			}
			farmID := u.FarmID
			if u.Role == "admin" {
				if id, ok := i64(in, "farm_id"); ok {
					farmID = &id
				}
			}
			if farmID == nil {
				writeErr(w, 400, "当前账号未关联农场")
				return
			}
			name, _ := str(in, "name")
			crop, _ := str(in, "crop")
			if name == "" || crop == "" {
				writeErr(w, 400, "地块名称和作物类型必填")
				return
			}
			area, _ := f64(in, "area_mu")
			loc, _ := str(in, "location")
			row, err := s.insertRow(r.Context(), "plots", map[string]interface{}{
				"farm_id":  *farmID,
				"name":     name,
				"crop":     crop,
				"area_mu":  area,
				"location": loc,
			})
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 201, row)
		})(w, r)
	default:
		writeErr(w, 405, "方法不允许")
	}
}

func (s *Server) handlePlotByID(w http.ResponseWriter, r *http.Request) {
	id, action, ok := pathParts("/api/plots/", r.URL.Path)
	if !ok {
		writeErr(w, 404, "地块不存在")
		return
	}
	switch action {
	case "":
		if r.Method != http.MethodGet {
			writeErr(w, 405, "方法不允许")
			return
		}
		rows, err := s.queryRows(r.Context(),
			`SELECT p.*, f.name AS farm_name FROM plots p JOIN farms f ON f.id=p.farm_id WHERE p.id=$1`, id)
		if err != nil || len(rows) == 0 {
			writeErr(w, 404, "地块不存在")
			return
		}
		writeJSON(w, 200, rows[0])
	case "ledger":
		s.handlePlotLedger(w, r, id)
	default:
		writeErr(w, 404, "未知资源")
	}
}

// handlePlotLedger 地块完整账：农事投入→灾害→成熟抽检→采摘→分拣→发货→售后回访→成本与满意度
func (s *Server) handlePlotLedger(w http.ResponseWriter, r *http.Request, id int64) {
	season := r.URL.Query().Get("season")
	if season == "" {
		season = "2026"
	}
	ctx := r.Context()
	plot, err := s.queryOne(ctx, `SELECT * FROM plots WHERE id=$1`, id)
	if err != nil {
		writeErr(w, 404, "地块不存在")
		return
	}

	costs, _ := s.queryRows(ctx,
		`SELECT cost_type, SUM(amount) AS amount, COUNT(*) AS n
		 FROM plot_costs WHERE plot_id=$1 AND season=$2 GROUP BY cost_type ORDER BY cost_type`, id, season)
	projects, _ := s.queryRows(ctx,
		`SELECT id, batch_no, kind, title, status, total_shares, sold_shares, price_per_share, guarantee_kg
		 FROM projects WHERE plot_id=$1 ORDER BY id`, id)
	logs, _ := s.queryRows(ctx,
		`SELECT fl.* FROM farm_logs fl JOIN projects p ON p.id=fl.project_id
		 WHERE p.plot_id=$1 ORDER BY fl.created_at`, id)
	disasters, _ := s.queryRows(ctx,
		`SELECT fl.* FROM farm_logs fl JOIN projects p ON p.id=fl.project_id
		 WHERE p.plot_id=$1 AND fl.log_type='disaster' ORDER BY fl.created_at`, id)
	checks, _ := s.queryRows(ctx,
		`SELECT mc.* FROM maturity_checks mc JOIN projects p ON p.id=mc.project_id
		 WHERE p.plot_id=$1 ORDER BY mc.checked_at`, id)
	harvests, _ := s.queryRows(ctx,
		`SELECT ht.*, u.name AS picker_name FROM harvest_tasks ht
		 LEFT JOIN users u ON u.id=ht.picker_id
		 WHERE ht.plot_id=$1 ORDER BY ht.planned_date`, id)
	sorting, _ := s.queryRows(ctx,
		`SELECT sr.*, u.name AS packer_name FROM sorting_records sr
		 JOIN projects p ON p.id=sr.project_id
		 LEFT JOIN users u ON u.id=sr.packer_id
		 WHERE p.plot_id=$1 ORDER BY sr.created_at`, id)
	shipments, _ := s.queryRows(ctx,
		`SELECT sh.* FROM shipments sh JOIN projects p ON p.id=sh.project_id
		 WHERE p.plot_id=$1 ORDER BY sh.seq, sh.id`, id)
	incidents, _ := s.queryRows(ctx,
		`SELECT i.* FROM incidents i JOIN projects p ON p.id=i.project_id
		 WHERE p.plot_id=$1 ORDER BY i.created_at`, id)
	compensations, _ := s.queryRows(ctx,
		`SELECT c.* FROM compensations c JOIN projects p ON p.id=c.project_id
		 WHERE p.plot_id=$1 ORDER BY c.created_at`, id)
	badFruitClaims, _ := s.queryRows(ctx,
		`SELECT bc.* FROM bad_fruit_claims bc JOIN projects p ON p.id=bc.project_id
		 WHERE p.plot_id=$1 ORDER BY bc.id`, id)
	callbacks, _ := s.queryRows(ctx,
		`SELECT cb.* FROM callbacks cb JOIN projects p ON p.id=cb.project_id
		 WHERE p.plot_id=$1 ORDER BY cb.created_at`, id)

	var totalCost float64
	for _, c := range costs {
		if v, ok := c["amount"].(float64); ok {
			totalCost += v
		}
	}
	var totalShipped float64
	var badRateSum, badRateN float64
	for _, sh := range shipments {
		if v, ok := sh["weight_kg"].(float64); ok {
			totalShipped += v
		}
		if v, ok := sh["bad_fruit_rate"].(float64); ok && sh["status"] != "planned" {
			badRateSum += v
			badRateN++
		}
	}
	avgBad := 0.0
	if badRateN > 0 {
		avgBad = badRateSum / badRateN
	}
	var satSum, satN float64
	for _, cb := range callbacks {
		if v, ok := numVal(cb["satisfaction"]); ok {
			satSum += v
			satN++
		}
	}
	avgSat := 0.0
	if satN > 0 {
		avgSat = satSum / satN
	}

	writeJSON(w, 200, map[string]interface{}{
		"plot":             plot,
		"season":           season,
		"projects":         projects,
		"costs":            costs,
		"total_cost":       totalCost,
		"logs":             logs,
		"disasters":        disasters,
		"maturity_checks":  checks,
		"harvest_tasks":    harvests,
		"sorting":          sorting,
		"shipments":        shipments,
		"total_shipped":    totalShipped,
		"avg_bad_rate":     avgBad,
		"incidents":        incidents,
		"compensations":    compensations,
		"bad_fruit_claims": badFruitClaims,
		"callbacks":        callbacks,
		"avg_satisfaction": avgSat,
	})
}

// ---------- 认养项目 ----------

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := `SELECT p.*, f.name AS farm_name, pl.name AS plot_name
		      FROM projects p JOIN farms f ON f.id=p.farm_id JOIN plots pl ON pl.id=p.plot_id`
		conds := []string{}
		args := []interface{}{}
		if v := r.URL.Query().Get("kind"); v != "" {
			conds = append(conds, fmt.Sprintf("p.kind=$%d", len(args)+1))
			args = append(args, v)
		}
		if v := r.URL.Query().Get("farm_id"); v != "" {
			conds = append(conds, fmt.Sprintf("p.farm_id=$%d", len(args)+1))
			args = append(args, v)
		}
		if v := r.URL.Query().Get("status"); v != "" {
			conds = append(conds, fmt.Sprintf("p.status=$%d", len(args)+1))
			args = append(args, v)
		}
		if len(conds) > 0 {
			q += " WHERE " + strings.Join(conds, " AND ")
		}
		q += " ORDER BY p.id"
		rows, err := s.queryRows(r.Context(), q, args...)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, rows)
	case http.MethodPost:
		u := s.currentUser(r)
		if u == nil {
			writeErr(w, 401, "未登录")
			return
		}
		if !requireRole(u, "farmer", "admin") {
			writeErr(w, 403, "只有农场主可以发布认养项目")
			return
		}
		var in map[string]interface{}
		if err := decode(r, &in); err != nil {
			writeErr(w, 400, "请求格式错误")
			return
		}
		plotID, _ := i64(in, "plot_id")
		batchNo, _ := str(in, "batch_no")
		kind, _ := str(in, "kind")
		title, _ := str(in, "title")
		price, hasPrice := f64(in, "price_per_share")
		total, _ := i64(in, "total_shares")
		if plotID == 0 || batchNo == "" || kind == "" || title == "" || !hasPrice {
			writeErr(w, 400, "plot_id/batch_no/kind/title/price_per_share 必填")
			return
		}
		var plotFarm int64
		if err := s.Pool.QueryRow(r.Context(),
			`SELECT farm_id FROM plots WHERE id=$1`, plotID).Scan(&plotFarm); err != nil {
			writeErr(w, 404, "地块不存在")
			return
		}
		if u.Role != "admin" && (u.FarmID == nil || *u.FarmID != plotFarm) {
			writeErr(w, 403, "不能在他人农场的地块上发布项目")
			return
		}
		if total == 0 {
			total = 100
		}
		data := map[string]interface{}{
			"farm_id":          plotFarm,
			"plot_id":          plotID,
			"batch_no":         batchNo,
			"kind":             kind,
			"title":            title,
			"description":      in["description"],
			"unit_name":        in["unit_name"],
			"total_shares":     total,
			"price_per_share":  price,
			"guarantee_kg":     in["guarantee_kg"],
			"est_mature_date":  in["est_mature_date"],
			"harvest_standard": in["harvest_standard"],
			"delivery_range":   in["delivery_range"],
			"rights_text":      in["rights_text"],
			"insurance_policy": in["insurance_policy"],
			"is_charity":       in["is_charity"],
		}
		if data["unit_name"] == nil {
			data["unit_name"] = "份"
		}
		row, err := s.insertRow(r.Context(), "projects", data)
		if err != nil {
			writeErr(w, 400, "发布失败："+err.Error())
			return
		}
		writeJSON(w, 201, row)
	default:
		writeErr(w, 405, "方法不允许")
	}
}

func (s *Server) handleProjectByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
	if rest == "" {
		writeErr(w, 404, "项目不存在")
		return
	}
	parts := strings.Split(rest, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeErr(w, 404, "项目不存在")
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			writeErr(w, 405, "方法不允许")
			return
		}
		s.projectDetail(w, r, id)
		return
	}
	// 子资源写操作均需登录
	s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		s.projectSubresource(w, r, id, parts[1:])
	})(w, r)
}

func (s *Server) projectDetail(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	p, err := s.queryOne(ctx, `
		SELECT p.*, f.name AS farm_name, f.location AS farm_location,
		       pl.name AS plot_name, pl.area_mu
		FROM projects p
		JOIN farms f ON f.id=p.farm_id
		JOIN plots pl ON pl.id=p.plot_id
		WHERE p.id=$1`, id)
	if err != nil {
		writeErr(w, 404, "认养项目不存在")
		return
	}
	logs, _ := s.queryRows(ctx,
		`SELECT fl.*, u.name AS author_name FROM farm_logs fl LEFT JOIN users u ON u.id=fl.author_id
		 WHERE fl.project_id=$1 ORDER BY fl.created_at DESC`, id)
	checks, _ := s.queryRows(ctx,
		`SELECT mc.*, u.name AS inspector_name FROM maturity_checks mc LEFT JOIN users u ON u.id=mc.inspector_id
		 WHERE mc.project_id=$1 ORDER BY mc.checked_at DESC`, id)
	harvests, _ := s.queryRows(ctx,
		`SELECT ht.*, u.name AS picker_name FROM harvest_tasks ht LEFT JOIN users u ON u.id=ht.picker_id
		 WHERE ht.project_id=$1 ORDER BY ht.planned_date`, id)
	sorting, _ := s.queryRows(ctx,
		`SELECT sr.*, u.name AS packer_name FROM sorting_records sr LEFT JOIN users u ON u.id=sr.packer_id
		 WHERE sr.project_id=$1 ORDER BY sr.created_at`, id)
	shipments, _ := s.queryRows(ctx,
		`SELECT sh.*, a.recipient_name, a.address FROM shipments sh
		 LEFT JOIN adoptions a ON a.id=sh.adoption_id
		 WHERE sh.project_id=$1 ORDER BY sh.seq, sh.id`, id)
	incidents, _ := s.queryRows(ctx,
		`SELECT i.*, u.name AS reporter_name FROM incidents i LEFT JOIN users u ON u.id=i.reporter_id
		 WHERE i.project_id=$1 ORDER BY i.created_at`, id)
	badClaims, _ := s.queryRows(ctx,
		`SELECT c.*, a.recipient_name FROM bad_fruit_claims c
		 LEFT JOIN adoptions a ON a.id=c.adoption_id
		 WHERE c.project_id=$1 ORDER BY c.id`, id)
	claims, _ := s.queryRows(ctx,
		`SELECT * FROM insurance_claims WHERE project_id=$1 ORDER BY applied_at`, id)
	charity, _ := s.queryRows(ctx,
		`SELECT * FROM charity_adoptions WHERE project_id=$1 ORDER BY created_at`, id)
	groupBuys, _ := s.queryRows(ctx,
		`SELECT * FROM group_buys WHERE project_id=$1 ORDER BY created_at`, id)
	settlements, _ := s.queryRows(ctx,
		`SELECT * FROM settlements WHERE project_id=$1`, id)
	live, _ := s.queryRows(ctx,
		`SELECT * FROM live_streams WHERE project_id=$1 ORDER BY starts_at DESC`, id)
	var settlement interface{}
	if len(settlements) > 0 {
		settlement = settlements[0]
	}

	var harvested, shipped float64
	for _, h := range harvests {
		if v, ok := h["actual_kg"].(float64); ok {
			harvested += v
		}
	}
	for _, sh := range shipments {
		if v, ok := sh["weight_kg"].(float64); ok && sh["status"] != "planned" {
			shipped += v
		}
	}
	writeJSON(w, 200, map[string]interface{}{
		"project":          p,
		"logs":             logs,
		"maturity_checks":  checks,
		"harvest_tasks":    harvests,
		"sorting":          sorting,
		"shipments":        shipments,
		"incidents":        incidents,
		"bad_fruit_claims": badClaims,
		"insurance_claims": claims,
		"charity":          charity,
		"group_buys":       groupBuys,
		"live_streams":     live,
		"settlement":       settlement,
		"sum_harvested":    harvested,
		"sum_shipped":      shipped,
	})
}
