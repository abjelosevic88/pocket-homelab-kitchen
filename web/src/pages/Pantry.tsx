import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { ago, fmtDate, todayISO } from '../lib/format'
import { fmtQty, type PantryItem } from '../lib/types'
import { Card, Empty, Loading, PageHeader, useAsync, useDebounce, useToast } from '../components/ui'
import { PantryModal, ScanModal } from '../components/forms'
import { Icon } from '../components/icons'
import { announceChange, useOnChange } from '../components/Layout'

type Facets = { locations: string[]; categories: string[]; synced_at: string; total: number; low: number; expiring: number }

export default function Pantry() {
  const { settings } = useApp()
  const { toast } = useToast()
  const [sp, setSp] = useSearchParams()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const low = sp.get('low') === '1', expiring = sp.get('expiring') === '1', loc = sp.get('location') || ''
  const setParam = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }) }
  const url = `${V1}/pantry?q=${encodeURIComponent(dq)}&low=${low ? 1 : 0}&expiring=${expiring ? 1 : 0}&location=${encodeURIComponent(loc)}`
  const { data, loading, reload } = useAsync(() => api.get<PantryItem[]>(url), [url])
  const { data: facets, reload: reloadFacets } = useAsync(() => api.get<Facets>(`${V1}/pantry/facets`), [])
  const [modal, setModal] = useState<{ kind: 'add' } | { kind: 'edit'; item: PantryItem } | { kind: 'scan' } | null>(() => sp.get('scan') ? { kind: 'scan' } : null)
  const [syncing, setSyncing] = useState(false)
  const refresh = () => announceChange()
  useOnChange(() => { reload(); reloadFacets() })
  const adjust = async (p: PantryItem, delta: number) => {
    try { await api.post(`${V1}/pantry/${p.id}/adjust`, { delta }); refresh() } catch (e) { toast((e as Error).message, 'err') }
  }
  const sync = async () => {
    setSyncing(true)
    try { const r = await api.post<{ ok: boolean; counts?: Record<string, number>; error?: string }>(`${V1}/pantry/sync`); if (r.ok) toast(`Synced: ${r.counts!.total} items (${r.counts!.created} new, ${r.counts!.archived} gone)`); else toast(r.error || 'sync failed', 'err'); refresh() } catch (e) { toast((e as Error).message, 'err') } finally { setSyncing(false) }
  }
  const today = todayISO()
  const byLoc: Record<string, PantryItem[]> = {}
  for (const p of data || []) (byLoc[p.location || 'Elsewhere'] ||= []).push(p)
  const step = (p: PantryItem) => (p.unit === 'g' || p.unit === 'ml') ? 100 : (p.unit === 'kg' || p.unit === 'l') ? 0.5 : 1
  return (
    <>
      <PageHeader title="Pantry" sub={facets ? `${facets.total} items · ${facets.low} low · ${facets.expiring} expiring` : ''}
        actions={<>{settings?.inventory_configured && <button type="button" className="btn" onClick={sync} disabled={syncing} title={facets?.synced_at ? `Last synced ${ago(facets.synced_at)}` : 'Never synced'}><Icon name="refresh" size={15} />{syncing ? 'Syncing…' : 'Sync Inventory'}</button>}<button type="button" className="btn" onClick={() => setModal({ kind: 'scan' })}><Icon name="scan" size={15} />Scan</button><button type="button" className="btn primary" onClick={() => setModal({ kind: 'add' })}><Icon name="plus" size={15} />Add</button></>} />
      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search the pantry…" /></div>
        <button type="button" className={`chip ${low ? 'active' : ''}`} onClick={() => setParam('low', low ? '' : '1')}><Icon name="alert" size={14} />Running low{facets?.low ? <span className="n">{facets.low}</span> : null}</button>
        <button type="button" className={`chip ${expiring ? 'active' : ''}`} onClick={() => setParam('expiring', expiring ? '' : '1')}><Icon name="clock" size={14} />Use soon{facets?.expiring ? <span className="n">{facets.expiring}</span> : null}</button>
      </div>
      {facets && facets.locations.length > 1 && <div className="filters"><button type="button" className={`chip ${!loc ? 'active' : ''}`} onClick={() => setParam('location', '')}>Everywhere</button>{facets.locations.map(l => <button type="button" key={l} className={`chip ${loc === l ? 'active' : ''}`} onClick={() => setParam('location', l)}>{l}</button>)}</div>}
      {loading && !data ? <Loading /> : !data || data.length === 0 ? <Card><Empty icon="basket" title={q || low || expiring || loc ? 'Nothing matches' : 'The pantry is empty'}>{!q && !low && !expiring && !loc && (settings?.inventory_configured ? `Tag items “${settings.inventory_tag}” in Pocket Inventory and press Sync, scan a barcode, or add by hand.` : 'Scan a barcode or add items by hand. Connect Pocket Inventory under Settings to share the pantry with the rest of the house.')}</Empty></Card> : (
        <Card flush>
          <div style={{ padding: '8px 14px' }}>
            {Object.keys(byLoc).sort().map(l => (
              <div key={l} className="loc-group">
                <h3>{l}</h3>
                {byLoc[l].map(p => (
                  <div key={p.id} className={`pantry-row ${p.quantity <= 0 ? 'zero' : p.low ? 'low' : ''}`}>
                    <div className="nm" onClick={() => setModal({ kind: 'edit', item: p })}>
                      <div className="t">{p.name}{p.inventory_id && <span className="src inv" title={`Pocket Inventory #${p.inventory_id}`}>inv</span>}{p.expiry_date && <span className={`expiry ${p.expiry_date < today ? 'past' : ''}`}>{p.expiry_date < today ? 'expired' : `by ${fmtDate(p.expiry_date, false)}`}</span>}</div>
                      <div className="s">{[p.brand, p.category, p.min_quantity > 0 ? `min ${fmtQty(p.min_quantity, p.unit)}` : '', p.notes].filter(Boolean).join(' · ')}</div>
                    </div>
                    <span className="qty"><button type="button" onClick={() => adjust(p, -step(p))} aria-label="Less"><Icon name="minus" size={14} /></button><span>{p.quantity > 0 ? fmtQty(p.quantity, p.unit) : `0${p.unit && p.unit !== 'pcs' ? ' ' + p.unit : ''}`}</span><button type="button" onClick={() => adjust(p, step(p))} aria-label="More"><Icon name="plus" size={14} /></button></span>
                  </div>
                ))}
              </div>
            ))}
          </div>
        </Card>
      )}
      <p className="muted small mt">−/+ step by 100 g / 100 ml, 0.5 kg / 0.5 l, or 1 piece. {settings?.inventory_configured ? `Items marked “inv” mirror Pocket Inventory; changes go there and back${facets?.synced_at ? ` (last sync ${ago(facets.synced_at)})` : ''}.` : ''}</p>
      {modal?.kind === 'add' && <PantryModal onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh() }} />}
      {modal?.kind === 'edit' && <PantryModal item={modal.item} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh() }} onDeleted={() => { setModal(null); refresh() }} />}
      {modal?.kind === 'scan' && <ScanModal onClose={() => { setModal(null); setParam('scan', '') }} onAdded={() => { setModal(null); setParam('scan', ''); refresh() }} />}
    </>
  )
}
