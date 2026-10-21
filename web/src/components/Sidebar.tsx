// Left column: create button, sections with counts, shopping lists, favourite recipes, settings — the Pocket Calendar sidebar.
import { type ReactNode, useCallback, useEffect, useState } from 'react'
import { Link, NavLink, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { recipeImage, type List, type Recipe } from '../lib/types'
import { COURSE_ICON, Icon } from './icons'
import { createItems, useOnChange, type CreateKind } from './Layout'
import { type Anchor, type MenuItem, Menu, rectOf } from './ui'

/** GET /api/v1/dashboard — the numbers the sidebar shows. */
export interface Counts { today_count: number; recipes: number; shopping_open: number; pantry_total: number; pantry_low: number; pantry_expiring: number; planned_week: number }

export function Sidebar({ onCreate, onNavigate, counts, lists }: { onCreate: (k: CreateKind) => void; onNavigate: () => void; counts: Counts | null; lists: List[] }) {
  const { status, settings } = useApp()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [favs, setFavs] = useState<Recipe[] | null>(null)
  const loadFavs = useCallback(() => { api.get<Recipe[]>(`${V1}/recipes?favorite=1&sort=cooked`).then(setFavs).catch(() => setFavs([])) }, [])
  useEffect(() => { loadFavs() }, [loadFavs])
  useOnChange(loadFavs)
  const title = settings?.title || status?.title || 'Pocket Kitchen'
  const pantryAlert = (counts?.pantry_low || 0) + (counts?.pantry_expiring || 0)
  const nav: { to: string; label: string; icon: string; end?: boolean; badge?: number; badgeKind?: string; badgeTitle?: string; count?: number }[] = [
    { to: '/', label: 'Home', icon: 'home', end: true },
    { to: '/recipes', label: 'Recipes', icon: 'book', count: counts?.recipes },
    { to: '/plan', label: 'Meal plan', icon: 'calendar', badge: counts?.today_count, badgeKind: 'accent', badgeTitle: 'meals planned today' },
    { to: '/shopping', label: 'Shopping', icon: 'cart', badge: counts?.shopping_open, badgeKind: '', badgeTitle: 'items to buy' },
    { to: '/pantry', label: 'Pantry', icon: 'basket', badge: pantryAlert, badgeKind: 'warn', badgeTitle: `${counts?.pantry_low || 0} running low · ${counts?.pantry_expiring || 0} to use soon` },
  ]
  return (
    <aside className="sidebar" aria-label="Kitchen">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Kitchen' ? title : <>Pocket <b>Kitchen</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^\d/.test(status.version) ? 'v' + status.version : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />New<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={createItems(onCreate)} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.badge && <span className={`badge ${n.badgeKind || ''}`} title={`${n.badge} ${n.badgeTitle || ''}`}>{n.badge}</span>}
            {!n.badge && !!n.count && <span className="sb-count">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <ListGroup lists={lists} onNavigate={onNavigate} add={() => onCreate('list')} />
      <FavGroup favs={favs} onNavigate={onNavigate} />

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pk:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

function Group({ title, add, addLabel, children }: { title: string; add?: () => void; addLabel?: string; children: ReactNode }) {
  const key = `pk-sb-${title}`
  const [open, setOpen] = useState(() => localStorage.getItem(key) !== '0')
  const toggle = () => setOpen(o => { localStorage.setItem(key, o ? '0' : '1'); return !o })
  return (
    <section className="sb-sec">
      <h4 className="sb-h">
        <button type="button" className="sb-h-btn" aria-expanded={open} onClick={toggle}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />{title}</button>
        {add && <button type="button" className="btn ghost icon-only xs" aria-label={addLabel} title={addLabel} onClick={add}><Icon name="plus" size={14} /></button>}
      </h4>
      {open && children}
    </section>
  )
}

function ListGroup({ lists, onNavigate, add }: { lists: List[]; onNavigate: () => void; add: () => void }) {
  return (
    <Group title="Shopping lists" add={add} addLabel="New shopping list">
      {lists.length === 0
        ? <p className="sb-empty">No lists yet. <button type="button" className="link-btn" onClick={add}>Start one</button></p>
        : <ul className="cal-list">{lists.map(l => <ListRow key={l.id} l={l} onNavigate={onNavigate} />)}</ul>}
    </Group>
  )
}

function ListRow({ l, onNavigate }: { l: List; onNavigate: () => void }) {
  const navigate = useNavigate()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const items: MenuItem[] = [
    { label: 'Open', icon: 'cart', onSelect: () => { onNavigate(); navigate(`/shopping/${l.id}`) } },
    { label: 'Add the low pantry items', icon: 'basket', onSelect: async () => { await api.post(`${V1}/lists/${l.id}/low`).catch(() => {}); onNavigate(); navigate(`/shopping/${l.id}`); window.dispatchEvent(new CustomEvent('pk:changed')) } },
  ]
  return (
    <li className="cal-row sb-row">
      <NavLink to={`/shopping/${l.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={[l.name, l.store].filter(Boolean).join(' · ')}>
        <span className="sb-ico"><Icon name="cart" size={13} /></span>
        <span className="cal-name ellipsis">{l.name}</span>
        {l.open > 0 && <span className="sb-count" title={`${l.open} to buy`}>{l.open}</span>}
      </NavLink>
      <button type="button" className="btn ghost icon-only xs cal-more" aria-label={`Options for ${l.name}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(rectOf(e.currentTarget))}><Icon name="more" size={15} /></button>
      {menu && <Menu anchor={menu} items={items} onClose={() => setMenu(null)} width={230} />}
    </li>
  )
}

function FavGroup({ favs, onNavigate }: { favs: Recipe[] | null; onNavigate: () => void }) {
  if (!favs) return null
  const shown = favs.slice(0, 10)
  return (
    <Group title="Favourites">
      {shown.length === 0
        ? <p className="sb-empty">Tap the heart on a recipe to keep it here.</p>
        : <ul className="cal-list">
            {shown.map(r => {
              const img = recipeImage(r)
              return (
                <li key={r.id} className="cal-row sb-row">
                  <NavLink to={`/recipes/${r.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={r.title}>
                    <span className={`sb-ico ${img ? 'img' : ''}`} style={img ? { backgroundImage: `url("${img}")` } : undefined}><Icon name={COURSE_ICON[r.course] || 'utensils'} size={13} /></span>
                    <span className="cal-name ellipsis">{r.title}</span>
                  </NavLink>
                </li>
              )
            })}
            {favs.length > shown.length && <li><Link className="sb-empty link-btn" to="/recipes?favorite=1" onClick={onNavigate}>All {favs.length} favourites</Link></li>}
          </ul>}
    </Group>
  )
}
