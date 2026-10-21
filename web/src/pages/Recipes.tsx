import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { cap, fmtMinutes, recipeImage, type Recipe } from '../lib/types'
import { Empty, Loading, PageHeader, useAsync, useDebounce } from '../components/ui'
import { Select } from '../components/Select'
import { ImportRecipeModal } from '../components/forms'
import { COURSE_ICON, Icon } from '../components/icons'
import { announceChange, useOnChange } from '../components/Layout'

export default function Recipes() {
  const { meta } = useApp()
  const [sp, setSp] = useSearchParams()
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q, 250)
  const course = sp.get('course') || ''
  const tag = sp.get('tag') || ''
  const sort = sp.get('sort') || 'title'
  const fav = sp.get('favorite') === '1'
  const [imp, setImp] = useState(false)
  const setParam = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }) }
  const { data: facets } = useAsync(() => api.get<{ tags: string[]; cuisines: string[]; courses: Record<string, number> }>(`${V1}/recipes/facets`), [])
  const url = `${V1}/recipes?q=${encodeURIComponent(dq)}&course=${course}&tag=${encodeURIComponent(tag)}&sort=${sort}&favorite=${fav ? 1 : 0}`
  const { data, loading, reload } = useAsync(() => api.get<Recipe[]>(url), [url])
  useOnChange(() => { reload() })
  useEffect(() => { setParam('q', dq) }, [dq]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <>
      <PageHeader title="Recipes" sub={data ? `${data.length} recipe${data.length === 1 ? '' : 's'}` : ''} actions={<><button type="button" className="btn" onClick={() => setImp(true)}><Icon name="link" size={15} />Import from a link</button><Link className="btn primary" to="/recipes/new"><Icon name="plus" size={15} />New recipe</Link></>} />
      <div className="toolbar">
        <div className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search title, ingredient, tag…" /></div>
        <Select value={sort} onChange={v => setParam('sort', v)} options={[{ value: 'title', label: 'A → Z' }, { value: 'cooked', label: 'Recently cooked' }, { value: 'popular', label: 'Most cooked' }, { value: 'rating', label: 'Best rated' }, { value: 'time', label: 'Quickest' }, { value: 'new', label: 'Newest' }]} ariaLabel="Sort" />
        {facets && facets.tags.length > 0 && <Select value={tag} onChange={v => setParam('tag', v)} options={facets.tags.map(t => ({ value: t, label: '#' + t }))} placeholder="Any tag" clearLabel="Any tag" ariaLabel="Tag" />}
        <button type="button" className={`chip ${fav ? 'active' : ''}`} onClick={() => setParam('favorite', fav ? '' : '1')}><Icon name="heart" size={14} />Favourites</button>
      </div>
      <div className="filters">
        <button type="button" className={`chip ${!course ? 'active' : ''}`} onClick={() => setParam('course', '')}>All</button>
        {meta.courses.map(c => <button type="button" key={c} className={`chip ${course === c ? 'active' : ''}`} onClick={() => setParam('course', c)}><Icon name={COURSE_ICON[c] || 'utensils'} size={14} />{cap(c)}{facets?.courses[c] ? <span className="n">{facets.courses[c]}</span> : null}</button>)}
      </div>
      {loading && !data ? <Loading /> : !data || data.length === 0 ? <Empty icon="book" title={q || course || tag || fav ? 'No recipes match' : 'No recipes yet'}>{!q && !course && !tag && !fav && 'Add one by hand or paste a link to any recipe site.'}</Empty> : (
        <div className="recipe-grid">{data.map(r => <RecipeCard key={r.id} r={r} onChanged={announceChange} />)}</div>
      )}
      {imp && <ImportRecipeModal onClose={() => setImp(false)} />}
    </>
  )
}

export function RecipeCard({ r, reason, onChanged }: { r: Recipe; reason?: string; onChanged?: () => void }) {
  const navigate = useNavigate()
  const img = recipeImage(r)
  const fav = async (e: React.MouseEvent) => { e.preventDefault(); e.stopPropagation(); await api.post(`${V1}/recipes/${r.id}/favorite`); onChanged?.() }
  return (
    <Link className="recipe-card" to={`/recipes/${r.id}`} onClick={e => { if ((e.target as HTMLElement).closest('.fav')) e.preventDefault() }}>
      <div className={`cover ${img ? 'has-img' : ''}`} style={img ? { backgroundImage: `url("${img}")` } : undefined}><Icon name={COURSE_ICON[r.course] || 'utensils'} size={40} strokeWidth={1.4} /><button type="button" className={`fav ${r.favorite ? 'on' : ''}`} onClick={fav} title={r.favorite ? 'Unfavourite' : 'Favourite'} aria-label="Favourite" aria-pressed={r.favorite}><Icon name="heart" size={15} strokeWidth={2} /></button></div>
      <div className="body">
        <h3>{r.title}</h3>
        <div className="facts">{r.total_minutes > 0 && <span><Icon name="clock" size={13} />{fmtMinutes(r.total_minutes)}</span>}<span><Icon name="users" size={13} />{r.servings}</span>{r.rating > 0 && <span className="stars">{'★'.repeat(r.rating)}</span>}{r.times_cooked > 0 && <span>cooked {r.times_cooked}×</span>}</div>
        {reason ? <div className="reason">{reason}</div> : r.tags ? <div className="chips">{r.tags.split(',').slice(0, 3).map(t => <span key={t} className="badge tag" onClick={e => { e.preventDefault(); navigate(`/recipes?tag=${encodeURIComponent(t.trim())}`) }}>{t.trim()}</span>)}</div> : null}
      </div>
    </Link>
  )
}
