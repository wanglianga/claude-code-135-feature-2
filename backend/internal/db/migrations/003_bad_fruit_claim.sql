-- 坏果赔付判定（feature-2-a）
-- 认养人反馈坏果较多 → 平台关联：采摘时间 / 分拣照片 / 冷链温度曲线 /
-- 签收时间 / 开箱照片 → 客服按坏果比例生成 补发/退款/物流追责 方案；
-- 赔付关闭前保留开箱照片、冷链曲线、客服结论；
-- 物流追责时运费与补发成本分开计算；
-- 赔付结论同步认养档案；补发订单同步物流端；冷链责任同步物流。

-- 1) 采摘任务：采摘时间（原仅有计划日期）
ALTER TABLE harvest_tasks ADD COLUMN IF NOT EXISTS harvested_at TIMESTAMPTZ;

-- 2) 分拣记录：分拣照片（包装出库留证）
ALTER TABLE sorting_records ADD COLUMN IF NOT EXISTS photos JSONB NOT NULL DEFAULT '[]';

-- 3) 冷链温度曲线（一条运单可有多段/多点测温）
CREATE TABLE IF NOT EXISTS cold_chain_readings (
    id           BIGSERIAL PRIMARY KEY,
    shipment_id  BIGINT NOT NULL REFERENCES shipments(id) ON DELETE CASCADE,
    project_id   BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    batch_no     TEXT NOT NULL,
    point        TEXT NOT NULL DEFAULT '',   -- 测温节点：装车预冷/济南分拨/送达前…
    temp_c       NUMERIC(4,1) NOT NULL,       -- 车厢温度 ℃
    recorded_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cold_readings_ship ON cold_chain_readings(shipment_id);

-- 4) 坏果赔付判定（一张工单对应一条判定结论，关闭前证据持续保留）
CREATE TABLE IF NOT EXISTS bad_fruit_claims (
    id                 BIGSERIAL PRIMARY KEY,
    incident_id        BIGINT NOT NULL UNIQUE REFERENCES incidents(id) ON DELETE CASCADE,
    project_id         BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    adoption_id        BIGINT REFERENCES adoptions(id) ON DELETE SET NULL,
    shipment_id        BIGINT REFERENCES shipments(id) ON DELETE SET NULL,
    batch_no           TEXT NOT NULL,
    -- 认养人反馈
    reported_bad_pct   NUMERIC(5,2) NOT NULL DEFAULT 0,   -- 开箱坏果比例 %
    total_count        INT NOT NULL DEFAULT 0,            -- 开箱总果数（盒数/枚数）
    bad_count          INT NOT NULL DEFAULT 0,            -- 坏果数量
    open_box_photos    JSONB NOT NULL DEFAULT '[]',       -- 开箱照片（赔付关闭前保留）
    signed_at          TIMESTAMPTZ,                        -- 签收时间
    opened_at          TIMESTAMPTZ,                        -- 开箱时间
    -- 平台关联的证据链（自动带出来源，客服可补充）
    harvest_time       TIMESTAMPTZ,                        -- 关联采摘时间
    harvest_task_id    BIGINT REFERENCES harvest_tasks(id) ON DELETE SET NULL,
    sorting_photos     JSONB NOT NULL DEFAULT '[]',        -- 关联分拣照片
    sorting_record_id  BIGINT REFERENCES sorting_records(id) ON DELETE SET NULL,
    cold_curve         JSONB NOT NULL DEFAULT '[]',        -- 关联冷链温度曲线（赔付关闭前保留）
    cold_max_c         NUMERIC(4,1),                        -- 冷链全程最高温
    cold_min_c         NUMERIC(4,1),                        -- 全程最低温
    cold_breach        BOOLEAN NOT NULL DEFAULT FALSE,      -- 是否存在脱冷（超温）
    cold_summary       TEXT NOT NULL DEFAULT '',
    -- 客服判定
    plan               TEXT NOT NULL DEFAULT 'pending' CHECK (
                         plan IN ('pending','reship','refund','logistics_liability')),
    liability_party    TEXT NOT NULL DEFAULT '' CHECK (liability_party IN ('','farm','logistics','shared')),
    bad_fruit_pct      NUMERIC(5,2) NOT NULL DEFAULT 0,    -- 核定坏果比例
    conclusion         TEXT NOT NULL DEFAULT '',            -- 客服结论（赔付关闭前保留）
    -- 责任与成本（物流追责时运费、补发成本分开计算）
    shipping_fee       NUMERIC(12,2) NOT NULL DEFAULT 0,   -- 原单运费
    freight_borne_by   TEXT NOT NULL DEFAULT '',            -- 运费承担方 logistics/farm/platform
    reship_cost        NUMERIC(12,2) NOT NULL DEFAULT 0,   -- 补发果品成本
    reship_freight     NUMERIC(12,2) NOT NULL DEFAULT 0,   -- 补发运费
    refund_amount      NUMERIC(12,2) NOT NULL DEFAULT 0,   -- 退款金额
    -- 同步状态
    archive_synced     BOOLEAN NOT NULL DEFAULT FALSE,      -- 赔付结论已同步认养档案
    logistics_synced   BOOLEAN NOT NULL DEFAULT FALSE,      -- 冷链责任/追责已同步物流端
    reship_synced      BOOLEAN NOT NULL DEFAULT FALSE,      -- 补发订单已同步物流端
    service_id         BIGINT REFERENCES users(id),
    closed_at          TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 5) 补发订单：赔付方案生成后同步到物流端的补发运单
CREATE TABLE IF NOT EXISTS reship_orders (
    id               BIGSERIAL PRIMARY KEY,
    claim_id         BIGINT NOT NULL REFERENCES bad_fruit_claims(id) ON DELETE CASCADE,
    project_id       BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    adoption_id      BIGINT REFERENCES adoptions(id) ON DELETE SET NULL,
    shipment_id      BIGINT REFERENCES shipments(id) ON DELETE SET NULL, -- 同步生成的补发运单
    batch_no         TEXT NOT NULL,
    reship_no        TEXT NOT NULL UNIQUE,
    spec             TEXT NOT NULL DEFAULT '',
    weight_kg        NUMERIC(10,2) NOT NULL DEFAULT 0,
    fruit_cost       NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 补发成本（果品）
    freight_cost     NUMERIC(12,2) NOT NULL DEFAULT 0,  -- 补发成本（运费，单独列）
    recipient_name   TEXT NOT NULL DEFAULT '',
    phone            TEXT NOT NULL DEFAULT '',
    address          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (
                       status IN ('pending','synced','shipped','delivered')),
    tracking_no      TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    synced_at        TIMESTAMPTZ,
    shipped_at       TIMESTAMPTZ,
    delivered_at     TIMESTAMPTZ
);

-- 6) 冷链责任通知单：责任判定同步给物流端
CREATE TABLE IF NOT EXISTS logistics_notices (
    id              BIGSERIAL PRIMARY KEY,
    claim_id        BIGINT NOT NULL REFERENCES bad_fruit_claims(id) ON DELETE CASCADE,
    project_id      BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    shipment_id     BIGINT REFERENCES shipments(id) ON DELETE SET NULL,
    batch_no        TEXT NOT NULL,
    tracking_no     TEXT NOT NULL DEFAULT '',
    notice_no       TEXT NOT NULL UNIQUE,
    kind            TEXT NOT NULL DEFAULT 'cold_chain_liability'
                    CHECK (kind IN ('cold_chain_liability','freight_claim','reship_dispatch')),
    cold_summary    TEXT NOT NULL DEFAULT '',
    freight_amount  NUMERIC(12,2) NOT NULL DEFAULT 0,   -- 向物流追责的运费金额
    conclusion      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','synced','acknowledged','closed')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    synced_at       TIMESTAMPTZ,
    acknowledged_at TIMESTAMPTZ
);

-- 7) 认养档案售后记录：赔付结论同步到认养档案（与我的认养对应）
CREATE TABLE IF NOT EXISTS adoption_archive_notes (
    id          BIGSERIAL PRIMARY KEY,
    adoption_id BIGINT NOT NULL REFERENCES adoptions(id) ON DELETE CASCADE,
    project_id  BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    claim_id    BIGINT REFERENCES bad_fruit_claims(id) ON DELETE SET NULL,
    batch_no    TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'bad_fruit',
    summary     TEXT NOT NULL,
    detail      JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_archive_adoption ON adoption_archive_notes(adoption_id);

-- =====================================================================
-- 演示故事线：蓝莓批次 BERRY-2026-A 坏果较多赔付判定（幂等）
-- 陈曦订单 #4（3kg 礼盒，2026-06-26 签收，当日开箱反馈坏果 18%）
-- 关联：6月25日采摘、分拣照片、冷链曲线（分拨时段脱冷至 9.2℃）
-- 客服按比例判定物流追责：补发 0.5kg，运费与补发成本分开，
-- 结论同步认养档案，补发订单与冷链责任同步物流端。
-- =====================================================================

-- 采摘时间（蓝莓抽检/采收任务原种子未建 harvest_tasks，这里补一条已完成任务）
INSERT INTO harvest_tasks (id, project_id, batch_no, plot_id, picker_id, priority, planned_date,
    target_kg, actual_kg, bad_rate_pct, status, harvested_at) VALUES
 (10,2,'BERRY-2026-A',2,4,10,'2026-06-25',18.00,16.50,2.10,'done','2026-06-25 06:40:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('harvest_tasks_id_seq', GREATEST((SELECT MAX(id) FROM harvest_tasks), 10));

-- 分拣照片（对应陈曦礼盒的分拣批次）
INSERT INTO sorting_records (id, project_id, batch_no, packer_id, spec, weight_kg, box_count, photos) VALUES
 (10,2,'BERRY-2026-A',5,'优瑞卡鲜果6斤礼盒装（125g×24盒）',12.00,40,
  '[{"caption":"蓝莓分拣台果粉完整","url":"/static/demo/berry-sort-1.jpg"},{"caption":"装盒预冷前抽检","url":"/static/demo/berry-sort-2.jpg"}]')
ON CONFLICT (id) DO NOTHING;
SELECT setval('sorting_records_id_seq', GREATEST((SELECT MAX(id) FROM sorting_records), 10));

-- 陈曦蓝莓运单为 shipments #4：补冷链温度曲线（含脱冷段）
INSERT INTO cold_chain_readings (id, shipment_id, project_id, batch_no, point, temp_c, recorded_at) VALUES
 (1,4,2,'BERRY-2026-A','烟台仓装车预冷',2.1,'2026-06-25 08:30:00+08'),
 (2,4,2,'BERRY-2026-A','机场货站等候装机',4.8,'2026-06-25 12:10:00+08'),
 (3,4,2,'BERRY-2026-A','北京分拨中转（停电检修约70分钟）',9.2,'2026-06-26 05:40:00+08'),
 (4,4,2,'BERRY-2026-A','末端网点出库',6.5,'2026-06-26 08:20:00+08'),
 (5,4,2,'BERRY-2026-A','送达签收时回温',4.2,'2026-06-26 10:00:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('cold_chain_readings_id_seq', GREATEST((SELECT MAX(id) FROM cold_chain_readings), 5));

-- 坏果投诉工单（蓝莓，陈曦订单 #4）
INSERT INTO incidents (id, project_id, batch_no, adoption_id, kind, title, description, status, reporter_id, created_at, resolved_at) VALUES
 (10,2,'BERRY-2026-A',4,'bad_fruit','蓝莓开箱坏果较多（约18%）要求赔付',
  '陈曦签收当日开箱反馈：一盒 125g 中霉变/压烂约 18%，果粉有融化痕迹，要求按坏果包赔处理。',
  'compensated',3,'2026-06-26 15:20:00+08','2026-06-27 16:00:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('incidents_id_seq', GREATEST((SELECT MAX(id) FROM incidents), 10));

INSERT INTO incident_events (id, incident_id, actor_id, actor_role, action, note, created_at) VALUES
 (100,10,3,'adopter','提交开箱照片与坏果比例','125g 装共 24 小盒，其中 4 盒上部约 1/4 霉变压烂，核定坏果比例约 18%，附 3 张开箱照片。','2026-06-26 15:20:00+08'),
 (101,10,4,'picker','关联采摘时间','该批为 6 月 25 日 06:40 晨采，采后 2 小时内入预冷库，采摘环节无异常。','2026-06-26 16:00:00+08'),
 (102,10,5,'packer','关联分拣照片','调出该批分拣台与装盒预冷照片，果粉完整、无挤压，包装环节无异常。','2026-06-26 16:20:00+08'),
 (103,10,6,'logistics','同步冷链温度曲线','曲线显示北京分拨中转时段最高 9.2℃、持续约 70 分钟，超出生鲜冷链 0-6℃ 标准，承认中转脱冷。','2026-06-27 09:30:00+08'),
 (104,10,7,'service','按坏果比例生成赔付方案','核定坏果比例 18%（≥10% 且为脱冷导致），生成物流追责方案：补发 0.5kg 鲜果，运费 18 元由物流承担、补发果品成本 26 元由物流承担，另退坏果对应货款 15 元。','2026-06-27 11:00:00+08'),
 (105,10,6,'logistics','冷链责任确认','确认分拨停电脱冷责任，接受运费与补发成本分开计责，已安排整改备用电源。','2026-06-27 14:00:00+08'),
 (106,10,7,'service','关闭判定并同步','开箱照片、冷链曲线、客服结论归档保留；结论同步认养档案；补发订单 RSHIP-BERRY-2026-A-010 与冷链责任通知已同步物流端。','2026-06-27 16:00:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('incident_events_id_seq', GREATEST((SELECT MAX(id) FROM incident_events), 106));

INSERT INTO bad_fruit_claims (id, incident_id, project_id, adoption_id, shipment_id, batch_no,
    reported_bad_pct, total_count, bad_count, open_box_photos, signed_at, opened_at,
    harvest_time, harvest_task_id, sorting_photos, sorting_record_id, cold_curve,
    cold_max_c, cold_min_c, cold_breach, cold_summary,
    plan, liability_party, bad_fruit_pct, conclusion,
    shipping_fee, freight_borne_by, reship_cost, reship_freight, refund_amount,
    archive_synced, logistics_synced, reship_synced, service_id, closed_at) VALUES
 (10,10,2,4,4,'BERRY-2026-A',
  18.00,24,4,
  '[{"caption":"开箱整体：4盒上部霉变","url":"/static/demo/berry-open-1.jpg"},{"caption":"霉变果特写","url":"/static/demo/berry-open-2.jpg"},{"caption":"果粉融化出水痕迹","url":"/static/demo/berry-open-3.jpg"}]',
  '2026-06-26 10:00:00+08','2026-06-26 15:00:00+08',
  '2026-06-25 06:40:00+08',10,
  '[{"caption":"蓝莓分拣台果粉完整","url":"/static/demo/berry-sort-1.jpg"},{"caption":"装盒预冷前抽检","url":"/static/demo/berry-sort-2.jpg"}]',10,
  '[{"point":"烟台仓装车预冷","temp_c":2.1,"recorded_at":"2026-06-25T08:30:00+08:00"},{"point":"机场货站等候装机","temp_c":4.8,"recorded_at":"2026-06-25T12:10:00+08:00"},{"point":"北京分拨中转（停电检修约70分钟）","temp_c":9.2,"recorded_at":"2026-06-26T05:40:00+08:00"},{"point":"末端网点出库","temp_c":6.5,"recorded_at":"2026-06-26T08:20:00+08:00"},{"point":"送达签收时回温","temp_c":4.2,"recorded_at":"2026-06-26T10:00:00+08:00"}]',
  9.2,2.1,TRUE,'北京分拨中转停电检修约70分钟，车厢温度升至 9.2℃，超出 0-6℃ 生鲜冷链标准；其余节点达标。',
  'logistics_liability','logistics',18.00,
  '核定坏果比例 18%：开箱照片与分拣照片比对，包装/采摘环节无异常；冷链曲线证实中转脱冷，责任在物流。按规则生成物流追责方案：补发鲜果 0.5kg（果品成本 ¥26.00）+ 退还坏果对应货款 ¥15.00；原单运费 ¥18.00 免收由物流承担，补发运费 ¥12.00 由物流承担，运费与补发成本分开计责。开箱照片、冷链曲线与本结论在赔付关闭后归档保留。',
  18.00,'logistics',26.00,12.00,15.00,
  TRUE,TRUE,TRUE,7,'2026-06-27 16:00:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('bad_fruit_claims_id_seq', GREATEST((SELECT MAX(id) FROM bad_fruit_claims), 10));

-- 补发运单：补发订单同步物流端后，在发货表里生成第 2 批运单（已送达）
INSERT INTO shipments (id, project_id, adoption_id, batch_no, seq, plan_date, pack_spec, cold_route,
    weight_kg, status, logistics_company, tracking_no, bad_fruit_rate, operator_id, shipped_at, delivered_at) VALUES
 (10,2,4,'BERRY-2026-A',2,'2026-06-28','坏果补发：优瑞卡鲜果0.5kg（4小盒×125g，加厚泡沫箱+2冰袋）',
    '烟台-北京（生鲜专机+冷链，分拨优先中转）',0.50,'delivered',
    '顺丰冷运','SFFRESH2026062801',0.00,6,'2026-06-28 08:00:00+08','2026-06-29 09:30:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('shipments_id_seq', GREATEST((SELECT MAX(id) FROM shipments), 10));

INSERT INTO reship_orders (id, claim_id, project_id, adoption_id, shipment_id, batch_no, reship_no,
    spec, weight_kg, fruit_cost, freight_cost, recipient_name, phone, address,
    status, tracking_no, synced_at, shipped_at, delivered_at) VALUES
 (10,10,2,4,10,'BERRY-2026-A','RSHIP-BERRY-2026-A-010',
  '优瑞卡鲜果补发 0.5kg（4小盒×125g，加厚泡沫箱+2冰袋）',0.50,26.00,12.00,
  '陈曦','13900000002','北京市朝阳区建国路88号SOHO现代城A座1203',
  'delivered','SFFRESH2026062801','2026-06-27 16:05:00+08','2026-06-28 08:00:00+08','2026-06-29 09:30:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('reship_orders_id_seq', GREATEST((SELECT MAX(id) FROM reship_orders), 10));

INSERT INTO logistics_notices (id, claim_id, project_id, shipment_id, batch_no, tracking_no, notice_no,
    kind, cold_summary, freight_amount, conclusion, status, synced_at, acknowledged_at) VALUES
 (10,10,2,4,'BERRY-2026-A','SFFRESH2026062501','LTC-BERRY-2026-A-010',
  'cold_chain_liability','北京分拨中转停电约70分钟，车厢最高 9.2℃，超出 0-6℃ 标准。',
  30.00,'冷链脱冷致蓝莓坏果率 18%：原单运费 ¥18.00 免收 + 补发运费 ¥12.00，合计运费追责 ¥30.00；补发果品成本 ¥26.00 另行结算。',
  'acknowledged','2026-06-27 16:05:00+08','2026-06-27 14:00:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('logistics_notices_id_seq', GREATEST((SELECT MAX(id) FROM logistics_notices), 10));

INSERT INTO adoption_archive_notes (id, adoption_id, project_id, claim_id, batch_no, kind, summary, detail) VALUES
 (10,4,2,10,'BERRY-2026-A','bad_fruit',
  '2026-06-26 蓝莓开箱坏果 18%，经分拣照片与冷链曲线核验为分拨脱冷（物流责任）：补发鲜果 0.5kg 已送达、退货款 ¥15.00、运费追责 ¥30.00（运费与补发成本分开），客户回访满意。',
  '{"plan":"logistics_liability","liability_party":"logistics","bad_fruit_pct":18,"refund_amount":15,"reship_weight_kg":0.5,"shipping_fee":18,"reship_freight":12,"reship_cost":26,"reship_no":"RSHIP-BERRY-2026-A-010","notice_no":"LTC-BERRY-2026-A-010"}')
ON CONFLICT (id) DO NOTHING;
SELECT setval('adoption_archive_notes_id_seq', GREATEST((SELECT MAX(id) FROM adoption_archive_notes), 10));

-- 坏果对应赔付记录（退款 15 元 + 补发折值 26 元，权益兑现按既有折算口径自动计入）
INSERT INTO compensations (id, incident_id, adoption_id, project_id, amount, kind, status, note, created_at, paid_at) VALUES
 (10,10,4,2,15.00,'cash','paid','坏果 18% 对应货款退款（物流责任，物流追责分开计）','2026-06-27 16:10:00+08','2026-06-27 18:00:00+08'),
 (11,10,4,2,26.00,'reship','paid','补发优瑞卡鲜果 0.5kg（果品成本26元，运费12元由物流承担，见补发单 RSHIP-BERRY-2026-A-010）','2026-06-27 16:10:00+08','2026-06-29 09:30:00+08')
ON CONFLICT (id) DO NOTHING;
SELECT setval('compensations_id_seq', GREATEST((SELECT MAX(id) FROM compensations), 11));

-- 物流追责相关地块成本（运费赔付与补发果品分列）
INSERT INTO plot_costs (id, plot_id, project_id, season, cost_type, item, amount) VALUES
 (20,2,2,'2026','logistics','蓝莓坏果物流追责：原单运费免收18元（向物流追回）',18.00),
 (21,2,2,'2026','compensation','蓝莓坏果补发0.5kg果品成本（向物流追回26元）',26.00)
ON CONFLICT (id) DO NOTHING;
SELECT setval('plot_costs_id_seq', GREATEST((SELECT MAX(id) FROM plot_costs), 21));

-- 同步更新蓝莓季结算（原 002 种子为零赔付，坏果故事线补充后重算发货/赔付）
UPDATE settlements SET
    total_shipped_kg = total_shipped_kg + 0.50,
    total_compensation = 41.00,
    summary = '本季蓝莓全园 420kg，发货 414.5kg（含坏果补发 0.5kg；6kg 留园体验消耗），常规批次零坏果；'
              || '陈曦批次分拨脱冷致坏果率 18%，按坏果赔付判定走物流追责：退款 15 元、补发折值 26 元、'
              || '运费追责 30 元（运费与补发成本分开），全部由物流承担，客户回访满意。'
              || '建议下一季扩容至 80 垄、推出亲子采摘日，并在合同中明确分拨停电追责条款。'
WHERE project_id = 2;
