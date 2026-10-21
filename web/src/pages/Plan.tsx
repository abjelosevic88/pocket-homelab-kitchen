import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { addDays, dateRange, fmtDate, todayISO, weekday } from '../lib/format'
import { cap, fmtMinutes, type PlanEntry } from '../lib/types'
import { Confirm, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { CookModal, PlanModal, ShopFromPlanModal } from '../components/forms'
import { Icon, SLOT_ICON } from '../components/icons'
import { announceChange, useOnChange } from '../components/Layout'

function weekStart(d: string, start: 'mon' | 'sun'): string {
  const [y, m, dd] = d.split('-').map(Number)
  const wd = new Date(y, m - 1, dd).getDay()
  let off = start === 'sun' ? wd : wd - 1
  if (off < 0) off += 7
  return addDays(d, -off)
}

export default function Plan() {
  const { settings, meta } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const today = todayISO()
  const [from, setFrom] = useState(() => weekStart(today, settings?.week_start || 'mon'))
  const to = addDays(from, 6)
  const { data, loading, reload } = useAsync(() => api.get<{ entries: PlanEntry[] }>(`${V1}/plan?from=${from}&to=${to}`), [from])
  const [modal, setModal] = useState<{ kind: 'new'; date: string; slot?: string } | { kind: 'edit'; entry: PlanEntry } | { kind: 'cook'; entry: PlanEntry } | { kind: 'shop' } | { kind: 'delete'; entry: PlanEntry } | null>(null)
  const [drag, setDrag] = useState<PlanEntry | null>(null)
  useOnChange(() => { reload() })
  const changed = () => { setModal(null); announceChange() }
  const [over, setOver] = useState('')
  const days = dateRange(from, to)
  const byDay: Record<string, PlanEntry[]> = {}
  for (const e of data?.entries || []) (byDay[e.date] ||= []).push(e)
  const drop = async (date: string) => {
    if (!drag || drag.date === date) { setDrag(null); setOver(''); return }
    try { await api.put(`${V1}/plan/${drag.id}`, { date }); announceChange() } catch (e) { toast((e as Error).message, 'err') }
    setDrag(null); setOver('')
  }
  const copyLast = async () => {
    try { const r = await api.post<{ copied: number }>(`${V1}/plan/copy`, { from: addDays(from, -7), to: addDays(to, -7), days: 7 }); toast(`${r.copied} meals copied from last week`); announceChange() } catch (e) { toast((e as Error).message, 'err') }
  }
  const planned = (data?.entries || []).filter(e => e.recipe_id && !e.cooked_at).length
  return (
    <>
      <PageHeader title="Meal plan" sub={`${fmtDate(from, false)} – ${fmtDate(to)}`}
        actions={<>
          <span className="week-nav"><button type="button" className="btn ghost icon-only" onClick={() => setFrom(addDays(from, -7))} aria-label="Previous week" title="Previous week"><Icon name="chevron-left" size={18} /></button><button type="button" className="btn" onClick={() => setFrom(weekStart(today, settings?.week_start || 'mon'))}>Today</button><button type="button" className="btn ghost icon-only" onClick={() => setFrom(addDays(from, 7))} aria-label="Next week" title="Next week"><Icon name="chevron-right" size={18} /></button></span>
          <button type="button" className="btn" onClick={copyLast}><Icon name="copy" size={15} />Copy last week</button>
          <button type="button" className="btn primary" onClick={() => setModal({ kind: 'shop' })} disabled={planned === 0}><Icon name="cart" size={15} />Shopping list<span className="badge">{planned}</span></button>
        </>} />
      {loading && !data ? <Loading /> : (
        <div className="week">
          {days.map(d => (
            <div key={d} className={`day-col ${d === today ? 'today' : ''} ${d < today ? 'past' : ''} ${over === d ? 'over' : ''}`} onDragOver={e => { e.preventDefault(); setOver(d) }} onDragLeave={() => setOver('')} onDrop={() => drop(d)}>
              <h4><span>{weekday(d)}</span><span className="d">{fmtDate(d, false)}</span></h4>
              {(byDay[d] || []).map(e => (
                <div key={e.id} className={`meal ${e.cooked_at ? 'cooked' : ''} ${drag?.id === e.id ? 'drag' : ''}`} draggable onDragStart={() => setDrag(e)} onDragEnd={() => { setDrag(null); setOver('') }} onClick={() => setModal({ kind: 'edit', entry: e })}>
                  <div className="slot"><span><Icon name={SLOT_ICON[e.slot] || 'utensils'} size={12} />{e.slot}</span>{e.cooked_at ? <span title="Cooked" className="ok-text"><Icon name="check" size={13} strokeWidth={2.4} /></span> : d <= today ? <button type="button" className="icon-btn" title="Cooked it" aria-label="Cooked it" onClick={ev => { ev.stopPropagation(); setModal({ kind: 'cook', entry: e }) }}><Icon name="flame" size={14} /></button> : null}</div>
                  <div className="t">{e.recipe_title || e.title}</div>
                  <div className="m">{e.recipe_minutes ? fmtMinutes(e.recipe_minutes) : ''}{e.servings ? ` · ${e.servings} serv.` : ''}{e.notes ? ` · ${e.notes}` : ''}</div>
                </div>
              ))}
              <button type="button" className="add" onClick={() => setModal({ kind: 'new', date: d })}><Icon name="plus" size={13} />add</button>
            </div>
          ))}
        </div>
      )}
      <p className="muted small mt">Drag a meal to another day. Click it to edit, the flame to log it as cooked (that takes the ingredients off the pantry{settings?.habits_configured ? ' and marks the habit' : ''}). Slots: {meta.slots.map(s => cap(s)).join(', ')}.</p>
      {modal?.kind === 'new' && <PlanModal date={modal.date} slot={modal.slot} onClose={() => setModal(null)} onSaved={changed} />}
      {modal?.kind === 'edit' && <EditEntry entry={modal.entry} onClose={() => setModal(null)} onChanged={changed} onCook={() => setModal({ kind: 'cook', entry: modal.entry })} onDelete={() => setModal({ kind: 'delete', entry: modal.entry })} />}
      {modal?.kind === 'cook' && <CookModal entry={modal.entry} onClose={() => setModal(null)} onDone={changed} />}
      {modal?.kind === 'shop' && <ShopFromPlanModal from={from} to={to} onClose={() => setModal(null)} onDone={id => { setModal(null); navigate(`/shopping/${id}`) }} />}
      {modal?.kind === 'delete' && <Confirm confirmLabel="Remove" title="Remove this meal from the plan?" message={modal.entry.recipe_title || modal.entry.title} onCancel={() => setModal(null)} onConfirm={async () => { await api.del(`${V1}/plan/${modal.entry.id}`); changed() }} />}
    </>
  )
}

function EditEntry({ entry, onClose, onChanged, onCook, onDelete }: { entry: PlanEntry; onClose: () => void; onChanged: () => void; onCook: () => void; onDelete: () => void }) {
  const { toast } = useToast()
  const today = todayISO()
  const uncook = async () => { try { await api.put(`${V1}/plan/${entry.id}`, { cooked: false }); toast('Marked as not cooked'); onChanged() } catch (e) { toast((e as Error).message, 'err') } }
  const extra = (
    <span className="row" style={{ gap: 4 }}>
      {entry.recipe_id && <Link className="btn ghost sm" to={`/recipes/${entry.recipe_id}`}><Icon name="book" size={14} />Recipe</Link>}
      {entry.cooked_at ? <button type="button" className="btn ghost sm" onClick={uncook}><Icon name="undo" size={14} />Not cooked</button> : entry.date <= today && <button type="button" className="btn ghost sm" onClick={onCook}><Icon name="flame" size={14} />Cooked it</button>}
      <button type="button" className="btn ghost sm danger-text" onClick={onDelete}><Icon name="trash" size={14} />Remove</button>
    </span>
  )
  return <PlanModal entry={entry} extra={extra} onClose={onClose} onSaved={onChanged} />
}
