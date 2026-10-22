// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { List } from '../lib/types'
import { Icon } from './icons'
import { Sidebar, type Counts } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { ImportRecipeModal, ItemModal, ListModal, PantryModal, PlanModal, ScanModal } from './forms'
import { Avatar, type Anchor, type MenuItem, Menu, MenuButton, modalOpen, rectOf, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pk-sidebar'

/** Something was added or changed from the shell (or a page): the sidebar counts and the pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pk:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('pk:changed', h)
    return () => window.removeEventListener('pk:changed', h)
  }, [])
}

/** The page's own name for the top bar breadcrumb (a recipe's title, a list's name). */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

export type CreateKind = 'recipe' | 'import' | 'plan' | 'item' | 'list' | 'pantry' | 'scan'

/** The create menu: the sidebar's "New" button and the phone's + button. */
export function createItems(onCreate: (k: CreateKind) => void): MenuItem[] {
  return [
    { label: 'Recipe', icon: 'book', hint: 'N', onSelect: () => onCreate('recipe') },
    { label: 'Import a recipe from a link', icon: 'link', hint: 'I', onSelect: () => onCreate('import') },
    'sep',
    { label: 'Plan a meal', icon: 'calendar-plus', hint: 'M', onSelect: () => onCreate('plan') },
    { label: 'Add to the shopping list', icon: 'cart', hint: 'A', onSelect: () => onCreate('item') },
    { label: 'New shopping list', icon: 'list', onSelect: () => onCreate('list') },
    'sep',
    { label: 'Pantry item', icon: 'basket', hint: 'E', onSelect: () => onCreate('pantry') },
    { label: 'Scan a barcode', icon: 'scan', hint: 'B', onSelect: () => onCreate('scan') },
  ]
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])

  // sidebar numbers: today's meals, open shopping lines, pantry low/expiring, and the lists
  const [counts, setCounts] = useState<Counts | null>(null)
  const [lists, setLists] = useState<List[]>([])
  const loadCounts = useCallback(() => {
    api.get<Counts>(`${V1}/dashboard`).then(setCounts).catch(() => {})
    api.get<List[]>(`${V1}/lists`).then(setLists).catch(() => {})
  }, [])
  useEffect(() => { loadCounts() }, [loc.pathname, loadCounts])
  useOnChange(loadCounts)

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [modal, setModal] = useState<CreateKind | null>(null)
  const [fabMenu, setFabMenu] = useState<Anchor | null>(null)

  const create = (k: CreateKind) => {
    closeDrawer()
    if (k === 'recipe') { navigate('/recipes/new'); return }
    setModal(k)
  }
  const createRef = useRef(create)
  createRef.current = create

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('pk:shortcuts', open)
    return () => window.removeEventListener('pk:shortcuts', open)
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      const mk = (k: CreateKind) => { e.preventDefault(); createRef.current(k) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'n': mk('recipe'); break
        case 'i': mk('import'); break
        case 'm': mk('plan'); break
        case 'a': mk('item'); break
        case 'e': mk('pantry'); break
        case 'b': mk('scan'); break
        case 'h': go('/'); break
        case 'r': go('/recipes'); break
        case 'w': go('/plan'); break
        case 's': go('/shopping'); break
        case 'p': go('/pantry'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'recipe', label: 'New recipe', icon: 'book', hint: 'N', keywords: 'add write', run: () => create('recipe') },
    { id: 'import', label: 'Import a recipe from a link', icon: 'link', hint: 'I', keywords: 'url website fetch', run: () => create('import') },
    { id: 'plan', label: 'Plan a meal', icon: 'calendar-plus', hint: 'M', keywords: 'meal plan week', run: () => create('plan') },
    { id: 'item', label: 'Add to the shopping list', icon: 'cart', hint: 'A', keywords: 'buy groceries', run: () => create('item') },
    { id: 'list', label: 'New shopping list', icon: 'list', keywords: 'store', run: () => create('list') },
    { id: 'pantry', label: 'Add a pantry item', icon: 'basket', hint: 'E', keywords: 'stock fridge', run: () => create('pantry') },
    { id: 'scan', label: 'Scan a barcode', icon: 'scan', hint: 'B', keywords: 'camera ean', run: () => create('scan') },
    { id: 'home', label: 'Home', icon: 'home', hint: 'H', run: () => navigate('/') },
    { id: 'recipes', label: 'Recipes', icon: 'book', hint: 'R', run: () => navigate('/recipes') },
    { id: 'favs', label: 'Favourite recipes', icon: 'heart', run: () => navigate('/recipes?favorite=1') },
    { id: 'week', label: 'Meal plan', icon: 'calendar', hint: 'W', keywords: 'week', run: () => navigate('/plan') },
    { id: 'shopping', label: 'Shopping', icon: 'cart', hint: 'S', run: () => navigate('/shopping') },
    { id: 'pantry-page', label: 'Pantry', icon: 'basket', hint: 'P', run: () => navigate('/pantry') },
    { id: 'low', label: 'Pantry: running low', icon: 'alert', run: () => navigate('/pantry?low=1') },
    { id: 'expiring', label: 'Pantry: use soon', icon: 'clock', keywords: 'expiring best before', run: () => navigate('/pantry?expiring=1') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'layers', keywords: 'inventory split habits core integrations', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  const done = () => { setModal(null); announceChange() }
  const firstList = lists[0]?.id
  return (
    <CrumbCtx.Provider value={setCrumb}>
      <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
        {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
        <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
          <Sidebar onCreate={create} onNavigate={closeDrawer} counts={counts} lists={lists} />
        </div>
        <div className="main">
          <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
          <main className="content"><div className="content-inner"><Outlet /></div></main>
        </div>
        {mobile && <button type="button" className="fab" aria-label="New" title="New" aria-haspopup="menu" aria-expanded={!!fabMenu} onClick={e => setFabMenu(fabMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
        {fabMenu && <Menu anchor={fabMenu} align="right" items={createItems(create)} onClose={() => setFabMenu(null)} width={250} />}
        {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} lists={lists} />}
        {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
        {modal === 'import' && <ImportRecipeModal onClose={() => setModal(null)} />}
        {modal === 'plan' && <PlanModal onClose={() => setModal(null)} onSaved={done} />}
        {modal === 'item' && (firstList
          ? <ItemModal listId={currentListId(loc.pathname, lists) || firstList} onClose={() => setModal(null)} onSaved={done} />
          : <ListModal onClose={() => setModal(null)} onSaved={l => { done(); navigate(`/shopping/${l.id}`) }} />)}
        {modal === 'list' && <ListModal onClose={() => setModal(null)} onSaved={l => { done(); navigate(`/shopping/${l.id}`) }} />}
        {modal === 'pantry' && <PantryModal onClose={() => setModal(null)} onSaved={done} />}
        {modal === 'scan' && <ScanModal onClose={() => setModal(null)} onAdded={done} />}
      </div>
    </CrumbCtx.Provider>
  )
}

/** The shopping list on screen, so "Add to the shopping list" goes there. */
function currentListId(path: string, lists: List[]): number | undefined {
  const m = path.match(/^\/shopping\/(\d+)/)
  const id = m ? Number(m[1]) : 0
  return lists.find(l => l.id === id)?.id
}

const SECTION: Record<string, [string, string]> = { '': ['Home', '/'], recipes: ['Recipes', '/recipes'], plan: ['Meal plan', '/plan'], shopping: ['Shopping', '/shopping'], pantry: ['Pantry', '/pantry'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pk:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
