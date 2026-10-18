import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate, weekday } from '../lib/format'
import { fmtMinutes, fmtQty, recipeImage, SLOT_ICON, type HomeData, type PlanEntry } from '../lib/types'
import { Card, Empty, Loading, PageHeader, useAsync } from '../components/ui'
import { Money } from '../components/Money'
import { CookModal, PlanModal } from '../components/forms'
import { RecipeCard } from './Recipes'

export default function Home() {
  const { user } = useApp()
  const navigate = useNavigate()
  const { data, error, loading, reload } = useAsync(() => api.get<HomeData>(`${V1}/home`), [])
  const [cook, setCook] = useState<PlanEntry | null>(null)
  const [plan, setPlan] = useState(false)
  if (loading && !data) return <Loading />
  if (error) return <div className="callout danger">{error}</div>
  if (!data) return null
  const { stats, week, today } = data
  const hour = new Date().getHours()
  const greet = hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening'
  const first = (user?.name || '').split(' ')[0] || 'there'
  const todays = week.filter(e => e.date === today)
  const next = todays.find(e => !e.cooked_at) || week.find(e => !e.cooked_at)
  const byDay: Record<string, PlanEntry[]> = {}
  for (const e of week) (byDay[e.date] ||= []).push(e)
  const days = Object.keys(byDay).sort().filter(d => d !== today)
  return (
    <>
      <PageHeader title={`${greet}, ${first}`} sub={todays.length ? `${todays.length} meal${todays.length === 1 ? '' : 's'} planned today.` : 'Nothing planned for today yet.'}
        actions={<><button className="btn" onClick={() => setPlan(true)}>📅 Plan a meal</button><Link className="btn" to="/shopping">🛒 List{stats.shopping_open ? ` (${stats.shopping_open})` : ''}</Link><Link className="btn primary" to="/recipes/new">+ Recipe</Link></>} />

      {next ? (
        <div className={`hero ${next.recipe_has_photo || next.recipe_image_url ? 'has-cover' : ''}`} style={next.recipe_id ? { backgroundImage: next.recipe_has_photo ? `url(${V1}/recipes/${next.recipe_id}/photo)` : next.recipe_image_url ? `url(${next.recipe_image_url})` : undefined } : undefined} onClick={() => next.recipe_id && navigate(`/recipes/${next.recipe_id}`)} role={next.recipe_id ? 'link' : undefined}>
          <div className="hero-body">
            <div>
              <div className="row" style={{ gap: 8, marginBottom: 6 }}><span className="status-pill">{next.date === today ? 'Today' : weekday(next.date, true)} · {SLOT_ICON[next.slot]} {next.slot}</span>{next.recipe_minutes ? <span className="status-pill">⏱ {fmtMinutes(next.recipe_minutes)}</span> : null}</div>
              <h1>{next.recipe_title || next.title}</h1>
              <div className="meta-line">{next.servings ? <span>{next.servings} servings</span> : next.recipe_servings ? <span>{next.recipe_servings} servings</span> : null}{next.notes && <span>{next.notes}</span>}</div>
            </div>
            <div className="row" onClick={e => e.stopPropagation()}>
              {next.recipe_id && <Link className="btn ghost" to={`/recipes/${next.recipe_id}`}>Recipe</Link>}
              <button className="btn ghost" onClick={() => setCook(next)}>🍳 Cooked it</button>
            </div>
          </div>
        </div>
      ) : (
        <Card className="mb"><Empty title="Nothing on the plan">Plan this week's meals and the shopping list writes itself.</Empty></Card>
      )}

      <div className="stats">
        <Link className="stat" to="/shopping"><div className="n">{stats.shopping_open}</div><div className="l">to buy</div></Link>
        <Link className="stat" to="/pantry?low=1"><div className="n">{stats.pantry_low}<span className="muted" style={{ fontSize: 14 }}> low</span></div><div className="l">{stats.pantry_expiring} expiring · {stats.pantry} in the pantry</div></Link>
        <div className="stat"><div className="n">{stats.cooked_month}</div><div className="l">cooked this month · {stats.cooked_days_30} of the last 30 days</div></div>
        <div className="stat"><div className="n"><Money minor={stats.spend_month} code={stats.currency} compact /></div><div className="l">groceries this month</div></div>
      </div>

      <div className="home-grid">
        <div>
          {todays.length > 0 && <Card title="Today" className="mb" actions={<Link className="btn ghost sm" to="/plan">Week</Link>}>
            {todays.map(e => <TodayMeal key={e.id} e={e} onCook={() => setCook(e)} />)}
          </Card>}
          {days.length > 0 && <Card title="Coming up" className="mb" actions={<Link className="btn ghost sm" to="/plan">Plan</Link>}>
            <div className="week-strip">{days.map(d => <div key={d} className="wd"><b>{weekday(d)} {fmtDate(d, false)}</b>{byDay[d].map(e => <div key={e.id} className="m" title={e.recipe_title || e.title}>{SLOT_ICON[e.slot]} {e.recipe_title || e.title}</div>)}</div>)}</div>
          </Card>}
          <h2 className="mb-sm">Cook something</h2>
          <div className="recipe-grid mb">{data.suggestions.map(r => <RecipeCard key={r.id} r={r} reason={r.reason} onChanged={reload} />)}</div>
        </div>
        <div className="grid" style={{ gap: 16 }}>
          {(data.expiring.length > 0 || data.low.length > 0) && <Card title="Pantry" actions={<Link className="btn ghost sm" to="/pantry">All</Link>}>
            {data.expiring.length > 0 && <><div className="muted small mb-sm">Use soon</div><div className="inline-list mb">{data.expiring.map(p => <div key={p.id} className="li"><span className="grow">{p.name}</span><span className={`expiry ${p.expiry_date < today ? 'past' : ''}`}>{p.expiry_date < today ? 'expired' : fmtDate(p.expiry_date, false)}</span></div>)}</div></>}
            {data.low.length > 0 && <><div className="muted small mb-sm">Running low</div><div className="inline-list">{data.low.map(p => <div key={p.id} className="li"><span className="grow">{p.name}</span><span className="muted small">{fmtQty(p.quantity, p.unit)}</span></div>)}</div></>}
          </Card>}
          <Card title="Recently cooked">
            {data.cooks.length === 0 ? <p className="muted">Press “Cooked it” on a recipe and it lands here.</p> : (
              <div className="inline-list">{data.cooks.map(c => <Link key={c.id} className="li" to={c.recipe_id ? `/recipes/${c.recipe_id}` : '/plan'} style={{ color: 'inherit' }}><span>🍳</span><span className="grow"><b>{c.recipe_title || c.notes || 'Something'}</b>{c.user_name && <span className="muted small"> · {c.user_name}</span>}</span><span className="muted small">{fmtDate(c.date, false)}</span>{c.rating > 0 && <span className="stars">{'★'.repeat(c.rating)}</span>}</Link>)}</div>
            )}
          </Card>
          {data.lists.length > 0 && <Card title="Shopping lists">
            <div className="inline-list">{data.lists.map(l => <Link key={l.id} className="li" to={`/shopping/${l.id}`} style={{ color: 'inherit' }}><span>🛒</span><span className="grow"><b>{l.name}</b>{l.store && <span className="muted small"> · {l.store}</span>}</span><span className="muted small">{l.open} open{l.checked ? `, ${l.checked} in the cart` : ''}</span></Link>)}</div>
          </Card>}
        </div>
      </div>

      {cook && <CookModal entry={cook} onClose={() => setCook(null)} onDone={() => { setCook(null); reload() }} />}
      {plan && <PlanModal onClose={() => setPlan(false)} onSaved={() => { setPlan(false); reload() }} />}
    </>
  )
}

export function TodayMeal({ e, onCook }: { e: PlanEntry; onCook: () => void }) {
  const img = e.recipe_id ? (e.recipe_has_photo ? `${V1}/recipes/${e.recipe_id}/photo` : e.recipe_image_url) : ''
  return (
    <div className="today-meal">
      <Link to={e.recipe_id ? `/recipes/${e.recipe_id}` : '/plan'} className="img" style={img ? { backgroundImage: `url(${img})` } : undefined}>{!img && (SLOT_ICON[e.slot] || '🍽')}</Link>
      <div><div className="t">{e.recipe_title || e.title}</div><div className="s">{SLOT_ICON[e.slot]} {e.slot}{e.recipe_minutes ? ` · ${fmtMinutes(e.recipe_minutes)}` : ''}{e.servings ? ` · ${e.servings} servings` : ''}{e.notes ? ` · ${e.notes}` : ''}</div></div>
      {e.cooked_at ? <span className="badge cat">cooked</span> : <button className="btn sm" onClick={onCook}>Cooked it</button>}
    </div>
  )
}

export { recipeImage }
