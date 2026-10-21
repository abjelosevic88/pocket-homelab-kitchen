import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { cap, fmtQty, type List, type ListItem } from '../lib/types'
import { Card, Confirm, Empty, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { CheckoutModal, ItemModal, ListModal } from '../components/forms'
import { AISLE_ICON, Icon } from '../components/icons'
import { announceChange, useCrumb, useOnChange } from '../components/Layout'
import { Money } from '../components/Money'

export default function Shopping() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { settings, meta } = useApp()
  const { data: lists, reload: reloadLists } = useAsync(() => api.get<List[]>(`${V1}/lists`), [])
  const listId = id ? Number(id) : lists?.[0]?.id
  useEffect(() => { if (!id && lists && lists[0]) navigate(`/shopping/${lists[0].id}`, { replace: true }) }, [id, lists, navigate])
  const { data, loading, reload } = useAsync(() => listId ? api.get<{ list: List; items: ListItem[] }>(`${V1}/lists/${listId}`) : Promise.resolve(null), [listId])
  const [quick, setQuick] = useState('')
  const [sugg, setSugg] = useState<string[]>([])
  const [modal, setModal] = useState<{ kind: 'item'; item?: ListItem } | { kind: 'checkout' } | { kind: 'newlist' } | { kind: 'editlist' } | { kind: 'clear' } | { kind: 'deletelist' } | null>(null)
  useEffect(() => { api.get<string[]>(`${V1}/lists/suggest`).then(setSugg).catch(() => {}) }, [listId])
  // a change here also updates the sidebar counts; a change from the shell reloads the list
  const refresh = () => announceChange()
  useOnChange(() => { reload(); reloadLists() })
  useCrumb(id && data?.list ? data.list.name : undefined)
  if (!lists) return <Loading />
  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!quick.trim() || !listId) return
    try { await api.post(`${V1}/lists/${listId}/items`, { name: quick.trim() }); setQuick(''); refresh() } catch (e) { toast((e as Error).message, 'err') }
  }
  const toggle = async (it: ListItem) => { try { await api.post(`${V1}/lists/${listId}/items/${it.id}/toggle`); refresh() } catch (e) { toast((e as Error).message, 'err') } }
  const addLow = async () => { try { const r = await api.post<{ added: number }>(`${V1}/lists/${listId}/low`); toast(`${r.added} low pantry item${r.added === 1 ? '' : 's'} added`); refresh() } catch (e) { toast((e as Error).message, 'err') } }
  const items = data?.items || []
  const open = items.filter(i => !i.checked), done = items.filter(i => i.checked)
  const byAisle: Record<string, ListItem[]> = {}
  for (const it of open) (byAisle[it.aisle || 'other'] ||= []).push(it)
  const order = [...meta.aisles, 'other'].filter(a => byAisle[a])
  const sum = done.reduce((a, i) => a + (i.price || 0), 0)
  const dec = settings?.home_currency || 'EUR'
  return (
    <>
      <PageHeader title="Shopping" sub={data ? `${open.length} to buy${done.length ? `, ${done.length} in the cart` : ''}` : ''}
        actions={<><button type="button" className="btn" onClick={addLow} title="Add every pantry item that is below its minimum"><Icon name="basket" size={15} />Add low pantry</button><button type="button" className="btn primary" onClick={() => setModal({ kind: 'checkout' })} disabled={done.length === 0}><Icon name="check" size={15} />Done shopping{done.length ? <span className="badge">{done.length}</span> : null}</button></>} />
      <nav className="tabs list-tabs" role="tablist" aria-label="Shopping lists">
        {lists.map(l => <button type="button" role="tab" aria-selected={l.id === listId} key={l.id} className={l.id === listId ? 'active' : ''} onClick={() => navigate(`/shopping/${l.id}`)}><Icon name="list" size={15} />{l.name}{l.open > 0 && <span className="n">{l.open}</span>}</button>)}
        <button type="button" onClick={() => setModal({ kind: 'newlist' })}><Icon name="plus" size={15} />List</button>
      </nav>
      <form onSubmit={add} className="quick-add mb"><Icon name="plus" size={18} strokeWidth={2.2} /><input value={quick} onChange={e => setQuick(e.target.value)} placeholder="Add… “2 kg potatoes”, “milk 1.5 l”, “eggs”" list="sugg" autoComplete="off" /><datalist id="sugg">{sugg.map(s => <option key={s} value={s} />)}</datalist></form>
      {loading && !data ? <Loading /> : items.length === 0 ? <Card><Empty icon="cart" title="The list is empty">Type an item above, add a recipe's ingredients from its page, or build the week's list from the plan.</Empty></Card> : (
        <div className="two-col cols-3-2">
          <Card flush>
            <div style={{ padding: '8px 14px' }}>
              {order.map(a => (
                <div key={a} className="aisle">
                  <h3><Icon name={AISLE_ICON[a] || 'cart'} size={14} />{cap(a)}</h3>
                  {byAisle[a].map(it => <ItemRow key={it.id} it={it} onToggle={() => toggle(it)} onEdit={() => setModal({ kind: 'item', item: it })} />)}
                </div>
              ))}
              {open.length === 0 && <p className="muted" style={{ padding: 10 }}>Everything is in the cart.</p>}
            </div>
          </Card>
          <div className="grid" style={{ gap: 16 }}>
            <Card title={`In the cart (${done.length})`} actions={done.length > 0 && <><button type="button" className="btn ghost sm" onClick={async () => { await api.post(`${V1}/lists/${listId}/check`, { checked: false }); refresh() }}>Uncheck all</button><button type="button" className="btn ghost sm danger-text" onClick={() => setModal({ kind: 'clear' })}>Clear</button></>}>
              {done.length === 0 ? <p className="muted small">Tap the box next to an item when it is in the cart.</p> : done.map(it => <ItemRow key={it.id} it={it} onToggle={() => toggle(it)} onEdit={() => setModal({ kind: 'item', item: it })} />)}
              {sum > 0 && <p className="muted small mt right">Prices so far: <Money minor={sum} code={done.find(i => i.currency)?.currency || dec} /></p>}
            </Card>
            {data && <Card title="List">
              <dl className="kv"><dt>Name</dt><dd>{data.list.name}</dd>{data.list.store && <><dt>Store</dt><dd>{data.list.store}</dd></>}</dl>
              <div className="row wrap mt"><button type="button" className="btn sm" onClick={() => setModal({ kind: 'editlist' })}><Icon name="edit" size={14} />Rename</button>{open.length > 0 && <button type="button" className="btn sm" onClick={async () => { await api.post(`${V1}/lists/${listId}/check`, { checked: true }); refresh() }}><Icon name="check" size={14} />Check all</button>}{lists.length > 1 && <button type="button" className="btn sm ghost danger-text" onClick={() => setModal({ kind: 'deletelist' })}><Icon name="trash" size={14} />Delete list</button>}</div>
            </Card>}
          </div>
        </div>
      )}
      {modal?.kind === 'item' && listId && <ItemModal listId={listId} item={modal.item} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh() }} onDeleted={() => { setModal(null); refresh() }} />}
      {modal?.kind === 'checkout' && data && <CheckoutModal list={data.list} items={items} onClose={() => setModal(null)} onDone={() => { setModal(null); refresh() }} />}
      {modal?.kind === 'newlist' && <ListModal onClose={() => setModal(null)} onSaved={l => { setModal(null); reloadLists(); announceChange(); navigate(`/shopping/${l.id}`) }} />}
      {modal?.kind === 'editlist' && data && <ListModal list={data.list} onClose={() => setModal(null)} onSaved={() => { setModal(null); refresh() }} />}
      {modal?.kind === 'clear' && <Confirm confirmLabel="Remove" title="Remove the checked items?" message="They are removed from the list without a checkout — nothing goes to the pantry or to Pocket Split." onCancel={() => setModal(null)} onConfirm={async () => { await api.post(`${V1}/lists/${listId}/clear`); setModal(null); refresh() }} />}
      {modal?.kind === 'deletelist' && data && <Confirm confirmLabel="Delete" title={`Delete “${data.list.name}”?`} message="The list and everything on it." onCancel={() => setModal(null)} onConfirm={async () => { await api.del(`${V1}/lists/${listId}`); setModal(null); await reloadLists(); announceChange(); navigate('/shopping') }} />}
    </>
  )
}

function ItemRow({ it, onToggle, onEdit }: { it: ListItem; onToggle: () => void; onEdit: () => void }) {
  return (
    <div className={`sl-item ${it.checked ? 'done' : ''}`}>
      <button type="button" className={`chk ${it.checked ? 'on' : ''}`} onClick={onToggle} aria-label={it.checked ? 'Uncheck' : 'Check'} aria-pressed={it.checked}>{it.checked && <Icon name="check" size={14} strokeWidth={2.6} />}</button>
      <div className="nm" onClick={onEdit}><div className="t">{it.name}</div>{(it.note || it.recipe_title) && <div className="s">{[it.note, it.recipe_title ? `for ${it.recipe_title}` : ''].filter(Boolean).join(' · ')}</div>}</div>
      <span className="q">{fmtQty(it.qty, it.unit)}{it.price > 0 && <> · <Money minor={it.price} code={it.currency || 'EUR'} /></>}</span>
    </div>
  )
}
