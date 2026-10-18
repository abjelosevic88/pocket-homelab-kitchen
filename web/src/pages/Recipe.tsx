import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate } from '../lib/format'
import { cap, COURSE_ICON, fmtMinutes, fmtQty, recipeImage, type Cook, type PantryMatch, type Recipe } from '../lib/types'
import { Card, Confirm, Loading, useAsync, useToast } from '../components/ui'
import { AddToListModal, AddToPlanModal, CookModal } from '../components/forms'

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
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger">{error}</div>
  if (!data) return null
  const r = data.recipe
  const serv = servings || r.servings
  const factor = r.servings > 0 ? serv / r.servings : 1
  const img = recipeImage(r)
  const fav = async () => { await api.post(`${V1}/recipes/${r.id}/favorite`); reload() }
  const dup = async () => { const c = await api.post<Recipe>(`${V1}/recipes/${r.id}/duplicate`); toast('Duplicated'); navigate(`/recipes/${c.id}/edit`) }
  const del = async () => { await api.del(`${V1}/recipes/${r.id}`); toast('Recipe deleted'); navigate('/recipes') }
  let group = ''
  return (
    <>
      <div className={`hero ${img ? 'has-cover' : ''}`} style={img ? { backgroundImage: `url(${img})` } : undefined}>
        <div className="hero-body">
          <div>
            <div className="row" style={{ gap: 8, marginBottom: 6, flexWrap: 'wrap' }}><span className="status-pill">{COURSE_ICON[r.course]} {cap(r.course)}</span>{r.cuisine && <span className="status-pill">{r.cuisine}</span>}{r.total_minutes > 0 && <span className="status-pill">⏱ {fmtMinutes(r.total_minutes)}</span>}{r.rating > 0 && <span className="status-pill">{'★'.repeat(r.rating)}</span>}{r.favorite && <span className="status-pill">♥</span>}</div>
            <h1>{r.title}</h1>
            <div className="meta-line">{r.times_cooked > 0 ? <span>cooked {r.times_cooked}× · last {fmtDate(r.last_cooked)}</span> : <span>never cooked</span>}{data.planned.length > 0 && <span>planned {data.planned.map(d => fmtDate(d, false)).join(', ')}</span>}{r.source_url && <a href={r.source_url} target="_blank" rel="noreferrer" style={{ color: '#fff' }}>{r.source_name || 'source'} ↗</a>}</div>
          </div>
          <div className="row">
            <button className="btn ghost" onClick={() => setModal('plan')}>📅 Plan</button>
            <button className="btn ghost" onClick={() => setModal('list')}>🛒 Shop</button>
            <button className="btn primary" onClick={() => setModal('cook')}>🍳 Cooked it</button>
          </div>
        </div>
      </div>
      <div className="row mb" style={{ flexWrap: 'wrap' }}>
        <Link className="btn sm" to={`/recipes/${r.id}/edit`}>Edit</Link>
        <button className="btn sm" onClick={fav}>{r.favorite ? '♥ Favourite' : '♡ Favourite'}</button>
        <button className="btn sm" onClick={dup}>Duplicate</button>
        <a className="btn sm" href={`${V1}/recipes/${r.id}/export.md`}>Markdown</a>
        <button className="btn sm ghost danger-text" onClick={() => setModal('delete')}>Delete</button>
        {r.tags && <span className="chips" style={{ marginLeft: 'auto' }}>{r.tags.split(',').map(t => <Link key={t} className="badge tag" to={`/recipes?tag=${encodeURIComponent(t.trim())}`}>{t.trim()}</Link>)}</span>}
      </div>
      {r.description && <p className="mb" style={{ fontSize: 15 }}>{r.description}</p>}
      <div className="two-col" style={{ gridTemplateColumns: 'minmax(0, 2fr) minmax(0, 3fr)' }}>
        <div className="grid" style={{ gap: 16 }}>
          <Card title="Ingredients" actions={<div className="servings-ctl"><button onClick={() => setServings(Math.max(1, serv - 1))}>−</button><span>{serv} serving{serv === 1 ? '' : 's'}</span><button onClick={() => setServings(serv + 1)}>+</button></div>}>
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
                    <div className="ing-row"><span className={`st ${st}`} title={title}>{st === 'ok' ? '✓' : st === 'short' ? '!' : '○'}</span><span className="q">{fmtQty(ing.qty * factor, ing.unit) || <span className="muted">—</span>}</span><span className="n">{ing.name}{ing.note && <span className="note"> · {ing.note}</span>}{ing.optional && <span className="opt">optional</span>}</span></div>
                  </div>
                )
              })}
            </div>
            <p className="muted small mt">✓ in the pantry · ! not enough · ○ not tracked{settings?.inventory_configured ? ' — pantry lives in Pocket Inventory' : ''}</p>
          </Card>
          {Object.keys(r.nutrition).length > 0 && <Card title="Nutrition (per serving)"><div className="nutri">{Object.entries(r.nutrition).map(([k, v]) => <span key={k}><b>{cap(k)}</b> {v}</span>)}</div></Card>}
          {r.notes && <Card title="Notes"><p style={{ whiteSpace: 'pre-wrap' }}>{r.notes}</p></Card>}
        </div>
        <div className="grid" style={{ gap: 16 }}>
          <Card title="Steps" actions={Object.values(done).some(Boolean) && <button className="btn ghost sm" onClick={() => setDone({})}>Reset</button>}>
            {r.steps.length === 0 ? <p className="muted">No steps written down.</p> : <ol className="steps">{r.steps.map((s, i) => <li key={i} className={done[i] ? 'done' : ''} onClick={() => setDone(d => ({ ...d, [i]: !d[i] }))}>{s}</li>)}</ol>}
          </Card>
          {data.cooks.length > 0 && <Card title="Cooked">
            <div className="inline-list">{data.cooks.map(c => <div key={c.id} className="li"><span>🍳</span><span className="grow">{fmtDate(c.date)}{c.user_name ? ` · ${c.user_name}` : ''}{c.servings ? ` · ${c.servings} servings` : ''}{c.notes ? ` · ${c.notes}` : ''}</span>{c.rating > 0 && <span className="stars">{'★'.repeat(c.rating)}</span>}{c.habit_marked && <span className="badge" title="Marked in Pocket Habits">habit</span>}</div>)}</div>
          </Card>}
        </div>
      </div>
      {modal === 'cook' && <CookModal recipe={r} onClose={() => setModal(null)} onDone={() => { setModal(null); reload() }} />}
      {modal === 'plan' && <AddToPlanModal recipe={r} onClose={() => setModal(null)} onSaved={() => { setModal(null); reload() }} />}
      {modal === 'list' && <AddToListModal recipe={r} onClose={() => setModal(null)} onDone={id => { setModal(null); navigate(`/shopping/${id}`) }} />}
      {modal === 'delete' && <Confirm title={`Delete “${r.title}”?`} message="The recipe, its photo and its cook history are removed. Plan entries keep their text." onCancel={() => setModal(null)} onConfirm={del} />}
    </>
  )
}
