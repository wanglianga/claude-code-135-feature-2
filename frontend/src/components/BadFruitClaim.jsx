import React, { useEffect, useState } from 'react'
import { get, post, CLAIM_SCHEME_LABEL, CLAIM_SCHEME_CLS, CLAIM_STATUS_LABEL, CLAIM_STATUS_CLS, fmtMoney, fmtDate } from '../api'
import { Tag, Modal, Empty, useToast } from './ui'
import { useAuth } from '../auth'

// 冷链温度曲线迷你图（0-6℃ 为达标区间，超过红线即脱冷）
function ColdCurve({ curve, min, max, breached }) {
  const pts = (curve || []).map((p) => Number(p.temp)).filter((v) => !Number.isNaN(v))
  if (pts.length === 0) return <span className="small muted">暂无温度曲线</span>
  const w = 300, h = 72, pad = 8
  const lo = Math.min(0, ...pts), hi = Math.max(6, ...pts)
  const x = (i) => pad + (i * (w - 2 * pad)) / Math.max(pts.length - 1, 1)
  const y = (v) => h - pad - ((v - lo) * (h - 2 * pad)) / (hi - lo)
  const line = pts.map((v, i) => `${x(i)},${y(v)}`).join(' ')
  const y6 = y(6)
  return (
    <div>
      <svg width="100%" viewBox={`0 0 ${w} ${h}`} style={{ maxWidth: 360 }}>
        <rect x={pad} y={y(6)} width={w - 2 * pad} height={y(lo) - y(6)} fill="#dcfce7" opacity="0.6" />
        <line x1={pad} y1={y6} x2={w - pad} y2={y6} stroke="#ef4444" strokeDasharray="4 3" strokeWidth="1" />
        <polyline points={line} fill="none" stroke="#0369a1" strokeWidth="2" />
        {pts.map((v, i) => <circle key={i} cx={x(i)} cy={y(v)} r="2.4" fill={v > 6 ? '#dc2626' : '#0369a1'} />)}
        <text x={w - pad - 44} y={y6 - 3} fontSize="9" fill="#dc2626">6℃ 上限</text>
      </svg>
      <div className="small">
        温度区间 <strong>{min ?? '—'}℃ ~ {max ?? '—'}℃</strong>{' '}
        {breached
          ? <Tag cls="red">⚠ 脱冷（超 6℃）</Tag>
          : <Tag cls="green">全程 0-6℃ 达标</Tag>}
        <span className="muted"> · 共 {pts.length} 个温度点</span>
      </div>
    </div>
  )
}

function PhotoLine({ photos, empty }) {
  if (!photos || photos.length === 0) return <span className="small muted">{empty || '无'}</span>
  return (
    <div className="row" style={{ flexWrap: 'wrap', gap: 6 }}>
      {photos.map((p, i) => (
        <span key={i} className="tag blue" title={p.url || String(p)}>
          📷 {p.caption || (typeof p === 'string' ? p : `照片 ${i + 1}`)}
        </span>
      ))}
    </div>
  )
}

// 工单详情中的坏果赔付判定面板
export function ClaimPanel({ incidentId, onChanged }) {
  const { user } = useAuth()
  const toast = useToast()
  const [claims, setClaims] = useState(null)
  const [judge, setJudge] = useState(false)
  const [coldForm, setColdForm] = useState(false)

  const load = () => get(`/claims/bad-fruit?incident_id=${incidentId}`).then(setClaims).catch(() => setClaims([]))
  useEffect(load, [incidentId])
  if (!claims) return null
  if (claims.length === 0) return null
  const c = claims[0]
  const canJudge = user && ['service', 'admin', 'farmer'].includes(user.role)
  const canClose = user && ['service', 'admin'].includes(user.role)
  const isLogistics = user && user.role === 'logistics'

  return (
    <div className="card mt16" style={{ background: '#fffdf5', borderColor: '#fde68a' }}>
      <div className="row">
        <div className="section-title" style={{ margin: 0 }}>🫐 坏果赔付判定 #{c.id}</div>
        <Tag cls={CLAIM_STATUS_CLS[c.status]}>{CLAIM_STATUS_LABEL[c.status]}</Tag>
        {c.scheme && <Tag cls={CLAIM_SCHEME_CLS[c.scheme]}>方案：{CLAIM_SCHEME_LABEL[c.scheme]}</Tag>}
        {c.cold_chain_responsible && <Tag cls="red">冷链责任 · 已同步物流</Tag>}
        <span className="spacer" />
        {canJudge && c.status === 'open' && <button className="sm amber" onClick={() => setJudge(true)}>出具赔付判定</button>}
        {canClose && c.status === 'judged' && <CloseClaimButton id={c.id} onDone={() => { load(); onChanged && onChanged() }} />}
      </div>

      <div className="grid grid-4 mt8">
        <div className="stat red"><div className="n">{c.bad_rate_pct}%</div><div className="l">坏果比例（{c.bad_count}/{c.total_count}）</div></div>
        <div className="stat"><div className="n">{fmtMoney(c.refund_amount)}</div><div className="l">退款</div></div>
        <div className="stat amber"><div className="n">{fmtMoney(c.reship_cost)}</div><div className="l">补发果品成本</div></div>
        <div className="stat blue"><div className="n">{fmtMoney(c.freight_cost)}</div><div className="l">运费（分开核算）</div></div>
      </div>
      {c.status === 'open' && c.recommended_scheme && (
        <div className="small mt8">📋 按坏果比例与冷链情况的系统建议：
          <Tag cls={CLAIM_SCHEME_CLS[c.recommended_scheme]}>{CLAIM_SCHEME_LABEL[c.recommended_scheme]}</Tag>
        </div>
      )}

      <div className="grid grid-2 mt16">
        <div>
          <h4>🔗 平台关联证据（赔付关闭后保留）</h4>
          <table>
            <tbody>
              <tr><td className="small muted">采摘时间</td><td className="small">{fmtDate(c.harvest_at)}</td></tr>
              <tr><td className="small muted">签收时间</td><td className="small">{fmtDate(c.signed_at)}</td></tr>
              <tr><td className="small muted">冷链路线</td><td className="small">{c.cold_route || '—'}<br />
                <span className="muted">{c.logistics_company} {c.tracking_no}</span></td></tr>
              <tr><td className="small muted">分拣照片</td><td><PhotoLine photos={c.sorting_photos} /></td></tr>
              <tr><td className="small muted">开箱照片</td><td><PhotoLine photos={c.open_box_photos} /></td></tr>
            </tbody>
          </table>
        </div>
        <div>
          <h4>🌡 冷链温度曲线</h4>
          <ColdCurve curve={c.cold_chain_curve} min={c.cold_temp_min} max={c.cold_temp_max} breached={c.cold_breached} />
          {isLogistics && !c.cold_chain_curve?.length && (
            <button className="sm ghost mt8" onClick={() => setColdForm(true)}>补登冷链温度曲线</button>
          )}
        </div>
      </div>

      {c.conclusion && (
        <div className="mt8 small"><strong>客服结论：</strong>{c.conclusion}
          <div className="muted">判定时间 {fmtDate(c.judged_at)} · 关闭时间 {fmtDate(c.closed_at)}</div>
        </div>
      )}
      {c.reship_shipment_id && (
        <div className="small mt8">📦 补发订单已同步物流端：补发运单 #{c.reship_shipment_id}
          {c.reship_tracking_no ? `（${c.reship_tracking_no}）` : ''}
          ，状态：{c.reship_status === 'delivered' ? '已送达' : c.reship_status === 'shipped' ? '运输中' : '待物流发货'}
        </div>
      )}

      {judge && <JudgeModal claim={c} onClose={() => setJudge(false)} onDone={() => { setJudge(false); load(); onChanged && onChanged() }} />}
      {coldForm && <ColdCurveModal shipmentId={c.shipment_id} onClose={() => setColdForm(false)} onDone={() => { setColdForm(false); load() }} />}
    </div>
  )
}

function CloseClaimButton({ id, onDone }) {
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  return (
    <button className="sm" disabled={busy} onClick={async () => {
      setBusy(true)
      try {
        const r = await post(`/claims/bad-fruit/${id}/close`, {})
        toast(r.message || '赔付已关闭')
        onDone()
      } catch (e) { toast(e.message, true) } finally { setBusy(false) }
    }}>✅ 关闭赔付（执行补发/退款并同步）</button>
  )
}

function JudgeModal({ claim, onClose, onDone }) {
  const toast = useToast()
  const [f, setF] = useState({
    scheme: claim.recommended_scheme || 'reship',
    refund_amount: 0, reship_cost: 0, freight_cost: 0,
    cold_chain_responsible: claim.cold_breached || false,
    conclusion: '',
  })
  const set = (k, v) => setF({ ...f, [k]: v })
  const submit = async () => {
    if (!f.conclusion) return toast('请填写客服结论', true)
    try {
      await post(`/claims/bad-fruit/${claim.id}/judge`, {
        ...f,
        refund_amount: Number(f.refund_amount), reship_cost: Number(f.reship_cost), freight_cost: Number(f.freight_cost),
      })
      toast('判定已出具，可关闭赔付执行同步')
      onDone()
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title={`坏果赔付判定 #${claim.id} · 坏果比例 ${claim.bad_rate_pct}%`} onClose={onClose} wide>
      <p className="muted small">证据已自动关联：采摘时间、分拣照片、冷链温度曲线、签收时间、开箱照片。
        系统建议：{CLAIM_SCHEME_LABEL[claim.recommended_scheme] || '—'}（冷链脱冷→物流追责；坏果率≥30%→退款；其余→补发）</p>
      <label>赔付方案</label>
      <div className="row">
        {Object.entries(CLAIM_SCHEME_LABEL).map(([k, v]) => (
          <button key={k} type="button" className={'sm' + (f.scheme === k ? '' : ' ghost')}
                  style={{ borderWidth: f.scheme === k ? 2 : 1 }} onClick={() => set('scheme', k)}>{v}</button>
        ))}
      </div>
      <div className="grid grid-3 mt8">
        <div><label>退款金额 ¥{f.scheme === 'refund' ? '' : '（仅退款方案）'}</label>
          <input type="number" disabled={f.scheme !== 'refund'} value={f.refund_amount}
                 onChange={(e) => set('refund_amount', e.target.value)} /></div>
        <div><label>补发果品成本 ¥</label>
          <input type="number" disabled={f.scheme === 'refund'} value={f.reship_cost}
                 onChange={(e) => set('reship_cost', e.target.value)} /></div>
        <div><label>运费 ¥（与补发成本分开）</label>
          <input type="number" disabled={f.scheme === 'refund'} value={f.freight_cost}
                 onChange={(e) => set('freight_cost', e.target.value)} /></div>
      </div>
      <label className="mt8">
        <input type="checkbox" style={{ width: 'auto', marginRight: 6 }} checked={f.cold_chain_responsible}
               onChange={(e) => set('cold_chain_responsible', e.target.checked)} />
        判定为冷链（物流）责任——关闭时同步物流端并向物流追偿运费
      </label>
      <label className="mt8">客服结论</label>
      <textarea rows="3" value={f.conclusion} onChange={(e) => set('conclusion', e.target.value)}
                placeholder="结合坏果比例、冷链曲线、分拣照片给出结论；赔付关闭后开箱照片、冷链曲线与结论将保留备查" />
      <button className="mt16" onClick={submit}>出具判定</button>
    </Modal>
  )
}

function ColdCurveModal({ shipmentId, onClose, onDone }) {
  const toast = useToast()
  const [text, setText] = useState('[{"t":"09:00","temp":2.4},{"t":"13:00","temp":8.6},{"t":"17:00","temp":3.1}]')
  const submit = async () => {
    let curve
    try { curve = JSON.parse(text) } catch { return toast('JSON 格式错误', true) }
    try {
      await post(`/shipments/${shipmentId}/cold-chain`, { cold_chain_curve: curve })
      toast('冷链曲线已登记并同步工单')
      onDone()
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title={`登记冷链温度曲线 · 运单 #${shipmentId}`} onClose={onClose}>
      <p className="muted small">温度点格式 [{"{\"t\":\"时间\",\"temp\":温度}"}]，最高温超过 6℃ 自动判定脱冷。</p>
      <textarea rows="6" value={text} onChange={(e) => setText(e.target.value)} />
      <button className="mt16" onClick={submit}>提交曲线</button>
    </Modal>
  )
}

// 认养人提交坏果反馈
export function BadFruitFeedbackModal({ adoption, onClose, onDone }) {
  const toast = useToast()
  const [f, setF] = useState({
    total_count: 12, bad_count: 1,
    open_box_photos: '', description: '',
  })
  const set = (k, v) => setF({ ...f, [k]: v })
  const rate = f.total_count > 0 ? (f.bad_count / f.total_count * 100).toFixed(1) : '0.0'
  const submit = async () => {
    const photos = f.open_box_photos.split(/[\n,，]/).map((s) => s.trim()).filter(Boolean)
      .map((url) => ({ caption: '开箱照片', url }))
    try {
      const r = await post('/claims/bad-fruit', {
        adoption_id: adoption.id,
        total_count: Number(f.total_count),
        bad_count: Number(f.bad_count),
        open_box_photos: photos,
        description: f.description,
      })
      toast(r.message || '坏果反馈已提交')
      onDone && onDone(r.incident_id)
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title={`反馈坏果 · ${adoption.title}（订单 #${adoption.id}）`} onClose={onClose}>
      <p className="muted small">平台会自动关联该订单的<b>采摘时间、分拣照片、冷链温度、签收时间</b>，
        你只需填写坏果数量并上传开箱照片；客服将按坏果比例给出补发 / 退款 / 物流追责方案。</p>
      <div className="grid grid-2">
        <div><label>签收总数量（盒/个）</label>
          <input type="number" value={f.total_count} onChange={(e) => set('total_count', e.target.value)} /></div>
        <div><label>坏果数量</label>
          <input type="number" value={f.bad_count} onChange={(e) => set('bad_count', e.target.value)} /></div>
      </div>
      <div className="small mt8">坏果比例：<strong style={{ color: rate >= 10 ? '#dc2626' : '#b45309' }}>{rate}%</strong></div>
      <label className="mt8">开箱照片（每行一个图片地址/说明）</label>
      <textarea rows="3" value={f.open_box_photos} onChange={(e) => set('open_box_photos', e.target.value)}
                placeholder={'/static/demo/open-1.jpg\n/static/demo/open-2.jpg'} />
      <label>情况说明</label>
      <textarea rows="2" value={f.description} onChange={(e) => set('description', e.target.value)}
                placeholder="如：开箱发现底层果粒压伤、长霉…" />
      <button className="mt16" onClick={submit}>提交坏果反馈</button>
    </Modal>
  )
}
