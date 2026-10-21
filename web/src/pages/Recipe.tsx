import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate } from '../lib/format'
import { cap, fmtMinutes, fmtQty, recipeImage, type Cook, type PantryMatch, type Recipe } from '../lib/types'
import { Card, Confirm, Loading, useAsync, useToast } from '../components/ui'
import { AddToListModal, AddToPlanModal, CookModal } from '../components/forms'
import { COURSE_ICON, Icon } from '../components/icons'
import { announceChange, useCrumb } from '../components/Layout'

type Detail = { recipe: Recipe; cooks: Cook[]; planned: string[]; pantry: (PantryMatch | null)[] }

export default function RecipePage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, error, loading, reload } = useAsync(() => api.get<Detail>(`${V1}/recipes/${id}`), [id])
  const [servings, setServings] = useState(0)
  const [done, setDone] = useState<Record<number, boolean>>({})
  const [modal, setModal] = useState<'cook' | 'plan' | 'list' | 'delete' | null>(null)
  useCrumb(data?.recipe.title)
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return null
  const r = data.recipe
  const serv = servings || r.servings
  const factor = r.servings > 0 ? serv / r.servings : 1
  const img = recipeImage(r)
  const fav = async () => { await api.post(`${V1}/recipes/${r.id}/favorite`); reload(); announceChange() }
  const dup = async () => { const c = await api.post<Recipe>(`${V1}/recipes/${r.id}/duplicate`); toast('Duplicated'); navigate(`/recipes/${c.id}/edit`) }
  const del = async () => { await api.del(`${V1}/recipes/${r.id}`); toast('Recipe deleted'); announceChange(); navigate('/recipes') }
  let group = ''
  return (
    <>
      <div className={`hero ${img ? 'has-cover' : ''}`} style={img ? { backgroundImage: `url("${img}")` } : undefined}>
        <div className="hero-body">
          <div>
            <div className="row" style={{ gap: 8, marginBottom: 6, flexWrap: 'wrap' }}><span className="status-pill"><Icon name={COURSE_ICON[r.course] || 'utensils'} size={12} strokeWidth={2.2} />{cap(r.course)}</span>{r.cuisine && <span className="status-pill">{r.cuisine}</span>}{r.total_minutes > 0 && <span className="status-pill"><Icon name="clock" size={12} strokeWidth={2.2} />{fmtMinutes(r.total_minutes)}</span>}{r.rating > 0 && <span className="status-pill">{'★'.repeat(r.rating)}</span>}{r.favorite && <span className="status-pill"><Icon name="heart" size={12} strokeWidth={2.2} />favourite</span>}</div>
            <h1>{r.title}</h1>
            <div className="meta-line">{r.times_cooked > 0 ? <span>cooked {r.times_cooked}× · last {fmtDate(r.last_cooked)}</span> : <span>never cooked</span>}{data.planned.length > 0 && <span>planned {data.planned.map(d => fmtDate(d, false)).join(', ')}</span>}{r.source_url && <a href={r.source_url} target="_blank" rel="noreferrer">{r.source_name || 'source'} ↗</a>}</div>
          </div>
          <div className="row">
            <button type="button" className="btn ghost" onClick={() => setModal('plan')}><Icon name="calendar-plus" size={15} />Plan</button>
            <button type="button" className="btn ghost" onClick={() => setModal('list')}><Icon name="cart" size={15} />Shop</button>
            <button type="button" className="btn primary" onClick={() => setModal('cook')}><Icon name="flame" size={15} />Cooked it</button>
          </div>
        </div>
      </div>
      <div className="recipe-actions">
        <Link className="btn sm" to={`/recipes/${r.id}/edit`}><Icon name="edit" size={14} />Edit</Link>
        <button type="button" className={`btn sm ${r.favorite ? 'active' : ''}`} onClick={fav} aria-pressed={r.favorite}><Icon name="heart" size={14} className={r.favorite ? 'danger-text' : ''} />{r.favorite ? 'Favourite' : 'Add to favourites'}</button>
        <button type="button" className="btn sm" onClick={dup}><Icon name="copy" size={14} />Duplicate</button>
        <a className="btn sm" href={`${V1}/recipes/${r.id}/export.md`}><Icon name="download" size={14} />Markdown</a>
        <button type="button" className="btn sm ghost danger-text" onClick={() => setModal('delete')}><Icon name="trash" size={14} />Delete</button>
        {r.tags && <span className="chips">{r.tags.split(',').map(t => <Link key={t} className="badge tag" to={`/recipes?tag=${encodeURIComponent(t.trim())}`}>{t.trim()}</Link>)}</span>}
      </div>
      {r.description && <p className="recipe-desc">{r.description}</p>}
      <div className="two-col cols-2-3">
        <div className="grid" style={{ gap: 16 }}>
          <Card title="Ingredients" actions={<div className="servings-ctl"><button type="button" aria-label="Fewer servings" onClick={() => setServings(Math.max(1, serv - 1))}><Icon name="minus" size={14} /></button><span>{serv} serving{serv === 1 ? '' : 's'}</span><button type="button" aria-label="More servings" onClick={() => setServings(serv + 1)}><Icon name="plus" size={14} /></button></div>}>
            {(r.prep_minutes > 0 || r.cook_minutes > 0) && <p className="muted small mb">{r.prep_minutes > 0 && `prep ${fmtMinutes(r.prep_minutes)}`}{r.prep_minutes > 0 && r.cook_minutes > 0 && ' · '}{r.cook_minutes > 0 && `cook ${fmtMinutes(r.cook_minutes)}`}</p>}
            <div className="ing-list">
              {r.ingredients.map((ing, i) => {
                const head = ing.group && ing.group !== group ? (group = ing.group) : null
                const m = data.pantry[i]
                const st = !m ? 'none' : m.enough ? 'ok' : 'short'
                const title = !m ? 'Not in the pantry' : m.enough ? `In the pantry: ${fmtQty(m.quantity, m.unit)} ${m.name}` : `Short: ${fmtQty(m.quantity, m.unit)} of ${m.name} in the pantry`
                return (
                  <div key={i}>
                    {head && <div className="ing-group">{head}</div>}
                    <div className="ing-row"><span className={`st ${st}`} title={title}><Icon name={st === 'ok' ? 'check' : st === 'short' ? 'alert' : 'circle'} size={st === 'none' ? 10 : 12} strokeWidth={2.4} /></span><span className="q">{fmtQty(ing.qty * factor, ing.unit) || <span className="muted">—</span>}</span><span className="n">{ing.name}{ing.note && <span className="note"> · {ing.note}</span>}{ing.optional && <span className="opt">optional</span>}</span></div>
                  </div>
                )
              })}
            </div>
            <p className="muted small mt ing-legend"><span><Icon name="check" size={12} className="ok-text" strokeWidth={2.4} />in the pantry</span><span><Icon name="alert" size={12} className="warn-text" strokeWidth={2.4} />not enough</span><span><Icon name="circle" size={10} />not tracked</span>{settings?.inventory_configured ? <span>· pantry lives in Pocket Inventory</span> : null}</p>
          </Card>
          {Object.keys(r.nutrition).length > 0 && <Card title="Nutrition (per serving)"><div className="nutri">{Object.entries(r.nutrition).map(([k, v]) => <span key={k}><b>{cap(k)}</b> {v}</span>)}</div></Card>}
          {r.notes && <Card title="Notes"><p style={{ whiteSpace: 'pre-wrap' }}>{r.notes}</p></Card>}
        </div>
        <div className="grid" style={{ gap: 16 }}>
          <Card title="Steps" actions={Object.values(done).some(Boolean) && <button type="button" className="btn ghost sm" onClick={() => setDone({})}><Icon name="undo" size={14} />Reset</button>}>
            {r.steps.length === 0 ? <p className="muted">No steps written down.</p> : <ol className="steps">{r.steps.map((s, i) => <li key={i} className={done[i] ? 'done' : ''} onClick={() => setDone(d => ({ ...d, [i]: !d[i] }))}>{s}</li>)}</ol>}
          </Card>
          {data.cooks.length > 0 && <Card title="Cooked">
            <div className="inline-list">{data.cooks.map(c => <div key={c.id} className="li"><Icon name="pot" size={15} /><span className="grow">{fmtDate(c.date)}{c.user_name ? ` · ${c.user_name}` : ''}{c.servings ? ` · ${c.servings} servings` : ''}{c.notes ? ` · ${c.notes}` : ''}</span>{c.rating > 0 && <span className="stars">{'★'.repeat(c.rating)}</span>}{c.habit_marked && <span className="badge ok" title="Marked in Pocket Habits">habit</span>}</div>)}</div>
          </Card>}
        </div>
      </div>
      {modal === 'cook' && <CookModal recipe={r} onClose={() => setModal(null)} onDone={() => { setModal(null); reload(); announceChange() }} />}
      {modal === 'plan' && <AddToPlanModal recipe={r} onClose={() => setModal(null)} onSaved={() => { setModal(null); reload(); announceChange() }} />}
      {modal === 'list' && <AddToListModal recipe={r} onClose={() => setModal(null)} onDone={id => { setModal(null); navigate(`/shopping/${id}`) }} />}
      {modal === 'delete' && <Confirm confirmLabel="Delete" title={`Delete “${r.title}”?`} message="The recipe, its photo and its cook history are removed. Plan entries keep their text." onCancel={() => setModal(null)} onConfirm={del} />}
    </>
  )
}
