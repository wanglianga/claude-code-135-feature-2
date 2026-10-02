import React, { useCallback, useEffect, useState } from 'react'
import { get, post, KIND_EMOJI, fmtMoney, fmtKg, fmtDay, fmtDate, SHIP_STATUS_LABEL } from '../api'
import { Tag, ShipFlow, Modal, Empty, Stat, useToast } from '../components/ui'

const ORDER_STATUS = { paid: '待发货', shipping: '履约中', completed: '已完成', refunded: '已退款' }
const COMP_LABEL = { cash: '现金赔付', reship: '补发', insurance: '保险赔付', credit: '下季抵扣' }
const COMP_CLS = { cash: 'red', reship: 'amber', insurance: 'teal', credit: 'purple' }
const CLAIM_PLAN_LABEL = { reship: '补发', refund: '退款', logistics_liability: '物流追责' }
const RESHIP_STATUS_LABEL = { pending: '待同步', synced: '已同步物流', shipped: '补发已发出', delivered: '补发已送达' }

export default function MyAdoptions() {
  const [list, setList] = useState([])
  const [focus, setFocus] = useState(null) // address / callback / cert
  const [cur, setCur] = useState(null)
  const load = useCallback(() => get('/my/adoptions').then(setList), [])
  useEffect(() => { load() }, [load])

  return (
    <>
      <h1>🎁 我的认养与权益兑现</h1>
      <p className="muted">每一份认养的保底产量、分批发货、赔付折算与满意度回访都在这里核对——看自己的权益是否被公平兑现。</p>
      {list.length === 0 ? <Empty text="还没有认养记录，去首页挑一块好地吧" /> : (
        <div className="grid">
          {list.map((a) => {
            const rf = a.rights_fulfillment
            return (
              <div key={a.id} className="card">
                <div className="row" style={{ alignItems: 'flex-start' }}>
                  <div className="proj-emoji">{KIND_EMOJI[a.kind]}</div>
                  <div style={{ flex: 1 }}>
                    <div className="row">
                      <h2 style={{ margin: 0 }}>{a.title}</h2>
                      <Tag cls={a.status === 'completed' ? 'gray' : 'amber'}>{ORDER_STATUS[a.status]}</Tag>
                      <Tag cls="blue">批次 {a.batch_no}</Tag>
                      <Tag>{a.shares} {a.unit_name || '份'}</Tag>
                    </div>
                    <div className="small muted mt8">{a.farm_name} · 订单号 #{a.id} · {fmtMoney(a.amount)}</div>
                  </div>
                  <div style={{ textAlign: 'right' }}>
                    {rf.fair
                      ? <span className="badge-fair ok">✓ 权益已公平兑现</span>
                      : rf.projected_fair
                        ? <span className="badge-fair warn">⏳ 在途批次兑现中</span>
                        : <span className="badge-fair warn">⚠ 保底权益存在缺口</span>}
                    <div className="small muted mt8">{rf.stage}</div>
                  </div>
                </div>

                <div className="grid grid-4 mt16">
                  <Stat label="保底产量" value={fmtKg(rf.guarantee_kg)} />
                  <Stat label="已收到" value={fmtKg(rf.shipped_kg)} cls="blue" />
                  <Stat label="在途/待发" value={fmtKg(rf.planned_kg)} cls="amber" />
                  <Stat label="赔付折算" value={fmtKg(rf.compensated_kg_equiv)} cls={rf.compensated_kg_equiv > 0 ? 'red' : ''} />
                </div>

                <div className="grid grid-2 mt16">
                  <div>
                    <h3>🚚 分批发货计划</h3>
                    <table>
                      <thead><tr><th>批次</th><th>计划</th><th>规格/路线</th><th>状态</th></tr></thead>
                      <tbody>
                        {(a.shipments || []).map((s) => (
                          <tr key={s.id || s.seq}>
                            <td>第{s.seq}批<br /><span className="small muted">{s.weight_kg}kg</span></td>
                            <td>{fmtDay(s.plan_date)}</td>
                            <td className="small">{s.pack_spec}<br /><span className="muted">{s.cold_route}</span><br />
                              {s.tracking_no && <span className="muted">运单 {s.tracking_no}</span>}</td>
                            <td><ShipFlow status={s.status} /></td>
                          </tr>
                        ))}
                        {(!a.shipments || a.shipments.length === 0) && <tr><td colSpan="4" className="muted">尚未排产</td></tr>}
                      </tbody>
                    </table>
                  </div>
                  <div>
                    <h3>💰 赔付与售后</h3>
                    {(a.compensations || []).length === 0 ? <Empty text="暂无赔付记录" /> : (
                      <table>
                        <thead><tr><th>事项</th><th>方式</th><th>金额</th><th>状态</th></tr></thead>
                        <tbody>
                          {a.compensations.map((c) => (
                            <tr key={c.id || c.note}>
                              <td className="small">{c.incident_title}<br /><span className="muted">{c.note}</span></td>
                              <td><Tag cls={COMP_CLS[c.kind]}>{COMP_LABEL[c.kind]}</Tag></td>
                              <td>{fmtMoney(c.amount)}</td>
                              <td>{c.status === 'paid' ? '已到账' : '处理中'}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                    <h3 className="mt16">🫐 坏果赔付判定（认养档案）</h3>
                    {(a.archive_notes || []).length === 0 ? <Empty text="暂无坏果赔付档案" /> : a.archive_notes.map((n) => (
                      <div key={n.id} className="card" style={{ background: '#fff7ed', padding: 10, marginBottom: 8 }}>
                        <div className="small"><Tag cls="red">{CLAIM_PLAN_LABEL[n.detail?.plan] || '坏果赔付'}</Tag> <span className="muted">{fmtDate(n.created_at)} · 批次 {n.batch_no}</span></div>
                        <div className="small mt8">{n.summary}</div>
                      </div>
                    ))}
                    {(a.reship_orders || []).length > 0 && (
                      <table>
                        <thead><tr><th>补发单</th><th>重量</th><th>物流状态</th></tr></thead>
                        <tbody>
                          {a.reship_orders.map((r) => (
                            <tr key={r.id}>
                              <td className="small">{r.reship_no}<br /><span className="muted">{r.ship_tracking_no || '待物流回填运单号'}</span></td>
                              <td>{r.weight_kg}kg</td>
                              <td><Tag cls={r.shipment_status === 'delivered' ? 'green' : 'teal'}>{RESHIP_STATUS_LABEL[r.status]}</Tag></td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                    <h3 className="mt16">⭐ 我的回访</h3>
                    {(a.callbacks || []).length === 0 ? <Empty text="尚未回访" /> : a.callbacks.map((c) => (
                      <div key={c.id} className="small">
                        满意度 {'★'.repeat(c.satisfaction)}{'☆'.repeat(5 - c.satisfaction)}
                        （品质 {c.quality_score}/5 · 配送 {c.delivery_score}/5）
                        {c.would_renew && <Tag cls="green">愿续认</Tag>}
                        <div className="muted">“{c.comment}”</div>
                        <div className="muted">续认建议：{c.renew_suggestion}</div>
                      </div>
                    ))}
                  </div>
                </div>

                <div className="row mt16">
                  {a.cert_no && <button className="sm ghost" onClick={() => setCur(a) || setFocus('cert')}>🏆 查看认养证书</button>}
                  {a.status !== 'completed' && (
                    <button className="sm ghost" onClick={() => { setCur(a); setFocus('address') }}>📍 申请改地址</button>
                  )}
                  {(a.callbacks || []).length === 0 && a.status === 'completed' && (
                    <button className="sm" onClick={() => { setCur(a); setFocus('callback') }}>📝 成熟度回访</button>
                  )}
                  <span className="spacer" />
                  <span className="small muted">收货：{a.recipient_name} {a.phone} · {a.address}</span>
                </div>
              </div>
            )
          })}
        </div>
      )}
      {focus === 'address' && cur && <AddressModal adoption={cur} onClose={() => setFocus(null)} onDone={load} />}
      {focus === 'callback' && cur && <CallbackModal adoption={cur} onClose={() => setFocus(null)} onDone={load} />}
      {focus === 'cert' && cur && <CertModal adoption={cur} onClose={() => setFocus(null)} />}
    </>
  )
}

function AddressModal({ adoption, onClose, onDone }) {
  const toast = useToast()
  const [f, setF] = useState({ recipient_name: adoption.recipient_name, phone: adoption.phone, address: adoption.address, reason: '' })
  const submit = async () => {
    try {
      await post(`/adoptions/${adoption.id}/address`, f)
      toast('地址已更新，客服与冷链已同步并生成工单')
      onDone(); onClose()
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title={`临时改地址 · 订单 #${adoption.id}`} onClose={onClose}>
      <p className="muted small">未发出的批次可在线改址；已有批次发出后需联系客服协调。</p>
      <div className="grid grid-2">
        <div><label>收货人</label><input value={f.recipient_name} onChange={(e) => setF({ ...f, recipient_name: e.target.value })} /></div>
        <div><label>手机号</label><input value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} /></div>
      </div>
      <label>新地址</label><input value={f.address} onChange={(e) => setF({ ...f, address: e.target.value })} />
      <label>改址原因</label><input value={f.reason} onChange={(e) => setF({ ...f, reason: e.target.value })} placeholder="出差/搬家/送亲友…" />
      <button className="mt16" onClick={submit}>提交申请</button>
    </Modal>
  )
}

function CallbackModal({ adoption, onClose, onDone }) {
  const toast = useToast()
  const [f, setF] = useState({ satisfaction: 5, quality_score: 5, delivery_score: 5, would_renew: true, comment: '', renew_suggestion: '' })
  const stars = (k) => (
    <div>
      {[1, 2, 3, 4, 5].map((n) => (
        <span key={n} style={{ fontSize: 22, cursor: 'pointer', color: n <= f[k] ? '#f59e0b' : '#d1d5db' }}
              onClick={() => setF({ ...f, [k]: n })}>★</span>
      ))}
    </div>
  )
  const submit = async () => {
    try {
      await post('/callbacks', { adoption_id: adoption.id, ...f })
      toast('感谢回访，你的建议会沉淀到下一季')
      onDone(); onClose()
    } catch (e) { toast(e.message, true) }
  }
  return (
    <Modal title={`成熟度回访 · ${adoption.title}`} onClose={onClose}>
      <label>总体满意度</label>{stars('satisfaction')}
      <label>果品/品质</label>{stars('quality_score')}
      <label>冷链配送</label>{stars('delivery_score')}
      <label><input type="checkbox" style={{ width: 'auto', marginRight: 6 }} checked={f.would_renew}
                    onChange={(e) => setF({ ...f, would_renew: e.target.checked })} />下一季愿意续认</label>
      <label>评价</label><textarea rows="2" value={f.comment} onChange={(e) => setF({ ...f, comment: e.target.value })} />
      <label>下一季续认建议</label><textarea rows="2" value={f.renew_suggestion} onChange={(e) => setF({ ...f, renew_suggestion: e.target.value })} />
      <button className="mt16" onClick={submit}>提交回访</button>
    </Modal>
  )
}

function CertModal({ adoption, onClose }) {
  return (
    <Modal title="认养证书" onClose={onClose}>
      <div className="cert">
        <div className="small muted">谷香家庭农场 · 农产品认养证书</div>
        <h2 className="mt8">{adoption.recipient_name}</h2>
        <div>认养 <strong>{adoption.title}</strong></div>
        <div className="small muted">批次 {adoption.batch_no} · {adoption.shares} {adoption.unit_name || '份'}</div>
        <div className="mt8">证书编号</div>
        <div className="no" style={{ fontSize: 18 }}>{adoption.cert_no}</div>
        <div className="small muted mt8">签发日期 {fmtDay(adoption.created_at)}</div>
      </div>
    </Modal>
  )
}
