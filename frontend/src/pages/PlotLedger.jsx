import React, { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import {
  get, fmtMoney, fmtKg, fmtDate, KIND_EMOJI, INCIDENT_LABEL, INCIDENT_COLOR,
  CLAIM_SCHEME_LABEL, CLAIM_SCHEME_CLS, CLAIM_STATUS_LABEL, CLAIM_STATUS_CLS,
} from '../api'
import { Stat, Tag, Empty, ShipFlow } from '../components/ui'

const COST_LABEL = {
  input: '🌱 农事投入', disaster: '🌩 防灾/灾害', harvest: '🧺 采摘',
  sorting: '📦 分拣包装', logistics: '🚚 冷链物流', compensation: '💸 赔付',
}
const LOG_LABEL = { farming: '农事', fertilizer: '施肥', pesticide: '用药', disaster: '灾害', photo: '照片', other: '其他' }

export default function PlotLedger() {
  const { id } = useParams()
  const [d, setD] = useState(null)
  useEffect(() => { get(`/plots/${id}/ledger`).then(setD) }, [id])
  if (!d) return <Empty text="加载中…" />
  const p = d.plot
  const cropEmoji = p.crop
  return (
    <>
      <Link to="/farm" className="small">← 返回农场工作台</Link>
      <div className="card mt8">
        <h1>🌍 {p.name} · 完整地块账</h1>
        <div className="muted small">
          {KIND_EMOJI[cropEmoji] || '🌱'} {p.crop} · {p.area_mu} 亩 · {p.location} · {d.season} 季
        </div>
        <div className="grid grid-4 mt16">
          <Stat label="本季总成本" value={fmtMoney(d.total_cost)} cls="red" />
          <Stat label="累计发货" value={fmtKg(d.total_shipped)} cls="blue" />
          <Stat label="平均坏果率" value={Number(d.avg_bad_rate || 0).toFixed(2) + '%'} cls="amber" />
          <Stat label="平均客户满意度" value={Number(d.avg_satisfaction || 0).toFixed(2) + ' / 5'} />
        </div>
      </div>

      <div className="grid grid-2">
        <div className="card">
          <div className="section-title">💰 成本构成（农事投入→灾害→采摘→分拣→物流→赔付）</div>
          <table>
            <thead><tr><th>类别</th><th>笔数</th><th>金额</th><th>占比</th></tr></thead>
            <tbody>
              {d.costs.map((c) => (
                <tr key={c.cost_type}>
                  <td>{COST_LABEL[c.cost_type] || c.cost_type}</td>
                  <td>{c.n}</td>
                  <td>{fmtMoney(c.amount)}</td>
                  <td>{d.total_cost > 0 ? ((c.amount / d.total_cost) * 100).toFixed(1) : 0}%</td>
                </tr>
              ))}
              {d.costs.length === 0 && <tr><td colSpan="4"><Empty text="暂无成本记录" /></td></tr>}
            </tbody>
          </table>
        </div>

        <div className="card">
          <div className="section-title">🌩 天气灾害记录</div>
          {d.disasters.length === 0 ? <Empty text="无灾害记录" /> : d.disasters.map((x) => (
            <div key={x.id} className="tl-item disaster">
              <strong>{x.title}</strong> <Tag cls="red">{x.disaster_kind} · 影响 {x.yield_impact_pct}%</Tag>
              <div className="small">{x.content}</div>
              <div className="tl-time">{fmtDate(x.created_at)}</div>
            </div>
          ))}
        </div>
      </div>

      <div className="card">
        <div className="section-title">📈 成熟 → 采摘 → 分拣 → 发货 全链路</div>
        <h3>成熟度抽检</h3>
        <table>
          <thead><tr><th>时间</th><th>糖度</th><th>成熟度</th><th>预估产量</th><th>坏果率</th><th>分级</th></tr></thead>
          <tbody>
            {d.maturity_checks.map((c) => (
              <tr key={c.id}><td>{fmtDate(c.checked_at)}</td><td>{c.brix ?? '—'}</td>
                <td>{c.maturity_pct}%</td><td>{fmtKg(c.est_yield_kg)}</td><td>{c.bad_rate_pct}%</td>
                <td className="small">{Object.entries(c.grade_dist || {}).map(([k, v]) => `${k}:${v}%`).join(' ')}</td></tr>
            ))}
            {d.maturity_checks.length === 0 && <tr><td colSpan="6"><Empty text="暂无抽检" /></td></tr>}
          </tbody>
        </table>

        <h3 className="mt16">采摘（实际产量 / 坏果率）</h3>
        <table>
          <thead><tr><th>日期</th><th>采摘工</th><th>目标</th><th>实际</th><th>坏果率</th><th>状态</th></tr></thead>
          <tbody>
            {d.harvest_tasks.map((h) => (
              <tr key={h.id}><td>{h.planned_date}</td><td>{h.picker_name || '—'}</td>
                <td>{fmtKg(h.target_kg)}</td><td>{h.actual_kg != null ? fmtKg(h.actual_kg) : '—'}</td>
                <td>{h.bad_rate_pct != null ? h.bad_rate_pct + '%' : '—'}</td>
                <td>{h.status === 'done' ? '已完成' : '计划中'}</td></tr>
            ))}
            {d.harvest_tasks.length === 0 && <tr><td colSpan="6"><Empty text="暂无采摘" /></td></tr>}
          </tbody>
        </table>

        <h3 className="mt16">分拣包装</h3>
        <table>
          <thead><tr><th>规格</th><th>重量</th><th>箱数</th><th>包装员</th></tr></thead>
          <tbody>
            {d.sorting.map((s) => <tr key={s.id}><td>{s.spec}</td><td>{fmtKg(s.weight_kg)}</td><td>{s.box_count}</td><td>{s.packer_name}</td></tr>)}
            {d.sorting.length === 0 && <tr><td colSpan="4"><Empty text="暂无分拣" /></td></tr>}
          </tbody>
        </table>

        <h3 className="mt16">发货（冷链路线 / 坏果率）</h3>
        <table>
          <thead><tr><th>批次</th><th>计划</th><th>规格</th><th>重量</th><th>冷链路线</th><th>状态</th><th>坏果率</th></tr></thead>
          <tbody>
            {d.shipments.map((s) => (
              <tr key={s.id}><td>第{s.seq}批{s.is_reship ? ' 📦补发' : ''}</td><td>{s.plan_date}</td><td className="small">{s.pack_spec}</td>
                <td>{s.weight_kg != null ? fmtKg(s.weight_kg) : '—'}</td><td className="small muted">{s.cold_route}</td>
                <td><ShipFlow status={s.status} /></td>
                <td>{s.status === 'planned' ? '—' : s.bad_fruit_rate + '%'}</td></tr>
            ))}
            {d.shipments.length === 0 && <tr><td colSpan="7"><Empty text="暂无发货" /></td></tr>}
          </tbody>
        </table>
      </div>

      <div className="grid grid-2">
        <div className="card">
          <div className="section-title">🧾 售后工单与赔付</div>
          {d.incidents.length === 0 ? <Empty text="无异常工单" /> : (
            <table>
              <tbody>
                {d.incidents.map((i) => (
                  <tr key={i.id}>
                    <td><span className="tag" style={{ background: '#fef2f2', color: INCIDENT_COLOR[i.kind], borderColor: '#fecaca' }}>
                      {INCIDENT_LABEL[i.kind]}</span><div className="small">{i.title}</div></td>
                    <td><Tag cls={i.status === 'compensated' ? 'green' : i.status === 'resolved' ? 'gray' : 'red'}>
                      {i.status === 'open' ? '待处理' : i.status === 'processing' ? '处理中' : i.status === 'resolved' ? '已解决' : '已赔付'}</Tag></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <h3 className="mt16">赔付明细</h3>
          <table>
            <tbody>
              {d.compensations.map((c) => (
                <tr key={c.id}><td className="small">{c.note}</td><td>{fmtMoney(c.amount)}</td><td>{c.status === 'paid' ? '已付' : '待付'}</td></tr>
              ))}
              {d.compensations.length === 0 && <tr><td colSpan="3"><Empty text="无赔付" /></td></tr>}
            </tbody>
          </table>

          <h3 className="mt16">🫐 坏果赔付判定（证据保留 / 运费与补发成本分开）</h3>
          {(d.bad_fruit_claims || []).length === 0 ? <Empty text="无坏果赔付单" /> : (
            <table>
              <thead><tr><th>坏果率</th><th>方案</th><th>退款</th><th>补发成本</th><th>运费</th><th>冷链责任</th><th>状态</th></tr></thead>
              <tbody>
                {d.bad_fruit_claims.map((c) => (
                  <tr key={c.id}>
                    <td><Tag cls="red">{c.bad_rate_pct}%</Tag></td>
                    <td>{c.scheme ? <Tag cls={CLAIM_SCHEME_CLS[c.scheme]}>{CLAIM_SCHEME_LABEL[c.scheme]}</Tag> : '—'}</td>
                    <td>{fmtMoney(c.refund_amount)}</td>
                    <td>{fmtMoney(c.reship_cost)}</td>
                    <td>{fmtMoney(c.freight_cost)}</td>
                    <td>{c.cold_chain_responsible ? <Tag cls="teal">已同步物流</Tag> : '否'}</td>
                    <td><Tag cls={CLAIM_STATUS_CLS[c.status]}>{CLAIM_STATUS_LABEL[c.status]}</Tag></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        <div className="card">
          <div className="section-title">⭐ 客户回访与满意度沉淀</div>
          {d.callbacks.length === 0 ? <Empty text="暂无回访" /> : d.callbacks.map((c) => (
            <div key={c.id} style={{ borderBottom: '1px solid var(--line)', paddingBottom: 10, marginBottom: 10 }}>
              <div>满意度 {'★'.repeat(c.satisfaction)}{'☆'.repeat(5 - c.satisfaction)}
                <span className="small muted">（品质 {c.quality_score} · 配送 {c.delivery_score}）</span>
                {c.would_renew && <Tag cls="green">愿续认</Tag>}
              </div>
              <div className="small">“{c.comment}”</div>
              <div className="small muted">下一季建议：{c.renew_suggestion}</div>
            </div>
          ))}
        </div>
      </div>

      <div className="card">
        <div className="section-title">🌳 地块上的认养项目</div>
        <div className="row">
          {d.projects.map((pr) => (
            <Link key={pr.id} to={`/projects/${pr.id}`} className="tag" style={{ fontSize: 13, padding: '6px 12px' }}>
              {pr.batch_no} · {pr.title} · {pr.sold_shares}/{pr.total_shares} 份
            </Link>
          ))}
        </div>
      </div>
    </>
  )
}
