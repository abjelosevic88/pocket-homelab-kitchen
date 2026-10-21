import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { decimalsOf, fmtDate, todayISO } from '../lib/format'
import { cap, fmtQty, type CheckoutResult, type CookResult, type List, type ListItem, type LookupResult, type PantryItem, type PlanEntry, type Recipe, type SplitGroup } from '../lib/types'
import { Field, Modal, Segmented, useToast } from './ui'
import { Select } from './Select'
import { AmountInput } from './Money'
import { Scanner } from './Scanner'
import { Icon, SLOT_ICON } from './icons'

export function DataList({ id, items }: { id: string; items: string[] }) { return <datalist id={id}>{items.map(i => <option key={i} value={i} />)}</datalist> }

/** Recipes as select options, loaded once per modal. */
export function useRecipeOptions() {
  const [recipes, setRecipes] = useState<Recipe[]>([])
  useEffect(() => { api.get<Recipe[]>(`${V1}/recipes?sort=cooked`).then(setRecipes).catch(() => {}) }, [])
  return recipes
}

// ---------- plan ----------

export function PlanModal({ entry, date, slot, recipe, extra, onClose, onSaved }: { entry?: PlanEntry; date?: string; slot?: string; recipe?: Recipe; extra?: React.ReactNode; onClose: () => void; onSaved: (p: PlanEntry) => void }) {
  const { meta } = useApp()
  const { toast } = useToast()
  const recipes = useRecipeOptions()
  const [f, setF] = useState({ date: entry?.date || date || todayISO(), slot: entry?.slot || slot || 'dinner', recipe_id: entry?.recipe_id || recipe?.id || 0, title: entry?.title || '', servings: entry?.servings || 0, notes: entry?.notes || '' })
  const [free, setFree] = useState(!!entry && !entry.recipe_id)
  const [busy, setBusy] = useState(false)
  const set = (k: string, v: unknown) => setF(x => ({ ...x, [k]: v }))
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    const body = { ...f, recipe_id: free ? 0 : f.recipe_id, title: free ? f.title : '' }
    try { const p = entry ? await api.put<PlanEntry>(`${V1}/plan/${entry.id}`, body) : await api.post<PlanEntry>(`${V1}/plan`, body); toast('Planned'); onSaved(p) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const sel = recipes.find(r => r.id === f.recipe_id)
  return (
    <Modal title={entry ? 'Edit meal' : 'Plan a meal'} onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="Date"><input type="date" value={f.date} onChange={e => set('date', e.target.value)} required /></Field>
          <Field group label="Meal"><Segmented value={f.slot} onChange={v => set('slot', v)} options={meta.slots.map(s => ({ value: s, label: <span className="row" style={{ gap: 5 }}><Icon name={SLOT_ICON[s] || 'utensils'} size={13} />{cap(s)}</span> }))} size="sm" /></Field>
        </div>
        <Field group label="What"><Segmented value={free ? 'free' : 'recipe'} onChange={v => setFree(v === 'free')} options={[{ value: 'recipe', label: 'A recipe' }, { value: 'free', label: 'Free text' }]} size="sm" /></Field>
        {free
          ? <Field label="Text"><input value={f.title} onChange={e => set('title', e.target.value)} placeholder="Leftovers, eating out, pizza night…" autoFocus required /></Field>
          : <Field label="Recipe"><Select value={f.recipe_id ? String(f.recipe_id) : ''} onChange={v => set('recipe_id', Number(v))} options={recipes.map(r => ({ value: String(r.id), label: r.title, hint: `${r.total_minutes ? r.total_minutes + ' min' : ''}${r.last_cooked ? ' · ' + fmtDate(r.last_cooked, false) : ''}` }))} placeholder="Pick a recipe…" /></Field>}
        <div className="two-col tight">
          <Field label="Servings" help={sel ? `Recipe makes ${sel.servings}; 0 = as written` : '0 = as written'}><input type="number" min={0} value={f.servings || ''} onChange={e => set('servings', Number(e.target.value))} placeholder={sel ? String(sel.servings) : ''} /></Field>
          <Field label="Note"><input value={f.notes} onChange={e => set('notes', e.target.value)} placeholder="Guests coming" /></Field>
        </div>
        <div className="form-actions">{extra}<span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || (!free && !f.recipe_id)}>{entry ? 'Save' : 'Add to plan'}</button></div>
      </form>
    </Modal>
  )
}

// ---------- cook ----------

export function CookModal({ recipe, entry, onClose, onDone }: { recipe?: Recipe; entry?: PlanEntry; onClose: () => void; onDone: (r: CookResult) => void }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const title = recipe?.title || entry?.recipe_title || entry?.title || ''
  const base = recipe?.servings || entry?.recipe_servings || 0
  const [f, setF] = useState({ date: entry && entry.date <= todayISO() ? entry.date : todayISO(), servings: entry?.servings || base, notes: '', rating: recipe?.rating || 0, deduct_pantry: true, mark_habit: true })
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<CookResult | null>(null)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const r = entry ? await api.post<CookResult>(`${V1}/plan/${entry.id}/cook`, f) : await api.post<CookResult>(`${V1}/recipes/${recipe!.id}/cook`, f)
      setRes(r)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  if (res) {
    return (
      <Modal title={`Cooked: ${title}`} onClose={() => onDone(res)}>
        <div className="grid" style={{ gap: 12 }}>
          {res.deducted?.length > 0 && <div><b>Taken from the pantry</b><div className="result-list mt-sm">{res.deducted.map(d => <div key={d} className="ok">{d}</div>)}</div></div>}
          {res.not_in_pantry?.length > 0 && <div><b>Not tracked in the pantry</b><div className="result-list mt-sm">{res.not_in_pantry.map(d => <div key={d} className="miss">{d}</div>)}</div></div>}
          {res.warnings?.length > 0 && <div className="result-list">{res.warnings.map(d => <div key={d} className="warn">{d}</div>)}</div>}
          <p className="muted small">{res.habit_marked ? '✓ “Cooked at home” marked in Pocket Habits.' : settings?.habits_configured ? 'Habit not marked.' : 'Connect Pocket Habits under Settings → Connected apps to mark a habit automatically.'}</p>
          <div className="form-actions"><button type="button" className="btn primary" onClick={() => onDone(res)}>Done</button></div>
        </div>
      </Modal>
    )
  }
  return (
    <Modal title={`Cook ${title}`} onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="Date"><input type="date" value={f.date} max={todayISO()} onChange={e => setF({ ...f, date: e.target.value })} required /></Field>
          <Field label="Servings made" help={base ? `recipe makes ${base}` : undefined}><input type="number" min={1} value={f.servings || ''} onChange={e => setF({ ...f, servings: Number(e.target.value) })} /></Field>
        </div>
        {recipe && <Field group label="How was it?"><RatingPick value={f.rating} onChange={v => setF({ ...f, rating: v })} /></Field>}
        <Field label="Notes"><input value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} placeholder="Less salt next time" /></Field>
        <label className="check-row"><input type="checkbox" checked={f.deduct_pantry} onChange={e => setF({ ...f, deduct_pantry: e.target.checked })} /> Take the ingredients off the pantry{settings?.inventory_configured ? ' (Pocket Inventory)' : ''}</label>
        {settings?.habits_configured && <label className="check-row"><input type="checkbox" checked={f.mark_habit} onChange={e => setF({ ...f, mark_habit: e.target.checked })} /> Mark “cooked at home” in Pocket Habits</label>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Saving…' : <><Icon name="flame" size={15} />I cooked it</>}</button></div>
      </form>
    </Modal>
  )
}

// ---------- add recipe to plan / list ----------

export function AddToPlanModal({ recipe, onClose, onSaved }: { recipe: Recipe; onClose: () => void; onSaved: () => void }) {
  return <PlanModal recipe={recipe} onClose={onClose} onSaved={onSaved} />
}

export function AddToListModal({ recipe, onClose, onDone }: { recipe: Recipe; onClose: () => void; onDone: (listId: number) => void }) {
  const { toast } = useToast()
  const [lists, setLists] = useState<List[]>([])
  const [f, setF] = useState({ list_id: 0, servings: recipe.servings, skip_pantry: true })
  const [busy, setBusy] = useState(false)
  useEffect(() => { api.get<List[]>(`${V1}/lists`).then(l => { setLists(l); if (l[0]) setF(x => ({ ...x, list_id: l[0].id })) }).catch(() => {}) }, [])
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { const r = await api.post<{ added: ListItem[]; skipped: string[]; list: List }>(`${V1}/recipes/${recipe.id}/shop`, f); toast(`${r.added.length} added${r.skipped.length ? `, ${r.skipped.length} already in the pantry` : ''}`); onDone(r.list.id) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Add ingredients to a list" onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <div className="two-col tight">
          <Field label="List"><Select value={String(f.list_id)} onChange={v => setF({ ...f, list_id: Number(v) })} options={lists.map(l => ({ value: String(l.id), label: l.name, hint: `${l.open} open` }))} /></Field>
          <Field label="For … servings"><input type="number" min={1} value={f.servings} onChange={e => setF({ ...f, servings: Number(e.target.value) })} /></Field>
        </div>
        <label className="check-row"><input type="checkbox" checked={f.skip_pantry} onChange={e => setF({ ...f, skip_pantry: e.target.checked })} /> Skip what the pantry already has</label>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || !f.list_id}>Add to list</button></div>
      </form>
    </Modal>
  )
}

export function ShopFromPlanModal({ from, to, onClose, onDone }: { from: string; to: string; onClose: () => void; onDone: (listId: number) => void }) {
  const { toast } = useToast()
  const [lists, setLists] = useState<List[]>([])
  const [f, setF] = useState({ from, to, list_id: 0, skip_pantry: true })
  const [busy, setBusy] = useState(false)
  useEffect(() => { api.get<List[]>(`${V1}/lists`).then(l => { setLists(l); if (l[0]) setF(x => ({ ...x, list_id: l[0].id })) }).catch(() => {}) }, [])
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { const r = await api.post<{ added: number; recipes: number; skipped: string[]; list: List }>(`${V1}/plan/shop`, f); toast(`${r.added} lines from ${r.recipes} recipes${r.skipped.length ? `, ${r.skipped.length} already stocked` : ''}`); onDone(r.list.id) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Shopping list from the plan" onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <p className="muted small">Every planned recipe that is not cooked yet, scaled to its servings, merged by ingredient.</p>
        <div className="two-col tight">
          <Field label="From"><input type="date" value={f.from} onChange={e => setF({ ...f, from: e.target.value })} /></Field>
          <Field label="To"><input type="date" value={f.to} onChange={e => setF({ ...f, to: e.target.value })} /></Field>
        </div>
        <Field label="List"><Select value={String(f.list_id)} onChange={v => setF({ ...f, list_id: Number(v) })} options={lists.map(l => ({ value: String(l.id), label: l.name, hint: `${l.open} open` }))} /></Field>
        <label className="check-row"><input type="checkbox" checked={f.skip_pantry} onChange={e => setF({ ...f, skip_pantry: e.target.checked })} /> Skip what the pantry already has (short amounts are reduced)</label>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || !f.list_id}>Build the list</button></div>
      </form>
    </Modal>
  )
}

// ---------- shopping item ----------

export function ItemModal({ listId, item, onClose, onSaved, onDeleted }: { listId: number; item?: ListItem; onClose: () => void; onSaved: (i: ListItem) => void; onDeleted?: () => void }) {
  const { meta, settings, currencies } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: item?.name || '', qty: item?.qty || 0, unit: item?.unit || '', note: item?.note || '', aisle: item?.aisle || '', price: item?.price || null as number | null, currency: item?.currency || settings?.home_currency || 'EUR' })
  const [busy, setBusy] = useState(false)
  const set = (k: string, v: unknown) => setF(x => ({ ...x, [k]: v }))
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    const body = { ...f, price: f.price || 0 }
    try { const it = item ? await api.put<ListItem>(`${V1}/lists/${listId}/items/${item.id}`, body) : await api.post<ListItem>(`${V1}/lists/${listId}/items`, body); toast('Saved'); onSaved(it) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={item ? 'Edit item' : 'Add item'} onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <Field label="Item"><input value={f.name} onChange={e => set('name', e.target.value)} required autoFocus /></Field>
        <div className="three-col tight">
          <Field label="Quantity"><input type="number" step="0.01" min={0} value={f.qty || ''} onChange={e => set('qty', Number(e.target.value))} /></Field>
          <Field label="Unit"><input value={f.unit} onChange={e => set('unit', e.target.value)} list="units" placeholder="g, ml, pcs" /><DataList id="units" items={meta.units} /></Field>
          <Field label="Aisle"><Select value={f.aisle} onChange={v => set('aisle', v)} options={meta.aisles.map(a => ({ value: a, label: cap(a) }))} placeholder="auto" clearLabel="Auto" /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Price (optional)" help="Summed at checkout when no total is typed."><AmountInput amount={f.price} currency={f.currency} onAmount={m => set('price', m)} onCurrency={c => set('currency', c)} /></Field>
          <Field label="Note"><input value={f.note} onChange={e => set('note', e.target.value)} placeholder="the ripe ones" /></Field>
        </div>
        <div className="form-actions">
          {item && onDeleted && <button type="button" className="btn ghost danger-text" onClick={async () => { await api.del(`${V1}/lists/${listId}/items/${item.id}`); onDeleted() }}>Delete</button>}
          <span className="spacer" />
          <button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{item ? 'Save' : 'Add'}</button>
        </div>
      </form>
    </Modal>
  )
}

// ---------- checkout ----------

export function CheckoutModal({ list, items, onClose, onDone }: { list: List; items: ListItem[]; onClose: () => void; onDone: (r: CheckoutResult) => void }) {
  const { settings, currencies } = useApp()
  const { toast } = useToast()
  const checked = items.filter(i => i.checked)
  const dec = decimalsOf(settings?.home_currency || 'EUR', currencies)
  const sum = checked.reduce((a, i) => a + (i.price || 0), 0)
  const [groups, setGroups] = useState<SplitGroup[]>([])
  const [cats, setCats] = useState<string[]>([])
  const [f, setF] = useState({ amount: sum || null as number | null, currency: settings?.home_currency || 'EUR', date: todayISO(), store: list.store || '', notes: '', restock: true, split: !!settings?.split_configured, paid_by: settings?.split_member_id || 0, category: settings?.split_category || 'groceries', remove: true })
  const [busy, setBusy] = useState(false)
  const [res, setRes] = useState<CheckoutResult | null>(null)
  useEffect(() => { if (settings?.split_configured) { api.get<SplitGroup[]>(`${V1}/split/groups`).then(setGroups).catch(() => {}); api.get<string[]>(`${V1}/split/categories`).then(setCats).catch(() => {}) } }, [settings?.split_configured])
  const group = groups.find(g => g.id === settings?.split_group_id)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setRes(await api.post<CheckoutResult>(`${V1}/lists/${list.id}/checkout`, { ...f, amount: f.amount || 0 })) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  if (res) {
    return (
      <Modal title="Shopping done" onClose={() => onDone(res)}>
        <div className="grid" style={{ gap: 12 }}>
          <p>{res.purchase.items} item{res.purchase.items === 1 ? '' : 's'}{res.purchase.amount > 0 ? ` · ${(res.purchase.amount / Math.pow(10, dec)).toFixed(dec)} ${res.purchase.currency}` : ''}{res.purchase.store ? ` · ${res.purchase.store}` : ''}</p>
          {res.restocked.length > 0 && <div><b>Pantry</b><div className="result-list mt-sm">{res.restocked.map(d => <div key={d} className="ok">{d}</div>)}</div></div>}
          {res.purchase.split_expense_id > 0 && <p className="ok">✓ Posted to Pocket Split{res.purchase.paid_by ? ` (paid by ${res.purchase.paid_by})` : ''}.{res.split_url && <> <a href={res.split_url} target="_blank" rel="noreferrer">Open</a></>}</p>}
          {res.warnings.length > 0 && <div className="result-list">{res.warnings.map(d => <div key={d} className="warn">{d}</div>)}</div>}
          <div className="form-actions"><button type="button" className="btn primary" onClick={() => onDone(res)}>Done</button></div>
        </div>
      </Modal>
    )
  }
  return (
    <Modal title="Done shopping" onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <p className="muted small">{checked.length} checked item{checked.length === 1 ? '' : 's'}: {checked.map(i => i.name).join(', ') || 'nothing yet'}.</p>
        <div className="two-col tight">
          <Field label="Total paid" help={sum > 0 ? 'Pre-filled from the line prices' : 'Optional'}><AmountInput amount={f.amount} currency={f.currency} onAmount={m => setF({ ...f, amount: m })} onCurrency={c => setF({ ...f, currency: c })} /></Field>
          <Field label="Date"><input type="date" value={f.date} onChange={e => setF({ ...f, date: e.target.value })} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Store"><input value={f.store} onChange={e => setF({ ...f, store: e.target.value })} placeholder="Bingo" /></Field>
          <Field label="Notes"><input value={f.notes} onChange={e => setF({ ...f, notes: e.target.value })} /></Field>
        </div>
        <label className="check-row"><input type="checkbox" checked={f.restock} onChange={e => setF({ ...f, restock: e.target.checked })} /> Put the items into the pantry{settings?.inventory_configured ? ' (Pocket Inventory)' : ''}</label>
        <label className="check-row"><input type="checkbox" checked={f.remove} onChange={e => setF({ ...f, remove: e.target.checked })} /> Remove the checked lines from the list</label>
        {settings?.split_configured ? <>
          <label className="check-row"><input type="checkbox" checked={f.split} onChange={e => setF({ ...f, split: e.target.checked })} /> Post the receipt to Pocket Split{group ? ` (${group.name})` : ''}</label>
          {f.split && <div className="two-col tight">
            <Field label="Paid by">{group ? <Select value={String(f.paid_by)} onChange={v => setF({ ...f, paid_by: Number(v) })} options={group.members.map(m => ({ value: String(m.id), label: m.name }))} placeholder="Who paid?" /> : <input value={f.paid_by || ''} onChange={e => setF({ ...f, paid_by: Number(e.target.value) })} placeholder="member id" />}</Field>
            <Field label="Category">{cats.length > 0 ? <Select value={f.category} onChange={v => setF({ ...f, category: v })} options={cats.map(c => ({ value: c, label: c }))} /> : <input value={f.category} onChange={e => setF({ ...f, category: e.target.value })} />}</Field>
          </div>}
        </> : <p className="muted small">Connect Pocket Split under Settings → Connected apps to post the receipt as a shared expense.</p>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy || (checked.length === 0 && !f.amount)}>{busy ? 'Saving…' : 'Finish'}</button></div>
      </form>
    </Modal>
  )
}

// ---------- pantry ----------

export function PantryModal({ item, prefill, onClose, onSaved, onDeleted }: { item?: PantryItem; prefill?: Partial<PantryItem>; onClose: () => void; onSaved: (p: PantryItem) => void; onDeleted?: () => void }) {
  const { meta, settings } = useApp()
  const { toast } = useToast()
  const [facets, setFacets] = useState<{ locations: string[]; categories: string[] }>({ locations: [], categories: [] })
  useEffect(() => { api.get<typeof facets>(`${V1}/pantry/facets`).then(setFacets).catch(() => {}) }, [])
  const src = item || prefill
  const [f, setF] = useState({ name: src?.name || '', quantity: src?.quantity ?? 1, unit: src?.unit || '', min_quantity: src?.min_quantity || 0, location: src?.location || settings?.inventory_location || '', category: src?.category || '', barcode: src?.barcode || '', brand: src?.brand || '', expiry_date: src?.expiry_date || '', notes: src?.notes || '', photo_url: src?.photo_url || '', local: false })
  const [busy, setBusy] = useState(false)
  const set = (k: string, v: unknown) => setF(x => ({ ...x, [k]: v }))
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { const p = item ? await api.put<PantryItem>(`${V1}/pantry/${item.id}`, f) : await api.post<PantryItem>(`${V1}/pantry`, f); toast(item ? 'Saved' : 'Added to the pantry'); onSaved(p) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={item ? 'Edit pantry item' : 'Add to the pantry'} onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => set('name', e.target.value)} required autoFocus={!src?.name} /></Field>
        <div className="three-col tight">
          <Field label="Quantity"><input type="number" step="0.01" min={0} value={f.quantity} onChange={e => set('quantity', Number(e.target.value))} /></Field>
          <Field label="Unit"><input value={f.unit} onChange={e => set('unit', e.target.value)} list="punits" placeholder="g, ml, pcs" /><DataList id="punits" items={meta.units} /></Field>
          <Field label="Low below" help="0 = never"><input type="number" step="0.01" min={0} value={f.min_quantity || ''} onChange={e => set('min_quantity', Number(e.target.value))} /></Field>
        </div>
        <div className="two-col tight">
          <Field label="Location"><input value={f.location} onChange={e => set('location', e.target.value)} list="plocs" placeholder="Fridge, Pantry, Freezer" /><DataList id="plocs" items={facets.locations} /></Field>
          <Field label="Category"><input value={f.category} onChange={e => set('category', e.target.value)} list="pcats" placeholder="dairy, spices…" /><DataList id="pcats" items={facets.categories} /></Field>
        </div>
        <div className="three-col tight">
          <Field label="Barcode"><input value={f.barcode} onChange={e => set('barcode', e.target.value)} inputMode="numeric" className="mono" /></Field>
          <Field label="Brand"><input value={f.brand} onChange={e => set('brand', e.target.value)} /></Field>
          <Field label="Best before"><input type="date" value={f.expiry_date} onChange={e => set('expiry_date', e.target.value)} /></Field>
        </div>
        <Field label="Notes"><input value={f.notes} onChange={e => set('notes', e.target.value)} /></Field>
        {!item && settings?.inventory_configured && <label className="check-row"><input type="checkbox" checked={!f.local} onChange={e => set('local', !e.target.checked)} /> Create it in Pocket Inventory too (tag “{settings.inventory_tag}”)</label>}
        {item?.inventory_id && <p className="muted small">Mirrors Pocket Inventory item #{item.inventory_id}; changes are pushed there.</p>}
        <div className="form-actions">
          {item && onDeleted && <button type="button" className="btn ghost danger-text" onClick={async () => { await api.del(`${V1}/pantry/${item.id}`); onDeleted() }}>Remove</button>}
          <span className="spacer" />
          <button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{item ? 'Save' : 'Add'}</button>
        </div>
      </form>
    </Modal>
  )
}

/** Scan or type a barcode, show what it is, add it to the pantry. */
export function ScanModal({ onClose, onAdded }: { onClose: () => void; onAdded: (p: PantryItem) => void }) {
  const { toast } = useToast()
  const [res, setRes] = useState<LookupResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [add, setAdd] = useState<Partial<PantryItem> | null>(null)
  const lookup = async (code: string) => {
    setBusy(true)
    try { setRes(await api.get<LookupResult>(`${V1}/lookup?barcode=${encodeURIComponent(code)}`)) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const plusOne = async (p: PantryItem) => { try { const np = await api.post<PantryItem>(`${V1}/pantry/${p.id}/adjust`, { delta: 1, note: 'Scanned' }); toast(`${np.name}: ${fmtQty(np.quantity, np.unit)}`); onAdded(np) } catch (e) { toast((e as Error).message, 'err') } }
  if (add) return <PantryModal prefill={add} onClose={() => setAdd(null)} onSaved={p => onAdded(p)} />
  return (
    <Modal title="Scan a barcode" onClose={onClose}>
      {!res ? (busy ? <p className="muted">Looking up…</p> : <Scanner onCode={lookup} />) : (
        <div className="grid" style={{ gap: 12 }}>
          {res.source === 'pantry' && res.pantry && <>
            <div className="product-card">{res.pantry.photo_url ? <img src={res.pantry.photo_url} alt="" /> : <div className="ph"><Icon name="basket" size={28} /></div>}<div><b>{res.pantry.name}</b><div className="muted small">Already in the pantry · {fmtQty(res.pantry.quantity, res.pantry.unit)}{res.pantry.location ? ` · ${res.pantry.location}` : ''}</div></div></div>
            <div className="form-actions"><button type="button" className="btn" onClick={() => setRes(null)}>Scan another</button><button type="button" className="btn primary" onClick={() => plusOne(res.pantry!)}>+1 {res.pantry.unit || ''}</button></div>
          </>}
          {res.source === 'inventory' && res.inventory && <>
            <div className="product-card"><div className="ph"><Icon name="package" size={28} /></div><div><b>{res.inventory.name}</b><div className="muted small">In Pocket Inventory (not tagged as pantry) · {fmtQty(res.inventory.quantity, res.inventory.unit)}{res.inventory.location_name ? ` · ${res.inventory.location_name}` : ''}</div></div></div>
            <p className="muted small">Add the pantry tag to that item in Pocket Inventory and sync, or create a separate pantry entry here.</p>
            <div className="form-actions"><button type="button" className="btn" onClick={() => setRes(null)}>Scan another</button><button type="button" className="btn primary" onClick={() => setAdd({ name: res.inventory!.name, barcode: res.barcode, brand: res.inventory!.brand, unit: res.inventory!.unit, quantity: 1 })}>Add to pantry</button></div>
          </>}
          {res.source === 'openfoodfacts' && res.product && <>
            <div className="product-card">{res.product.image_url ? <img src={res.product.image_url} alt="" /> : <div className="ph"><Icon name="cart" size={28} /></div>}<div><b>{res.product.name}</b><div className="muted small">{[res.product.brand, res.product.quantity].filter(Boolean).join(' · ')}</div><div className="muted small">{res.product.categories.slice(-3).join(', ')}</div><div className="muted small">Open Food Facts</div></div></div>
            <div className="form-actions"><button type="button" className="btn" onClick={() => setRes(null)}>Scan another</button><button type="button" className="btn primary" onClick={() => setAdd({ name: res.product!.name, brand: res.product!.brand, barcode: res.barcode, category: res.product!.categories.slice(-1)[0] || '', photo_url: res.product!.image_url, quantity: 1, unit: 'pcs', notes: res.product!.quantity })}>Add to pantry</button></div>
          </>}
          {res.source === 'none' && <>
            <div className="callout"><Icon name="info" size={15} /><span>Nothing known for <code>{res.barcode}</code>{res.error ? ` (${res.error})` : ''}. Add it by hand — the barcode is remembered for next time.</span></div>
            <div className="form-actions"><button type="button" className="btn" onClick={() => setRes(null)}>Scan another</button><button type="button" className="btn primary" onClick={() => setAdd({ barcode: res.barcode, quantity: 1, unit: 'pcs' })}>Add by hand</button></div>
          </>}
        </div>
      )}
    </Modal>
  )
}

// ---------- recipe import ----------

export function ImportRecipeModal({ onClose }: { onClose: () => void }) {
  const { toast } = useToast()
  const navigate = useNavigate()
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { const r = await api.post<Recipe>(`${V1}/recipes/import`, { url }); sessionStorage.setItem('pk-import', JSON.stringify(r)); onClose(); navigate('/recipes/new?import=1') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title="Import a recipe from a website" onClose={onClose}>
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <p className="muted small">Most recipe sites publish their recipe as structured data; the title, times, servings, ingredients, steps and photo come across and you can fix anything before saving.</p>
        <Field label="URL"><input type="url" value={url} onChange={e => setUrl(e.target.value)} placeholder="https://www.bbcgoodfood.com/recipes/…" required autoFocus /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Fetching…' : 'Fetch'}</button></div>
      </form>
    </Modal>
  )
}

/** One to five stars; a second click on the current star clears it. */
export function RatingPick({ value, onChange }: { value: number; onChange: (v: number) => void }) {
  return <div className="rating-pick" role="radiogroup" aria-label="Rating">{[1, 2, 3, 4, 5].map(n => <button type="button" key={n} role="radio" aria-checked={n === value} aria-label={`${n} star${n === 1 ? '' : 's'}`} className={n <= value ? 'on' : ''} onClick={() => onChange(value === n ? 0 : n)}><Icon name="star" size={20} /></button>)}</div>
}

// ---------- shopping list ----------

export function ListModal({ list, onClose, onSaved }: { list?: List; onClose: () => void; onSaved: (l: List) => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: list?.name || '', store: list?.store || '' })
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const l = list ? await api.put<List>(`${V1}/lists/${list.id}`, f) : await api.post<List>(`${V1}/lists`, f); onSaved(l) } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <Modal title={list ? 'Edit list' : 'New list'} onClose={onClose} size="sm">
      <form onSubmit={submit} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required autoFocus placeholder="Groceries, Market, Costco…" /></Field>
        <Field label="Store (optional)"><input value={f.store} onChange={e => setF({ ...f, store: e.target.value })} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary">{list ? 'Save' : 'Create'}</button></div>
      </form>
    </Modal>
  )
}
