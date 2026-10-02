import React, { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  get, post, INCIDENT_LABEL, INCIDENT_COLOR, ROLE_LABEL,
  CLAIM_PLAN_LABEL, CLAIM_PLAN_HINT, LIABILITY_LABEL, RESHIP_STATUS_LABEL, NOTICE_STATUS_LABEL,
  fmtMoney, fmtDate,
} from '../api'
import { Tag, Modal, Empty, useToast } from '../components/ui'
import { useAuth } from '../auth'

const STATUS = { open: '待处理', processing: '处理中', resolved: '已解决', compensated: '已赔付' }
const STATUS_CLS = { open: 'red', processing: 'amber', resolved: 'gray', compensated: 'green' }
const COMP_LABEL = { cash: '现金赔付', reship: '补发', insurance: '保险赔付', credit: '下季抵扣' }
const ROLE_COLOR = {
  farmer: '#2f7d32', adopter: '#0369a1', picker: '#b45309',
  packer: '#7c3aed', logistics: '#be185d', service: '#0f766e', admin: '#374151',
}

export default function Incidents() {
  const [list, setList] = useState([])
  const [detail, setDetail] = useState(null)
  const [showNew, setShowNew] = useState(false)
  const [sp] = useSearchParams()
  const load = useCallback(() => get('/incidents').then(setList), [])
  useEffect(() => {
    load()
    const f = sp.get('focus')
    if (f) get(`/incidents/${f}`).then(setDetail).catch(() => {})
  }, [])

  return (
    <>
      <div className="row">
        <h1 style={{ margin: 0 }}>🧾 异常工单 · 同一认养批次协同处理</h1>
        <span className="spacer" />
        <button onClick={() => setShowNew(true)}>+ 提交工单</button>
      </div>
      <p className="muted">冰雹减产、成熟延后、临时改地址、果品不均、坏果投诉、分批争议、权益与产量不一致——农场、认养人、采摘工、包装员、物流、客服的处理结果全部串联在同一批次。坏果投诉可关联采摘时间、分拣照片、冷链温度曲线、签收/开箱时间与开箱照片，客服按坏果比例生成补发/退款/物流追责方案。</p>

      <div className="grid grid-2">
        {list.map((i) => (
          <div key={i.id} className="card" style={{ cursor: 'pointer' }} onClick={() => get(`/incidents/${i.id}`).then(setDetail)}>
            <div className="row" style={{ justifyContent: 'space-between' }}>
              <span className="tag" style={{ background: '#fef2f2', color: INCIDENT_COLOR[i.kind], borderColor: '#fecaca' }}>
                {INCIDENT_LABEL[i.kind]}
              </span>
              <Tag cls={STATUS_CLS[i.status]}>{STATUS[i.status]}</Tag>
            </div>
            <h3 className="mt8">{i.title}</h3>
            <div className="small muted">{i.description}</div>
            <div className="small muted mt8">{i.project_title} · {i.batch_no} · {i.reporter_name} · {fmtDate(i.created_at)}</div>
          </div>
        ))}
        {list.length === 0 && <Empty />}
      </div>

      {detail && <IncidentDetail d={detail} onClose={() => setDetail(null)} onChanged={() => { get(`/incidents/${detail.incident.id}`).then(setDetail); load() }} />}
      {showNew && <NewIncident onClose={() => setShowNew(false)} onDone={() => { setShowNew(false); load() }} />}
    </>
  )
}

function IncidentDetail({ d, onClose, onChanged }) {
  const { user } = useAuth()
  const toast = useToast()
  const inc = d.incident
  const [act, setAct] = useState('')
  const [note, setNote] = useState('')
  const [showComp, setShowComp] = useState(false)
  const canService = user && ['service', 'admin', 'farmer'].includes(user.role)

  const addEvent = async (status) => {
    if (!act) return toast('请填写处理动作', true)
    try {
      await post(`/incidents/${inc.id}/events`, { action: act, note, ...(status ? { status } : {}) })
      setAct(''); setNote(''); toast('已记录'); onChanged()
    } catch (e) { toast(e.message, true) }
  }

  return (
    <Modal title={`工单 #${inc.id} · ${inc.title}`} onClose={onClose} wide>
      <div className="row">
        <span className="tag" style={{ background: '#fef2f2', color: INCIDENT_COLOR[inc.kind], borderColor: '#fecaca' }}>
          {INCIDENT_LABEL[inc.kind]}
        </span>
        <Tag cls={STATUS_CLS[inc.status]}>{STATUS[inc.status]}</Tag>
        <span className="small muted">{inc.project_title} · {inc.batch_no}</span>
      </div>
      <p className="mt8">{inc.description}</p>

      {inc.kind === 'bad_fruit' && (
        <BadFruitPanel d={d} onChanged={onChanged} />
      )}

      <div className="section-title mt16">处理流水（按时间串联各角色）</div>
      <div className="timeline">
        {d.events.map((e) => (
          <div key={e.id} className="tl-item">
            <div className="row" style={{ justifyContent: 'space-between' }}>
              <strong>{e.action}</strong>
              <Tag cls="gray">
                <span style={{ color: ROLE_COLOR[e.actor_role] }}>●</span> {e.actor_name || ROLE_LABEL[e.actor_role]}（{ROLE_LABEL[e.actor_role]}）
              </Tag>
            </div>
            {e.note && <div className="small">{e.note}</div>}
            <div className="tl-time">{fmtDate(e.created_at)}</div>
          </div>
        ))}
      </div>

      {d.compensations?.length > 0 && (
        <>
          <div className="section-title mt16">赔付记录</div>
          <table>
            <thead><tr><th>方式</th><th>金额</th><th>说明</th><th>状态</th><th>时间</th></tr></thead>
            <tbody>
              {d.compensations.map((c) => (
                <tr key={c.id}><td><Tag cls="teal">{COMP_LABEL[c.kind]}</Tag></td><td>{fmtMoney(c.amount)}</td>
                  <td className="small">{c.note}</td><td>{c.status === 'paid' ? '已到账' : '待发放'}</td><td className="small">{fmtDate(c.paid_at)}</td></tr>
              ))}
            </tbody>
          </table>
        </>
      )}

      <div className="card mt16" style={{ background: '#fafbf8' }}>
        <h3>追加处理结果（当前身份：{ROLE_LABEL[user.role]}）</h3>
        <label>处理动作</label>
        <input value={act} onChange={(e) => setAct(e.target.value)}
               placeholder="如：核实情况 / 补发 / 更新冷链路线 / 包装整改 / 回访确认" />
        <label>说明</label>
        <textarea rows="2" value={note} onChange={(e) => setNote(e.target.value)} />
        <div className="row mt8">
          <button className="sm" onClick={() => addEvent('')}>仅记录</button>
          <button className="sm ghost" onClick={() => addEvent('processing')}>标记处理中</button>
          <button className="sm ghost" onClick={() => addEvent('resolved')}>标记已解决</button>
          {canService && inc.status !== 'compensated' && (
            <button className="sm amber" onClick={() => setShowComp(true)}>登记赔付并结案</button>
          )}
        </div>
      </div>

      {showComp && <CompModal incidentId={inc.id} onClose={() => setShowComp(false)} onDone={() => { setShowComp(false); onChanged() }} />}
    </Modal>
  )
}

// ---------------- 坏果赔付判定 ----------------

function BadFruitPanel({ d, onChanged }) {
  const { user } = useAuth()
  const toast = useToast()
  const inc = d.incident
  const claim = d.bad_fruit_claim
  const [mode, setMode] = useState(null) // evidence / decide / close
  const canService = ['service', 'admin'].includes(user.role)
  const canCreate = ['service', 'admin', 'farmer'].includes(user.role)
  const isLogistics = user.role === 'logistics'

  const createClaim = async () => {
    try {
      await post('/bad-fruit-claims', { incident_id: inc.id })
      toast('已建立坏果赔付判定，采摘时间/分拣照片/冷链曲线已自动关联')
      onChanged()
    } catch (e) { toast(e.message, true) }
  }

  if (!claim) {
    return (
      <div className="card mt16" style={{ background: '#fff7ed', border: '1px dashed #fdba74' }}>
        <div className="section-title">🫐 坏果赔付判定</div>
        <p className="small muted">建立判定后，平台将自动关联该批次的<b>采摘时间、分拣照片、冷链温度曲线</b>，并由客服结合签收/开箱信息按坏果比例生成方案。</p>
        {canCreate && inc.status !== 'compensated' && (
          <button className="sm amber" onClick={createClaim}>🔎 建立坏果赔付判定（自动关联证据链）</button>
        )}
      </div>
    )
  }

  const closed = !!claim.closed_at
  const curve = d.cold_chain_curve || []

  return (
    <div className="card mt16" style={{ background: '#fff', border: '1px solid #e5e7eb' }}>
      <div className="row">
        <div className="section-title" style={{ margin: 0 }}>🫐 坏果赔付判定 #{claim.id}</div>
        <Tag cls={claim.plan === 'pending' ? 'amber' : 'teal'}>{CLAIM_PLAN_LABEL[claim.plan]}</Tag>
        {claim.liability_party && <Tag cls={claim.liability_party === 'logistics' ? 'red' : 'purple'}>{LIABILITY_LABEL[claim.liability_party]}</Tag>}
        {closed ? <Tag cls="green">已关闭 · 证据归档保留</Tag> : <Tag cls="amber">判定中</Tag>}
        <span className="spacer" />
        {!closed && (user.role === 'adopter' || canService) && (
          <button className="sm ghost" onClick={() => setMode('evidence')}>📷 补充开箱证据</button>
        )}
        {!closed && canService && (
          <>
            <button className="sm" onClick={() => setMode('decide')}>⚖️ {claim.plan === 'pending' ? '生成赔付方案' : '修改判定方案'}</button>
            <button className="sm amber" onClick={() => setMode('close')}>🔒 关闭判定并三向同步</button>
          </>
        )}
      </div>

      {/* 关联证据链：采摘时间 / 分拣照片 / 冷链曲线 / 签收开箱 */}
      <h4 className="mt16">🔗 平台关联证据链</h4>
      <div className="grid grid-4">
        <div className="stat">
          <div className="n" style={{ fontSize: 14 }}>{fmtDate(claim.harvest_time)}</div>
          <div className="l">采摘时间{claim.harvest_task_id ? `（任务#${claim.harvest_task_id}）` : ''}</div>
        </div>
        <div className="stat">
          <div className="n" style={{ fontSize: 14 }}>{(claim.sorting_photos || []).length} 张</div>
          <div className="l">分拣照片{claim.sorting_record_id ? `（记录#${claim.sorting_record_id}）` : ''}</div>
        </div>
        <div className="stat">
          <div className="n" style={{ fontSize: 14, color: claim.cold_breach ? 'var(--red)' : undefined }}>
            {claim.cold_min_c ?? '—'}~{claim.cold_max_c ?? '—'}℃
          </div>
          <div className="l">{claim.cold_breach ? '⚠️ 冷链脱冷' : '冷链达标'}</div>
        </div>
        <div className="stat">
          <div className="n" style={{ fontSize: 14 }}>{fmtDate(claim.signed_at)}</div>
          <div className="l">签收时间 · 开箱 {fmtDate(claim.opened_at)}</div>
        </div>
      </div>

      <div className="grid grid-2 mt16">
        <div>
          <strong className="small">📦 分拣照片</strong>
          {(claim.sorting_photos || []).length === 0 ? <Empty text="未关联分拣照片" /> : (
            <div className="small muted mt8">{(claim.sorting_photos || []).map((p) => '📷 ' + p.caption).join('；')}</div>
          )}
          <strong className="small mt16" style={{ display: 'block' }}>📸 开箱照片（关闭前保留）</strong>
          {(claim.open_box_photos || []).length === 0 ? <Empty text="认养人尚未上传开箱照片" /> : (
            <div className="small muted mt8">{(claim.open_box_photos || []).map((p) => '📷 ' + p.caption).join('；')}</div>
          )}
          <div className="small mt8">
            反馈坏果 <b style={{ color: 'var(--red)' }}>{claim.reported_bad_pct}%</b>
            {claim.total_count > 0 && <span className="muted">（{claim.bad_count}/{claim.total_count} 盒）</span>}
            {claim.bad_fruit_pct > 0 && <> · 客服核定 <b>{claim.bad_fruit_pct}%</b></>}
          </div>
        </div>
        <div>
          <strong className="small">🌡 冷链温度曲线（关闭前保留）</strong>
          {curve.length === 0 ? <Empty text="该运单暂无测温数据" /> : (
            <table className="mt8">
              <thead><tr><th>节点</th><th>时间</th><th>温度</th></tr></thead>
              <tbody>
                {curve.map((p) => {
                  const bad = p.temp_c > 6 || p.temp_c < 0
                  return (
                    <tr key={p.id}>
                      <td className="small">{p.point}</td>
                      <td className="small muted">{fmtDate(p.recorded_at)}</td>
                      <td style={{ color: bad ? 'var(--red)' : undefined, fontWeight: bad ? 700 : 400 }}>{p.temp_c}℃{bad && ' ⚠️'}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
          {claim.cold_summary && <div className="small muted mt8">{claim.cold_summary}</div>}
        </div>
      </div>

      {claim.conclusion && (
        <>
          <h4 className="mt16">⚖️ 客服结论（关闭前保留）</h4>
          <p className="small">{claim.conclusion}</p>
        </>
      )}

      {/* 成本分开计算：退款 / 补发果品 / 原单运费 / 补发运费 */}
      {(claim.refund_amount > 0 || claim.reship_cost > 0 || claim.shipping_fee > 0 || claim.reship_freight > 0) && (
        <>
          <h4 className="mt16">💰 赔付与追责金额（运费、补发成本分开计算）</h4>
          <table>
            <thead><tr><th>项目</th><th>金额</th><th>承担方</th></tr></thead>
            <tbody>
              {claim.refund_amount > 0 && <tr><td>坏果货款退款</td><td>{fmtMoney(claim.refund_amount)}</td><td><Tag cls="red">{LIABILITY_LABEL[claim.liability_party] || '—'}</Tag></td></tr>}
              {claim.reship_cost > 0 && <tr><td>补发果品成本</td><td>{fmtMoney(claim.reship_cost)}</td><td><Tag cls="amber">{LIABILITY_LABEL[claim.liability_party] || '—'}</Tag></td></tr>}
              {claim.shipping_fee > 0 && <tr><td>原单运费（免收/追责）</td><td>{fmtMoney(claim.shipping_fee)}</td><td><Tag cls="purple">{claim.freight_borne_by === 'logistics' ? '物流承担' : claim.freight_borne_by === 'farm' ? '农场承担' : claim.freight_borne_by === 'platform' ? '平台承担' : claim.freight_borne_by}</Tag></td></tr>}
              {claim.reship_freight > 0 && <tr><td>补发运费</td><td>{fmtMoney(claim.reship_freight)}</td><td><Tag cls="purple">{claim.freight_borne_by === 'logistics' ? '物流承担' : claim.freight_borne_by}</Tag></td></tr>}
            </tbody>
          </table>
        </>
      )}

      {/* 三向同步状态 */}
      <h4 className="mt16">🔄 同步状态</h4>
      <div className="row">
        <Tag cls={claim.archive_synced ? 'green' : 'gray'}>① 赔付结论{claim.archive_synced ? '已同步' : '未同步'}认养档案</Tag>
        <Tag cls={claim.reship_synced ? 'green' : 'gray'}>② 补发订单{claim.reship_synced ? '已同步' : '未同步'}物流端</Tag>
        <Tag cls={claim.logistics_synced ? 'green' : 'gray'}>③ 冷链责任{claim.logistics_synced ? '已同步' : '未同步'}物流端</Tag>
      </div>

      {(claim.reship_orders || []).length > 0 && (
        <table className="mt8">
          <thead><tr><th>补发单号</th><th>规格/重量</th><th>果品成本</th><th>补发运费</th><th>状态</th><th>运单号</th></tr></thead>
          <tbody>
            {claim.reship_orders.map((r) => (
              <tr key={r.id}>
                <td className="small">{r.reship_no}</td>
                <td className="small">{r.spec}<br /><span className="muted">{r.weight_kg}kg</span></td>
                <td>{fmtMoney(r.fruit_cost)}</td>
                <td>{fmtMoney(r.freight_cost)}</td>
                <td><Tag cls={r.status === 'delivered' ? 'green' : 'teal'}>{RESHIP_STATUS_LABEL[r.status]}</Tag></td>
                <td className="small">{r.tracking_no || '待物流发车回填'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {(claim.logistics_notices || []).length > 0 && (
        <table className="mt8">
          <thead><tr><th>通知单号</th><th>冷链情况</th><th>运费追责</th><th>状态</th><th>操作</th></tr></thead>
          <tbody>
            {claim.logistics_notices.map((n) => (
              <tr key={n.id}>
                <td className="small">{n.notice_no}<br /><span className="muted">原运单 {n.tracking_no || '—'}</span></td>
                <td className="small muted">{n.cold_summary}</td>
                <td><b>{fmtMoney(n.freight_amount)}</b></td>
                <td><Tag cls={n.status === 'acknowledged' ? 'green' : 'teal'}>{NOTICE_STATUS_LABEL[n.status]}</Tag></td>
                <td>
                  {isLogistics && n.status === 'synced' && (
                    <button className="sm ghost" onClick={async () => {
                      try { await post(`/logistics-notices/${n.id}/ack`, {}); toast('已确认冷链责任'); onChanged() }
                      catch (e) { toast(e.message, true) }
                    }}>确认责任</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {mode === 'evidence' && <EvidenceModal claim={claim} onClose={() => setMode(null)} onDone={() => { setMode(null); onChanged() }} />}
      {mode === 'decide' && <DecideModal claim={claim} onClose={() => setMode(null)} onDone={() => { setMode(null); onChanged() }} />}
      {mode === 'close' && <CloseClaimModal claim={claim} onClose={() => setMode(null)} onDone={() => { setMode(null); onChanged() }} />}
    </div>
  )
}

function EvidenceModal({ claim, onClose, onDone }) {
  const toast = useToast()
  const [f, setF] = useState({
    reported_bad_pct: claim.reported_bad_pct || 0,
    total_count: claim.total_count || 0,
    bad_count: claim.bad_count || 0,
    signed_at: claim.signed_at ? String(claim.signed_at).slice(0, 16) : '',
    opened_at: claim.opened_at ? String(claim.opened_at).slice(0, 16) : '',
    photosText: (claim.open_box_photos || []).map((p) => p.caption).join('\n'),
  })
  const submit = async () => {
    const photos = f.photosText.split('\n').map((s) => s.trim()).filter(Boolean).map((caption) => ({ caption }))
    try {
      await post(`/bad-fruit-claims/${claim.id}/evidence`, {
        reported_bad_pct: Number(f.reported_bad_pct), total_count: Number(f.total_count),
        bad_count: Number(f.bad_count), open_box_photos: photos,
        signed_at: f.signed_at ? new Date(f.signed_at).toISOString() : null,
        opened_at: f.opened_at ? new Date(f.opened_at).toISOString() : null,
      })
      toast('证据已补充，关闭前平台将持续保留开箱照片'); onDone()
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title="补充开箱证据" onClose={onClose}>
      <div className="grid grid-3">
        <div><label>坏果比例 %</label><input type="number" step="0.1" value={f.reported_bad_pct} onChange={(e) => setF({ ...f, reported_bad_pct: e.target.value })} /></div>
        <div><label>总盒/枚数</label><input type="number" value={f.total_count} onChange={(e) => setF({ ...f, total_count: e.target.value })} /></div>
        <div><label>坏果数</label><input type="number" value={f.bad_count} onChange={(e) => setF({ ...f, bad_count: e.target.value })} /></div>
      </div>
      <label>签收时间</label><input type="datetime-local" value={f.signed_at} onChange={(e) => setF({ ...f, signed_at: e.target.value })} />
      <label>开箱时间</label><input type="datetime-local" value={f.opened_at} onChange={(e) => setF({ ...f, opened_at: e.target.value })} />
      <label>开箱照片说明（每行一张，如：开箱整体4盒上部霉变）</label>
      <textarea rows="3" value={f.photosText} onChange={(e) => setF({ ...f, photosText: e.target.value })} />
      <button className="mt16" onClick={submit}>保存证据</button>
    </Modal>
  )
}

function DecideModal({ claim, onClose, onDone }) {
  const toast = useToast()
  const suggested = claim.cold_breach ? 'logistics_liability' : (Number(claim.reported_bad_pct) >= 10 ? 'refund' : 'reship')
  const [f, setF] = useState({
    bad_fruit_pct: claim.reported_bad_pct || 0,
    plan: claim.plan === 'pending' ? suggested : claim.plan,
    liability_party: claim.liability_party || (suggested === 'logistics_liability' ? 'logistics' : 'farm'),
    shipping_fee: claim.shipping_fee || 0,
    freight_borne_by: claim.freight_borne_by || (suggested === 'logistics_liability' ? 'logistics' : 'farm'),
    reship_cost: claim.reship_cost || 0,
    reship_freight: claim.reship_freight || 0,
    refund_amount: claim.refund_amount || 0,
    conclusion: claim.conclusion || '',
  })
  const set = (k, v) => setF({ ...f, [k]: v })
  const submit = async () => {
    if (!f.conclusion.trim()) return toast('客服结论必填（赔付关闭前需保留）', true)
    try {
      await post(`/bad-fruit-claims/${claim.id}/decide`, {
        ...f, bad_fruit_pct: Number(f.bad_fruit_pct), shipping_fee: Number(f.shipping_fee),
        reship_cost: Number(f.reship_cost), reship_freight: Number(f.reship_freight),
        refund_amount: Number(f.refund_amount),
      })
      toast('赔付方案已生成'); onDone()
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title="按坏果比例生成赔付方案" onClose={onClose} wide>
      <p className="muted small">
        规则建议：脱冷 → 物流追责；坏果≥10% → 退款；不足10% → 补发。
        本次建议：<b>{CLAIM_PLAN_LABEL[suggested]}</b>（{CLAIM_PLAN_HINT[suggested]}）
      </p>
      <div className="grid grid-2">
        <div><label>核定坏果比例 %</label><input type="number" step="0.1" value={f.bad_fruit_pct} onChange={(e) => set('bad_fruit_pct', e.target.value)} /></div>
        <div>
          <label>赔付方案</label>
          <select value={f.plan} onChange={(e) => set('plan', e.target.value)}>
            <option value="reship">补发</option>
            <option value="refund">退款</option>
            <option value="logistics_liability">物流追责（补发+追责）</option>
          </select>
        </div>
        <div>
          <label>责任方</label>
          <select value={f.liability_party} onChange={(e) => set('liability_party', e.target.value)}>
            <option value="farm">农场</option><option value="logistics">物流</option><option value="shared">共同责任</option>
          </select>
        </div>
        <div>
          <label>运费承担方</label>
          <select value={f.freight_borne_by} onChange={(e) => set('freight_borne_by', e.target.value)}>
            <option value="farm">农场</option><option value="logistics">物流</option><option value="platform">平台</option>
          </select>
        </div>
        <div><label>退款金额 ¥</label><input type="number" value={f.refund_amount} onChange={(e) => set('refund_amount', e.target.value)} /></div>
        <div><label>补发果品成本 ¥（与运费分开）</label><input type="number" value={f.reship_cost} onChange={(e) => set('reship_cost', e.target.value)} /></div>
        <div><label>原单运费 ¥（追责时免收）</label><input type="number" value={f.shipping_fee} onChange={(e) => set('shipping_fee', e.target.value)} /></div>
        <div><label>补发运费 ¥（单独列）</label><input type="number" value={f.reship_freight} onChange={(e) => set('reship_freight', e.target.value)} /></div>
      </div>
      <label>客服结论</label>
      <textarea rows="4" value={f.conclusion} onChange={(e) => set('conclusion', e.target.value)}
                placeholder="比对开箱照片/分拣照片/冷链曲线后的责任认定与处置依据……" />
      <button className="mt16" onClick={submit}>确认方案</button>
    </Modal>
  )
}

function CloseClaimModal({ claim, onClose, onDone }) {
  const toast = useToast()
  const needReship = claim.plan === 'reship' || claim.plan === 'logistics_liability'
  const [f, setF] = useState({ reship_weight_kg: 0, reship_spec: '' })
  const submit = async () => {
    try {
      await post(`/bad-fruit-claims/${claim.id}/close`, {
        reship_weight_kg: Number(f.reship_weight_kg), reship_spec: f.reship_spec,
      })
      toast('判定已关闭：证据归档、认养档案/补发/冷链责任已同步')
      onDone()
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title="关闭坏果赔付判定并三向同步" onClose={onClose}>
      <p className="small muted">
        关闭前系统将校验并保留：<b>开箱照片、冷链温度曲线、客服结论</b>。关闭后自动：
        ① 赔付结论同步认养档案；② 补发订单同步物流端（生成补发运单）；③ 冷链责任同步物流端。
      </p>
      {needReship && (
        <>
          <label>补发重量 kg</label>
          <input type="number" step="0.1" value={f.reship_weight_kg} onChange={(e) => setF({ ...f, reship_weight_kg: e.target.value })} />
          <label>补发规格（可留空自动生成）</label>
          <input value={f.reship_spec} onChange={(e) => setF({ ...f, reship_spec: e.target.value })} placeholder="如：优瑞卡鲜果0.5kg（4小盒+冰袋）" />
        </>
      )}
      <button className="mt16 amber" onClick={submit}>确认关闭并同步</button>
    </Modal>
  )
}

function CompModal({ incidentId, onClose, onDone }) {
  const toast = useToast()
  const [f, setF] = useState({ amount: 0, kind: 'cash', note: '' })
  const submit = async () => {
    if (f.amount <= 0) return toast('请填写赔付金额', true)
    try {
      await post(`/compensations/incident/${incidentId}`, f)
      toast('赔付已登记，工单自动转为已赔付'); onDone()
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title="登记赔付" onClose={onClose}>
      <label>赔付方式</label>
      <select value={f.kind} onChange={(e) => setF({ ...f, kind: e.target.value })}>
        <option value="cash">现金赔付</option>
        <option value="reship">补发果品</option>
        <option value="insurance">保险理赔款发放</option>
        <option value="credit">下一季认养抵扣</option>
      </select>
      <label>金额/折值 ¥</label>
      <input type="number" onChange={(e) => setF({ ...f, amount: Number(e.target.value) })} />
      <label>说明</label>
      <textarea rows="2" value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} />
      <button className="mt16" onClick={submit}>确认赔付</button>
    </Modal>
  )
}

function NewIncident({ onClose, onDone }) {
  const toast = useToast()
  const [projects, setProjects] = useState([])
  const [f, setF] = useState({ project_id: '', kind: 'bad_fruit', title: '', description: '', adoption_id: '' })
  useEffect(() => { get('/projects').then(setProjects) }, [])
  const submit = async () => {
    if (!f.project_id || !f.title) return toast('请选择项目并填写标题', true)
    try {
      await post('/incidents', {
        ...f,
        project_id: Number(f.project_id),
        adoption_id: f.adoption_id ? Number(f.adoption_id) : undefined,
      })
      toast('工单已提交'); onDone()
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title="提交异常工单" onClose={onClose}>
      <label>认养项目 / 批次</label>
      <select value={f.project_id} onChange={(e) => setF({ ...f, project_id: e.target.value })}>
        <option value="">请选择</option>
        {projects.map((p) => <option key={p.id} value={p.id}>{p.title}（{p.batch_no}）</option>)}
      </select>
      <label>问题类型</label>
      <select value={f.kind} onChange={(e) => setF({ ...f, kind: e.target.value })}>
        {Object.entries(INCIDENT_LABEL).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
      </select>
      <label>关联认养订单 ID（批次级问题可不填）</label>
      <input value={f.adoption_id} onChange={(e) => setF({ ...f, adoption_id: e.target.value })} />
      <label>标题</label><input value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} />
      <label>详细描述</label><textarea rows="3" value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} />
      <button className="mt16" onClick={submit}>提交</button>
    </Modal>
  )
}
