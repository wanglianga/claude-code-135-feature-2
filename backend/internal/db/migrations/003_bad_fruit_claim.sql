-- 坏果赔付判定：关联采摘时间、分拣照片、冷链温度曲线、签收时间、开箱照片；
-- 客服按坏果比例生成补发 / 退款 / 物流追责方案；运费与补发成本分开计算；
-- 补发订单同步物流端，冷链责任同步物流，赔付结论同步认养档案。

-- 分拣照片（坏果核定时回溯包装环节）
ALTER TABLE sorting_records ADD COLUMN IF NOT EXISTS photos JSONB NOT NULL DEFAULT '[]';

-- 运单补充：补发单标记 + 冷链温度曲线（温度点序列/极值/是否脱冷）
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS is_reship BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS claim_id BIGINT;
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS cold_chain_curve JSONB NOT NULL DEFAULT '[]';
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS cold_temp_min NUMERIC(5,2);
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS cold_temp_max NUMERIC(5,2);
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS cold_breached BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS cold_logged_by BIGINT REFERENCES users(id);
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS cold_logged_at TIMESTAMPTZ;

-- 坏果赔付判定单
CREATE TABLE IF NOT EXISTS bad_fruit_claims (
    id                BIGSERIAL PRIMARY KEY,
    incident_id       BIGINT NOT NULL UNIQUE REFERENCES incidents(id) ON DELETE CASCADE,
    adoption_id       BIGINT REFERENCES adoptions(id) ON DELETE SET NULL,
    project_id        BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    shipment_id       BIGINT REFERENCES shipments(id) ON DELETE SET NULL,
    batch_no          TEXT NOT NULL,
    -- 认养人申报：坏果数量与开箱照片
    total_count       INT NOT NULL DEFAULT 0,           -- 签收总果/总盒数
    bad_count         INT NOT NULL DEFAULT 0,           -- 坏果数
    bad_rate_pct      NUMERIC(6,2) NOT NULL DEFAULT 0,  -- 坏果比例 %
    open_box_photos   JSONB NOT NULL DEFAULT '[]',      -- 开箱照片
    -- 平台在赔付关闭前保留的证据快照：采摘时间 / 分拣照片 / 签收时间 / 冷链曲线
    harvest_at        TIMESTAMPTZ,                      -- 关联采摘时间
    sorting_photos    JSONB NOT NULL DEFAULT '[]',      -- 分拣照片快照
    signed_at         TIMESTAMPTZ,                      -- 签收时间
    cold_route        TEXT NOT NULL DEFAULT '',
    logistics_company TEXT NOT NULL DEFAULT '',
    tracking_no       TEXT NOT NULL DEFAULT '',
    cold_temp_min     NUMERIC(5,2),
    cold_temp_max     NUMERIC(5,2),
    cold_breached     BOOLEAN NOT NULL DEFAULT FALSE,
    cold_chain_curve  JSONB NOT NULL DEFAULT '[]',      -- 冷链温度曲线快照
    -- 客服判定结论：reship 补发 / refund 退款 / logistics_claim 物流追责
    scheme            TEXT CHECK (scheme IN ('reship','refund','logistics_claim')),
    recommended_scheme TEXT NOT NULL DEFAULT '',        -- 按坏果比例与冷链情况给出的建议方案
    refund_amount     NUMERIC(12,2) NOT NULL DEFAULT 0,-- 退款金额
    reship_cost       NUMERIC(12,2) NOT NULL DEFAULT 0,-- 补发果品成本（与运费分开）
    freight_cost      NUMERIC(12,2) NOT NULL DEFAULT 0,-- 运费（物流追责时与补发成本分开）
    cold_chain_responsible BOOLEAN NOT NULL DEFAULT FALSE, -- 是否判定冷链（物流）责任
    cold_synced_at    TIMESTAMPTZ,                      -- 冷链责任同步物流时间
    conclusion        TEXT NOT NULL DEFAULT '',         -- 客服结论
    reship_shipment_id BIGINT REFERENCES shipments(id), -- 补发订单（同步物流端）
    reporter_id       BIGINT REFERENCES users(id),
    handler_id        BIGINT REFERENCES users(id),
    status            TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','judged','closed')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    judged_at         TIMESTAMPTZ,
    closed_at         TIMESTAMPTZ                       -- 赔付关闭时间（证据关闭后仍保留）
);

CREATE INDEX IF NOT EXISTS idx_bfc_project ON bad_fruit_claims(project_id);
CREATE INDEX IF NOT EXISTS idx_bfc_adoption ON bad_fruit_claims(adoption_id);
CREATE INDEX IF NOT EXISTS idx_shipments_claim ON shipments(claim_id);

-- ============================================================================
-- 演示故事线：蓝莓批次 BERRY-2026-A 两笔坏果赔付判定
--   1) 陈曦散单：冷链达标，坏果率 25% → 补发（运费与补发成本分开）
--   2) 星辰科技团购：中转脱冷（最高 9.4℃），坏果率 40% → 物流追责，冷链责任同步物流
-- ============================================================================

-- 蓝莓采摘时间（赔付关联证据）
INSERT INTO harvest_tasks (id, project_id, batch_no, plot_id, picker_id, priority, planned_date, target_kg, actual_kg, bad_rate_pct, status) VALUES
 (3,2,'BERRY-2026-A',2,4,10,'2026-06-25',18.00,17.60,1.20,'done'),
 (4,2,'BERRY-2026-A',2,4,20,'2026-06-26',15.00,15.10,0.90,'done')
ON CONFLICT (id) DO NOTHING;
SELECT setval('harvest_tasks_id_seq', GREATEST((SELECT MAX(id) FROM harvest_tasks), 2));

-- 分拣照片（赔付关联证据）
INSERT INTO sorting_records (id, project_id, batch_no, packer_id, spec, weight_kg, box_count, photos) VALUES
 (5,2,'BERRY-2026-A',5,'优瑞卡鲜果6斤礼盒（12小盒）',3.20,12,
  '[{"caption":"分拣台果粉完整","url":"/static/demo/berry-sort-1.jpg"},{"caption":"泡沫托装盒照","url":"/static/demo/berry-sort-2.jpg"}]'),
 (6,2,'BERRY-2026-A',5,'企业团购蓝莓礼盒3斤（6小盒）',15.00,10,
  '[{"caption":"团购礼盒装箱照","url":"/static/demo/berry-sort-3.jpg"}]')
ON CONFLICT (id) DO NOTHING;
SELECT setval('sorting_records_id_seq', GREATEST((SELECT MAX(id) FROM sorting_records), 4));

-- 两笔原始运单的冷链温度曲线
UPDATE shipments SET
  cold_chain_curve='[
    {"t":"2026-06-25 09:30","temp":2.4},{"t":"2026-06-25 13:00","temp":3.1},
    {"t":"2026-06-25 18:00","temp":2.0},{"t":"2026-06-25 23:00","temp":1.2},
    {"t":"2026-06-26 06:00","temp":0.6},{"t":"2026-06-26 09:30","temp":3.8}]',
  cold_temp_min=0.60, cold_temp_max=3.80, cold_breached=FALSE,
  cold_logged_by=6, cold_logged_at='2026-06-26 10:30:00+08'
WHERE id=4;

UPDATE shipments SET
  cold_chain_curve='[
    {"t":"2026-06-26 08:00","temp":2.2},{"t":"2026-06-26 12:00","temp":7.8},
    {"t":"2026-06-26 16:00","temp":9.4},{"t":"2026-06-26 20:00","temp":6.1},
    {"t":"2026-06-27 00:00","temp":3.5},{"t":"2026-06-27 08:00","temp":1.2}]',
  cold_temp_min=1.20, cold_temp_max=9.40, cold_breached=TRUE,
  cold_logged_by=6, cold_logged_at='2026-06-27 10:00:00+08'
WHERE id=5;

-- 两笔坏果工单
INSERT INTO incidents (id, project_id, batch_no, adoption_id, kind, title, description, status, reporter_id, created_at, resolved_at) VALUES
 (5,2,'BERRY-2026-A',4,'bad_fruit','蓝莓礼盒开箱坏果较多（陈曦）',
    '陈曦签收 6 斤礼盒（12 小盒），开箱发现 3 小盒底层果粒压伤、长霉，坏果比例 25%，上传开箱照片要求处理。',
    'compensated',3,'2026-06-26 18:10:00+08','2026-06-28 11:00:00+08'),
 (6,2,'BERRY-2026-A',5,'bad_fruit','企业团购蓝莓大面积坏果，疑冷链脱冷（星辰科技）',
    '星辰科技 30 斤团购礼盒（30 小盒）中有 12 小盒出汁霉变，坏果比例 40%，企业行政质疑冷链中转脱冷。',
    'compensated',8,'2026-06-27 15:00:00+08','2026-06-29 16:00:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('incidents_id_seq', GREATEST((SELECT MAX(id) FROM incidents), 4));

-- 工单流水：认养人→物流（冷链温度）→包装员（分拣照片）→客服判定→补发同步物流/冷链责任同步
INSERT INTO incident_events (id, incident_id, actor_id, actor_role, action, note, created_at) VALUES
 (17,5,3,'adopter','开箱反馈坏果并上传照片',
    '12 小盒中 3 盒底层压伤霉变，坏果比例 25%，上传 3 张开箱照片。','2026-06-26 18:10:00+08'),
 (18,5,6,'logistics','提供冷链温度曲线',
    '全程 0.6-3.8℃ 在 0-4℃ 标准内，中转时长 25.5 小时无异常，非冷链责任。','2026-06-26 20:00:00+08'),
 (19,5,5,'packer','回溯分拣照片',
    '调取分拣台与装盒照片，果粉完整、无坏果；判定为运输中单层泡沫托缓冲不足导致压伤。','2026-06-27 09:30:00+08'),
 (20,5,7,'service','客服出具坏果赔付判定',
    '坏果比例 25%（<30%）且冷链达标，方案：补发 3 小盒；补发果品成本 ¥45.00，运费 ¥18.00 分开计算。','2026-06-27 14:00:00+08'),
 (21,5,7,'service','补发同步物流并关闭赔付',
    '补发货运单已推送物流端；赔付结论同步认养档案，开箱照片/冷链曲线/客服结论保留备查。','2026-06-28 11:00:00+08'),
 (22,6,8,'adopter','开箱反馈坏果并上传照片',
    '30 小盒中 12 盒出汁霉变，坏果比例 40%，附开箱照片与企业签收记录。','2026-06-27 15:00:00+08'),
 (23,6,6,'logistics','提供冷链温度曲线并确认异常',
    '曲线显示 6 月 26 日中午济南中转滞留 6 小时，最高温 9.4℃，确认脱冷，属物流冷链责任。','2026-06-27 17:30:00+08'),
 (24,6,5,'packer','回溯分拣照片',
    '分拣装箱照片显示出库时果粒完好、预冷到位，排除包装与果品原因。','2026-06-28 09:00:00+08'),
 (25,6,7,'service','客服出具坏果赔付判定',
    '坏果比例 40% 且冷链脱冷，方案：物流追责并补发 10 小盒；补发成本 ¥570.00，运费 ¥260.00 分开计算，运费向物流全额追偿。','2026-06-28 15:00:00+08'),
 (26,6,7,'service','冷链责任同步物流并关闭赔付',
    '冷链责任判定已同步物流端，运费 ¥260.00 由物流承担；补发运单推送物流，结论同步企业认养档案。','2026-06-29 16:00:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('incident_events_id_seq', GREATEST((SELECT MAX(id) FROM incident_events), 16));

-- 补发运单（同步物流端，is_reship 标记）
INSERT INTO shipments (id, project_id, adoption_id, batch_no, seq, plan_date, pack_spec, cold_route, weight_kg, status,
    logistics_company, tracking_no, bad_fruit_rate, is_reship, claim_id, operator_id, shipped_at, delivered_at,
    cold_temp_min, cold_temp_max, cold_breached, cold_logged_by, cold_logged_at) VALUES
 (6,2,4,'BERRY-2026-A',2,'2026-06-28','坏果补发：优瑞卡鲜果 3 小盒（加厚泡沫托）','烟台-北京（生鲜专机+冷链）',0.75,'delivered',
    '顺丰冷运','SFFRESH2026062801',0.00,TRUE,1,6,'2026-06-28 16:00:00+08','2026-06-29 10:30:00+08',
    0.80,3.20,FALSE,6,'2026-06-29 11:00:00+08'),
 (7,2,5,'BERRY-2026-A',2,'2026-06-30','坏果补发：企业团购蓝莓礼盒 10 小盒','烟台-上海虹桥（生鲜冷链整车）',5.00,'delivered',
    '顺丰冷运','SFFRESH2026063002',0.00,TRUE,2,6,'2026-06-30 07:00:00+08','2026-07-01 09:00:00+08',
    0.90,3.50,FALSE,6,'2026-07-01 09:30:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('shipments_id_seq', GREATEST((SELECT MAX(id) FROM shipments), 5));

-- 坏果赔付判定单（证据快照 + 客服结论，关闭后仍保留）
INSERT INTO bad_fruit_claims (id, incident_id, adoption_id, project_id, shipment_id, batch_no,
    total_count, bad_count, bad_rate_pct, open_box_photos,
    harvest_at, sorting_photos, signed_at, cold_route, logistics_company, tracking_no,
    cold_temp_min, cold_temp_max, cold_breached, cold_chain_curve,
    scheme, recommended_scheme, refund_amount, reship_cost, freight_cost,
    cold_chain_responsible, cold_synced_at, conclusion, reship_shipment_id,
    reporter_id, handler_id, status, created_at, judged_at, closed_at) VALUES
 (1,5,4,2,4,'BERRY-2026-A',
    12,3,25.00,
    '[{"caption":"开箱全景：3 盒霉变","url":"/static/demo/berry-open-1.jpg"},{"caption":"底层压伤果特写","url":"/static/demo/berry-open-2.jpg"},{"caption":"正常果对比","url":"/static/demo/berry-open-3.jpg"}]',
    '2026-06-25 12:00:00+08',
    '[{"caption":"分拣台果粉完整","url":"/static/demo/berry-sort-1.jpg"},{"caption":"泡沫托装盒照","url":"/static/demo/berry-sort-2.jpg"}]',
    '2026-06-26 10:00:00+08','烟台-北京（生鲜专机+冷链）','顺丰冷运','SFFRESH2026062501',
    0.60,3.80,FALSE,
    '[{"t":"2026-06-25 09:30","temp":2.4},{"t":"2026-06-25 13:00","temp":3.1},{"t":"2026-06-25 18:00","temp":2.0},{"t":"2026-06-25 23:00","temp":1.2},{"t":"2026-06-26 06:00","temp":0.6},{"t":"2026-06-26 09:30","temp":3.8}]',
    'reship','reship',0.00,45.00,18.00,
    FALSE,NULL,
    '坏果比例 25%，冷链全程达标，判定为包装缓冲不足导致压伤；补发 3 小盒并升级加厚泡沫托，运费 18 元由农场承担，与补发成本分开核算。',
    6,3,7,'closed','2026-06-26 18:10:00+08','2026-06-27 14:00:00+08','2026-06-28 11:00:00+08'),
 (2,6,5,2,5,'BERRY-2026-A',
    30,12,40.00,
    '[{"caption":"团购箱开箱：12 盒霉变","url":"/static/demo/berry-open-g1.jpg"},{"caption":"出汁果盒特写","url":"/static/demo/berry-open-g2.jpg"},{"caption":"企业签收记录","url":"/static/demo/berry-open-g3.jpg"}]',
    '2026-06-26 10:00:00+08',
    '[{"caption":"团购礼盒装箱照","url":"/static/demo/berry-sort-3.jpg"}]',
    '2026-06-27 09:30:00+08','烟台-上海虹桥（生鲜冷链整车）','顺丰冷运','SFFRESH2026062608',
    1.20,9.40,TRUE,
    '[{"t":"2026-06-26 08:00","temp":2.2},{"t":"2026-06-26 12:00","temp":7.8},{"t":"2026-06-26 16:00","temp":9.4},{"t":"2026-06-26 20:00","temp":6.1},{"t":"2026-06-27 00:00","temp":3.5},{"t":"2026-06-27 08:00","temp":1.2}]',
    'logistics_claim','logistics_claim',0.00,570.00,260.00,
    TRUE,'2026-06-29 16:00:00+08',
    '坏果比例 40%，冷链曲线显示中转滞留脱冷（最高 9.4℃），判定冷链（物流）责任；补发 10 小盒，补发成本 570 元与运费 260 元分开计算，运费向物流全额追偿。',
    7,8,7,'closed','2026-06-27 15:00:00+08','2026-06-28 15:00:00+08','2026-06-29 16:00:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('bad_fruit_claims_id_seq', (SELECT MAX(id) FROM bad_fruit_claims));

-- 赔付记录（补发折值，结论同步认养档案）
INSERT INTO compensations (id, incident_id, adoption_id, project_id, amount, kind, status, note, created_at, paid_at) VALUES
 (4,5,4,2,45.00,'reship','paid','坏果补发 3 小盒优瑞卡（补发成本 ¥45，运费 ¥18 分开核算）','2026-06-27 14:30:00+08','2026-06-28 11:00:00+08'),
 (5,6,5,2,570.00,'reship','paid','冷链脱冷坏果补发 10 小盒（补发成本 ¥570，运费 ¥260 向物流追偿）','2026-06-28 15:30:00+08','2026-06-29 16:00:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('compensations_id_seq', GREATEST((SELECT MAX(id) FROM compensations), 3));

-- 运费（物流成本）与补发成本（赔付成本）分开入账
INSERT INTO plot_costs (id, plot_id, project_id, season, cost_type, item, amount) VALUES
 (9,2,2,'2026','compensation','坏果补发果品成本（赔付单 #1 陈曦 3 小盒）',45.00),
 (10,2,2,'2026','logistics','坏果补发运费（赔付单 #1）',18.00),
 (11,2,2,'2026','compensation','冷链脱冷坏果补发成本（赔付单 #2 星辰科技 10 小盒）',570.00),
 (12,2,2,'2026','logistics','冷链脱冷补发运费（赔付单 #2，向物流追偿）',260.00)
ON CONFLICT (id) DO NOTHING;
SELECT setval('plot_costs_id_seq', GREATEST((SELECT MAX(id) FROM plot_costs), 8));
