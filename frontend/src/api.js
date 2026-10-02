// API 封装：token 存 localStorage，自动带 Authorization
const TOKEN_KEY = 'farmcrowd_token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY)
}
export function setToken(t) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

export async function api(method, path, body) {
  const headers = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) headers.Authorization = 'Bearer ' + token
  const res = await fetch('/api' + path, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) {
    throw new Error((data && data.error) || `请求失败 ${res.status}`)
  }
  return data
}

export const get = (p) => api('GET', p)
export const post = (p, b) => api('POST', p, b || {})

export const KIND_LABEL = {
  apple: '苹果',
  blueberry: '蓝莓',
  honey: '蜂蜜',
  rice: '稻田',
  vegetable: '有机蔬菜',
}
export const KIND_EMOJI = {
  apple: '🍎',
  blueberry: '🫐',
  honey: '🍯',
  rice: '🌾',
  vegetable: '🥬',
}
export const INCIDENT_LABEL = {
  hail: '冰雹减产',
  delay: '成熟延后',
  address_change: '改地址',
  size_uneven: '果品大小不均',
  bad_fruit: '坏果投诉',
  delivery_dispute: '分批配送争议',
  rights_mismatch: '权益与产量不一致',
  insurance: '保险理赔',
  other: '其他',
}
export const INCIDENT_COLOR = {
  hail: '#b45309',
  delay: '#a16207',
  address_change: '#0369a1',
  size_uneven: '#7c3aed',
  bad_fruit: '#dc2626',
  delivery_dispute: '#be185d',
  rights_mismatch: '#9d174d',
  insurance: '#0f766e',
  other: '#475569',
}
export const ROLE_LABEL = {
  admin: '平台管理员',
  farmer: '农场主',
  adopter: '认养人',
  picker: '采摘工',
  packer: '包装员',
  logistics: '物流',
  service: '客服',
}
export const SHIP_STATUS_LABEL = {
  planned: '待采摘',
  picked: '已采摘',
  packed: '已包装',
  shipped: '运输中',
  delivered: '已送达',
}
export const SHIP_STATUS_FLOW = ['planned', 'picked', 'packed', 'shipped', 'delivered']

// 坏果赔付判定
export const CLAIM_PLAN_LABEL = {
  pending: '待判定',
  reship: '补发',
  refund: '退款',
  logistics_liability: '物流追责',
}
export const CLAIM_PLAN_HINT = {
  reship: '坏果比例较低（<10%）：补发等值果品',
  refund: '坏果比例 ≥10%：按坏果量退还货款',
  logistics_liability: '冷链曲线证实脱冷：向物流追责（运费与补发成本分开）',
}
export const LIABILITY_LABEL = {
  farm: '农场责任',
  logistics: '物流责任',
  shared: '共同责任',
}
export const RESHIP_STATUS_LABEL = {
  pending: '待同步',
  synced: '已同步物流',
  shipped: '补发已发出',
  delivered: '补发已送达',
}
export const NOTICE_STATUS_LABEL = {
  pending: '待同步',
  synced: '已同步物流',
  acknowledged: '物流已确认',
  closed: '已关闭',
}

export function fmtMoney(v) {
  if (v === null || v === undefined) return '—'
  return '¥' + Number(v).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}
export function fmtKg(v) {
  if (v === null || v === undefined) return '—'
  return Number(v).toFixed(1) + ' kg'
}
export function fmtDate(v) {
  if (!v) return '—'
  return String(v).replace('T', ' ').slice(0, 16)
}
export function fmtDay(v) {
  if (!v) return '—'
  return String(v).slice(0, 10)
}
