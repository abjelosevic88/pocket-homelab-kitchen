import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, ConnMode, Habit, Settings as SettingsT, SplitGroup, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { CurrencySelect } from '../components/Money'
import { ShortcutsSheet } from '../components/ShortcutsSheet'

type TabId = 'general' | 'notifications' | 'apps' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'Currency, week start, servings, date and time formats, and the app title.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'One push a day with what is planned and what to use up.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Pocket Inventory, Pocket Split and Pocket Habits — through Pocket Core or directly — and Open Food Facts.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Homepage widgets, Siri Shortcuts and Raycast.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database, export your kitchen or import one.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]
// the old ?tab= names
const ALIAS: Record<string, TabId> = { integrations: 'apps', users: 'people', api: 'tokens' }

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const legacy = new URLSearchParams(window.location.search).get('tab') || ''
  const want = raw || legacy
  const tab: TabId = TABS.find(t => t.id === (ALIAS[want] || want))?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })

  // /settings?tab=integrations (old links from other apps) → /settings/apps
  useEffect(() => { if (!raw && legacy) navigate(`/settings/${tab}`, { replace: true }) }, [raw, legacy, tab, navigate])
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back to the kitchen" title="Back to the kitchen (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'general' && <General />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'apps' && <Integrations />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

/** "Test connection" results per integration. */
function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Your kitchen" sub="Yours alone — everyone on the box picks their own.">
        <SettingRow title="Home currency" help="Grocery spend and checkout totals default to this."><CurrencySelect value={f.home_currency} onChange={c => setF({ ...f, home_currency: c })} /></SettingRow>
        <SettingRow title="Week starts on" help="The meal plan shows a week from this day."><Segmented value={f.week_start} onChange={v => setF({ ...f, week_start: v as 'mon' | 'sun' })} options={[{ value: 'mon', label: 'Monday' }, { value: 'sun', label: 'Sunday' }]} size="sm" /></SettingRow>
        <SettingRow title="Servings for new recipes"><input type="number" min={1} max={50} value={f.default_servings} onChange={e => setF({ ...f, default_servings: Number(e.target.value) })} className="set-time" /></SettingRow>
      </Section>
      <Section title="Date & time">
        <SettingRow title="Date format"><Select value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
      </Section>
      <Section title="This box" sub={isAdmin ? 'Applies to everyone on this box.' : 'Set by an admin for everyone.'}>
        <SettingRow title="App title" help="Shown in the sidebar and the browser tab."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} disabled={!isAdmin} title={isAdmin ? undefined : 'Box-wide — only an admin can change it'} /></SettingRow>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
      <Section title="How it fits together">
        <dl className="set-dl">
          <div><dt>Recipes</dt><dd className="muted">Typed in, or imported from any recipe site by URL. Ingredient names link to pantry items.</dd></div>
          <div><dt>Plan</dt><dd className="muted">A week of slots. “Shopping list from the plan” scales every uncooked recipe and merges the ingredients.</dd></div>
          <div><dt>Shopping</dt><dd className="muted">Lines grouped by aisle. Checking out puts the bought items into the pantry and posts the receipt to Pocket Split.</dd></div>
          <div><dt>Pantry</dt><dd className="muted">With Pocket Inventory connected the pantry is the items tagged <code>{f.inventory_tag}</code> there; cooking calls its adjust endpoint.</dd></div>
          <div><dt>Cooking</dt><dd className="muted">“Cooked it” logs the dish, takes the ingredients off the pantry and marks a Pocket Habits habit for the day.</dd></div>
          <div><dt>Barcodes</dt><dd className="muted">Scan with the phone camera; the pantry, then Pocket Inventory, then Open Food Facts answer.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const notifyRun = async () => { try { const r = await api.post<{ ok: boolean; sent: number; error?: string }>(`${V1}/notify/run`); toast(r.ok ? (r.sent ? 'Sent' : 'Nothing to send today') : r.error || 'failed', r.ok ? 'ok' : 'err') } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <Section title="Daily push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>One push a day: what is planned today and which pantry items to use soon. Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/kitchen" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
        <SettingRow title="Send at" help="Hour of the day, server time."><input type="number" min={0} max={23} value={f.notify_hour} onChange={e => setF({ ...f, notify_hour: Number(e.target.value) })} className="set-time" /></SettingRow>
        <SettingRow title="Today's plan" help="The meals planned for the day."><Switch checked={f.notify_plan} onChange={v => setF({ ...f, notify_plan: v })} label="Include today's plan" /></SettingRow>
        <SettingRow title="Use soon" help="Pantry items close to their best-before date."><Switch checked={f.notify_expiry} onChange={v => setF({ ...f, notify_expiry: v })} label="Include pantry items to use soon" /></SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        {f.notify_configured && <button type="button" className="btn" onClick={notifyRun}><Icon name="bell" size={15} />Send today's message now</button>}
        {status('notify')}
      </div>
    </div>
  )
}

function Integrations() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const { run, status } = useTester()
  const [groups, setGroups] = useState<SplitGroup[]>([])
  const [cats, setCats] = useState<string[]>([])
  const [habits, setHabits] = useState<Habit[]>([])
  const [locs, setLocs] = useState<{ id: number; name: string }[]>([])
  // the pickers list through whichever connection is in effect (Pocket Core or direct)
  const splitKey = `${f?.split_mode}:${f?.split_via}:${f?.split_url}:${f?.split_token}`
  const habitsKey = `${f?.habits_mode}:${f?.habits_via}:${f?.habits_url}:${f?.habits_token}`
  const invKey = `${f?.inventory_mode}:${f?.inventory_via}`
  useEffect(() => { setGroups([]); setCats([]); if (f?.split_via) { api.get<SplitGroup[]>(`${V1}/split/groups`).then(setGroups).catch(() => {}); api.get<string[]>(`${V1}/split/categories`).then(setCats).catch(() => {}) } }, [splitKey]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setHabits([]); if (f?.habits_via) api.get<Habit[]>(`${V1}/habits/list`).then(setHabits).catch(() => {}) }, [habitsKey]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setLocs([]); if (f?.inventory_via) api.get<typeof locs>(`${V1}/inventory/locations`).then(setLocs).catch(() => {}) }, [invKey]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!f) return <Loading />
  const group = groups.find(g => g.id === f.split_group_id)
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><span>These connections are yours. Through <b>Pocket Core</b> Kitchen acts as you in your own Pocket Inventory, Split and Habits with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter. Nobody else's pantry, groceries or habits are touched from your account, and yours are not touched from theirs.</span></div>
      <Section title="Pocket Inventory — the pantry" icon="package" actions={connBadge(f.inventory_mode, f.inventory_via)}
        sub="The pantry is the set of Inventory items carrying one tag. Cooking a recipe calls Inventory's adjust endpoint for each ingredient, a checkout tops items up or creates them with the tag, and a sync every 30 minutes pulls changes made in Inventory.">
        <Connection app="Pocket Inventory" mode={f.inventory_mode} via={f.inventory_via} detail={f.inventory_detail} core={f.core_configured} hasToken={!!f.inventory_token}
          onMode={m => save({ inventory_mode: m })} onDisconnect={() => save({ inventory_url: '', inventory_token: '' })}
          test={<><button type="button" className="btn sm" onClick={() => run('inventory', f.inventory_url, f.inventory_token)}>Test connection</button>{status('inventory')}</>}
          direct={<div className="two-col tight">
            <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f.inventory_url} onChange={e => setF({ ...f, inventory_url: e.target.value })} placeholder="http://host.docker.internal:8170" /></Field>
            <Field label="API token" help="Pocket Inventory → Settings → API tokens"><input value={f.inventory_token} onChange={e => setF({ ...f, inventory_token: e.target.value })} autoComplete="off" /></Field>
          </div>} />
        {f.inventory_mode !== 'off' && <div className="mt">
          <SettingRow title="Pantry tag" help="Items with this tag in Inventory are the pantry."><input value={f.inventory_tag} onChange={e => setF({ ...f, inventory_tag: e.target.value })} /></SettingRow>
          <SettingRow title="Location for new items">{locs.length > 0 ? <Select value={f.inventory_location} onChange={v => setF({ ...f, inventory_location: v })} options={locs.map(l => ({ value: l.name, label: l.name }))} clearLabel="None" onCreate={v => v} /> : <input value={f.inventory_location} onChange={e => setF({ ...f, inventory_location: e.target.value })} placeholder="Kitchen" />}</SettingRow>
          <SettingRow title="Sync every 30 minutes" help="Pull the pantry from Inventory in the background."><Switch checked={f.inventory_auto_sync} onChange={v => setF({ ...f, inventory_auto_sync: v })} label="Sync the pantry from Inventory every 30 minutes" /></SettingRow>
        </div>}
      </Section>
      <Section title="Pocket Split — who paid for the groceries" icon="wallet" actions={connBadge(f.split_mode, f.split_via)}
        sub="“Done shopping” can post the receipt as an expense in one group, split evenly between its members, with the bought items in the notes.">
        <Connection app="Pocket Split" mode={f.split_mode} via={f.split_via} detail={f.split_detail} core={f.core_configured} hasToken={!!f.split_token}
          onMode={m => save({ split_mode: m })} onDisconnect={() => save({ split_url: '', split_token: '' })}
          test={<><button type="button" className="btn sm" onClick={() => run('split', f.split_url, f.split_token)}>Test connection</button>{status('split')}</>}
          direct={<div className="two-col tight">
            <Field label="URL"><input value={f.split_url} onChange={e => setF({ ...f, split_url: e.target.value })} placeholder="http://host.docker.internal:8172" /></Field>
            <Field label="API token" help="Pocket Split → Settings → API tokens"><input value={f.split_token} onChange={e => setF({ ...f, split_token: e.target.value })} autoComplete="off" /></Field>
          </div>} />
        {f.split_mode !== 'off' && <div className="mt">
          <SettingRow title="Group">{groups.length > 0 ? <Select value={String(f.split_group_id || '')} onChange={v => setF({ ...f, split_group_id: Number(v) })} options={groups.map(g => ({ value: String(g.id), label: g.name, hint: g.currency }))} placeholder="Pick a group" /> : <input type="number" value={f.split_group_id || ''} onChange={e => setF({ ...f, split_group_id: Number(e.target.value) })} placeholder="group id" />}</SettingRow>
          <SettingRow title="Usually paid by">{group ? <Select value={String(f.split_member_id || '')} onChange={v => setF({ ...f, split_member_id: Number(v) })} options={group.members.map(m => ({ value: String(m.id), label: m.name }))} placeholder="Ask each time" clearLabel="Ask each time" /> : <input type="number" value={f.split_member_id || ''} onChange={e => setF({ ...f, split_member_id: Number(e.target.value) })} placeholder="member id" />}</SettingRow>
          <SettingRow title="Category">{cats.length > 0 ? <Select value={f.split_category} onChange={v => setF({ ...f, split_category: v })} options={cats.map(c => ({ value: c, label: c }))} /> : <input value={f.split_category} onChange={e => setF({ ...f, split_category: e.target.value })} />}</SettingRow>
          {f.split_mode === 'direct' && <SettingRow title="Public URL (optional)" help="What the browser opens for the “Open in Split” link when the URL above is internal." stack><input value={f.split_public_url} onChange={e => setF({ ...f, split_public_url: e.target.value })} placeholder="https://split.example.com" /></SettingRow>}
        </div>}
      </Section>
      <Section title="Pocket Habits — cooked at home" icon="habit" actions={connBadge(f.habits_mode, f.habits_via)}
        sub="Every “Cooked it” marks one habit as done for that day. Create a habit like “Cook at home” in Pocket Habits and pick it here.">
        <Connection app="Pocket Habits" mode={f.habits_mode} via={f.habits_via} detail={f.habits_detail} core={f.core_configured} hasToken={!!f.habits_token}
          onMode={m => save({ habits_mode: m })} onDisconnect={() => save({ habits_url: '', habits_token: '' })}
          test={<><button type="button" className="btn sm" onClick={() => run('habits', f.habits_url, f.habits_token)}>Test connection</button>{status('habits')}</>}
          direct={<div className="two-col tight">
            <Field label="URL"><input value={f.habits_url} onChange={e => setF({ ...f, habits_url: e.target.value })} placeholder="http://host.docker.internal:8169" /></Field>
            <Field label="API token" help="Pocket Habits → Settings → API tokens"><input value={f.habits_token} onChange={e => setF({ ...f, habits_token: e.target.value })} autoComplete="off" /></Field>
          </div>} />
        {f.habits_mode !== 'off' && <div className="mt">
          <SettingRow title="Habit">{habits.length > 0 ? <Select value={String(f.habit_id || '')} onChange={v => setF({ ...f, habit_id: Number(v) })} options={habits.filter(h => !h.end_date).map(h => ({ value: String(h.id), label: h.name }))} placeholder="Pick a habit" /> : <input type="number" value={f.habit_id || ''} onChange={e => setF({ ...f, habit_id: Number(e.target.value) })} placeholder="habit id" />}</SettingRow>
        </div>}
      </Section>
      <Section title="Open Food Facts" icon="globe" actions={<span className={`badge ${f.food_facts ? 'ok' : ''}`}>{f.food_facts ? 'on' : 'off'}</span>}
        sub={<>When a scanned barcode is neither in the pantry nor in Pocket Inventory, the public Open Food Facts database names the product. No account needed; lookups are cached.{isAdmin ? ' Shared by everyone on this box.' : ' Set by an admin for the whole box.'}</>}>
        <SettingRow title="Look up unknown barcodes" help={<span className="row wrap mt-sm"><button type="button" className="btn sm" onClick={() => run('foodfacts', '', '')}>Test</button>{status('foodfacts')}</span>}>
          <Switch checked={f.food_facts} onChange={v => setF({ ...f, food_facts: v })} disabled={!isAdmin} label="Look up unknown barcodes on Open Food Facts" />
        </SettingRow>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

/**
 * One Pocket app connection: Pocket Core, Direct (URL + token) or Off, with
 * what it actually is right now. The mode saves at once; Disconnect removes
 * the saved URL and token. On a box with Pocket Core, Direct is advanced:
 * the switch is Pocket Core | Off and Direct sits behind a small link, unless
 * the person is on Direct already (their current mode is never hidden).
 */
function Connection({ app, mode, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const ok = via !== ''
  const [advanced, setAdvanced] = useState(mode === 'direct')
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Kitchen does not talk to ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'There is no Pocket Core on this box' },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own recipes, plan, shopping lists, pantry and integrations. Admins manage accounts; they do not see anyone else's kitchen.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their recipes, plan, lists, pantry and history." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) or drive a phone shortcut. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage widget)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Shortcuts & widgets" icon="smartphone" sub={<>Handy endpoints at <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>GET /dashboard</code></dt><dd className="muted">Today's meals, open shopping lines, pantry low/expiring, cooked this month — for Homepage.</dd></div>
          <div><dt><code>POST /lists/1/items</code></dt><dd className="muted"><code>{'{"name":"2 kg potatoes"}'}</code> — a Siri Shortcut / Raycast “add to the list”.</dd></div>
          <div><dt><code>GET /lookup?barcode=…</code></dt><dd className="muted">Pantry → Inventory → Open Food Facts.</dd></div>
          <div><dt><code>POST /recipes/import</code></dt><dd className="muted"><code>{'{"url":"…","save":true}'}</code> — share a recipe link from the phone.</dd></div>
          <div><dt><code>POST /recipes/{'{id}'}/cook</code></dt><dd className="muted">Log a cook, deduct the pantry, mark the habit.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const importJSON = async (file: File) => {
    setBusy(true)
    try { const r = await api.post<Record<string, number>>(`${V1}/import`, JSON.parse(await file.text())); toast('Imported: ' + (Object.entries(r).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new')) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Recipe photos live next to it under <code>files/</code>. Restore by stopping the app and replacing <code>pocket-kitchen.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
      <Section title="Export & import" icon="archive" sub="Your recipes, plan, lists, pantry, cook log and checkouts as one JSON file. Importing merges into your own kitchen: recipes by title, pantry items and lists by name.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a><label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label></div>
      </Section>
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const tiles: [string, number | undefined, string][] = [['Recipes', counts.recipes, 'book'], ['Planned meals', counts.plan, 'calendar'], ['Cooks', counts.cooklog, 'flame'], ['Pantry items', counts.pantry, 'basket']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))} · files {fmtBytes(Number(data.files_bytes))}</span>],
    ['More rows', `${counts.list_items ?? 0} list lines · ${counts.purchases ?? 0} checkouts · ${counts.users ?? 0} users`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Kitchen</h3>
          <p className="muted small">The recipes, the meal plan, the shopping list and the pantry in one small server — the pantry is Pocket Inventory, the receipt goes to Pocket Split, and every cooked dish marks a Pocket Habits habit. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{n ?? 0}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-kitchen" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Sibling projects: Pocket Habits, Pocket Inventory, Pocket Bookshelf, Pocket Split, Pocket Clips, Pocket Travel, Pocket Garage.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
