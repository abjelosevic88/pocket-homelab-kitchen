import React, { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, ConnMode, Habit, Settings as SettingsT, SplitGroup, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Card, Confirm, Field, Loading, Modal, PageHeader, Segmented, Tabs, useAsync, useToast } from '../components/ui'
import { Select } from '../components/Select'
import { CurrencySelect } from '../components/Money'
import { Avatar } from '../components/people'

type Tab = 'general' | 'integrations' | 'users' | 'profile' | 'api' | 'data' | 'about'

export default function Settings() {
  const [tab, setTab] = useState<Tab>(() => (new URLSearchParams(window.location.search).get('tab') as Tab) || 'general')
  return (
    <>
      <PageHeader title="Settings" />
      <Tabs tabs={[{ id: 'general', label: 'General' }, { id: 'integrations', label: 'Integrations' }, { id: 'users', label: 'Users' }, { id: 'profile', label: 'Profile' }, { id: 'api', label: 'API & apps' }, { id: 'data', label: 'Backup & data' }, { id: 'about', label: 'About' }]} value={tab} onChange={setTab} />
      <div className="mt">
        {tab === 'general' && <General />}
        {tab === 'integrations' && <Integrations />}
        {tab === 'users' && <Users />}
        {tab === 'profile' && <Profile />}
        {tab === 'api' && <Tokens />}
        {tab === 'data' && <Data />}
        {tab === 'about' && <About />}
      </div>
    </>
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

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="two-col">
      <Card title="Preferences">
        <div className="grid" style={{ gap: 14 }}>
          <Field label="App title"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} disabled={!isAdmin} title={isAdmin ? undefined : 'Box-wide — only an admin can change it'} /></Field>
          <Field label="Home currency" help="Grocery spend and checkout totals default to this."><CurrencySelect value={f.home_currency} onChange={c => setF({ ...f, home_currency: c })} /></Field>
          <div className="two-col tight">
            <Field label="Date format"><Select value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></Field>
            <Field label="Time format"><Select value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></Field>
          </div>
          <div className="two-col tight">
            <Field group label="Week starts on"><Segmented value={f.week_start} onChange={v => setF({ ...f, week_start: v as 'mon' | 'sun' })} options={[{ value: 'mon', label: 'Monday' }, { value: 'sun', label: 'Sunday' }]} size="sm" /></Field>
            <Field label="Default servings for new recipes"><input type="number" min={1} max={50} value={f.default_servings} onChange={e => setF({ ...f, default_servings: Number(e.target.value) })} /></Field>
          </div>
          <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
          <p className="muted small">These preferences are yours.{isAdmin ? ' The app title is shared by everyone on this box.' : ' The app title is set by an admin.'}</p>
        </div>
      </Card>
      <Card title="How it fits together">
        <table className="table compact"><tbody>
          <tr><th>Recipes</th><td className="muted">Typed in, or imported from any recipe site by URL. Ingredient names link to pantry items.</td></tr>
          <tr><th>Plan</th><td className="muted">A week of slots. “Shopping list from the plan” scales every uncooked recipe and merges the ingredients.</td></tr>
          <tr><th>Shopping</th><td className="muted">Lines grouped by aisle. Checking out puts the bought items into the pantry and posts the receipt to Pocket Split.</td></tr>
          <tr><th>Pantry</th><td className="muted">With Pocket Inventory connected the pantry is the items tagged <code>{f.inventory_tag}</code> there; cooking calls its adjust endpoint.</td></tr>
          <tr><th>Cooking</th><td className="muted">“Cooked it” logs the dish, takes the ingredients off the pantry and marks a Pocket Habits habit for the day.</td></tr>
          <tr><th>Barcodes</th><td className="muted">Scan with the phone camera; the pantry, then Pocket Inventory, then Open Food Facts answer.</td></tr>
        </tbody></table>
      </Card>
    </div>
  )
}

function Integrations() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const { toast } = useToast()
  const [test, setTest] = useState<Record<string, string>>({})
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
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  const badge = (on: boolean) => <span className={`badge ${on ? 'cat' : ''}`}>{on ? 'connected' : 'not set'}</span>
  const group = groups.find(g => g.id === f.split_group_id)
  const notifyRun = async () => { try { const r = await api.post<{ ok: boolean; sent: number; error?: string }>(`${V1}/notify/run`); toast(r.ok ? (r.sent ? 'Sent' : 'Nothing to send today') : r.error || 'failed', r.ok ? 'ok' : 'err') } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="grid" style={{ gap: 16 }}>
      <p className="muted small">These connections are yours. Through <b>Pocket Core</b> Kitchen acts as you in your own Pocket Inventory, Split and Habits with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter. Nobody else's pantry, groceries or habits are touched from your account, and yours are not touched from theirs.</p>
      <Card title="📦 Pocket Inventory — the pantry" actions={connBadge(f.inventory_mode, f.inventory_via)}>
        <p className="muted small">The pantry is the set of Inventory items carrying one tag. Cooking a recipe calls Inventory's adjust endpoint for each ingredient, a checkout tops items up or creates them with the tag, and a sync every 30 minutes pulls changes made in Inventory.</p>
        <div className="grid" style={{ gap: 12 }}>
          <Connection app="Pocket Inventory" mode={f.inventory_mode} via={f.inventory_via} detail={f.inventory_detail} core={f.core_configured} hasToken={!!f.inventory_token}
            onMode={m => save({ inventory_mode: m })} onDisconnect={() => save({ inventory_url: '', inventory_token: '' })}
            test={<><button className="btn sm" onClick={() => run('inventory', f.inventory_url, f.inventory_token)}>Test connection</button>{status('inventory')}</>}
            direct={<div className="two-col tight">
              <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f.inventory_url} onChange={e => setF({ ...f, inventory_url: e.target.value })} placeholder="http://host.docker.internal:8170" /></Field>
              <Field label="API token" help="Pocket Inventory → Settings → API tokens"><input value={f.inventory_token} onChange={e => setF({ ...f, inventory_token: e.target.value })} autoComplete="off" /></Field>
            </div>} />
          {f.inventory_mode !== 'off' && <>
          <div className="two-col tight">
            <Field label="Pantry tag" help="Items with this tag in Inventory are the pantry."><input value={f.inventory_tag} onChange={e => setF({ ...f, inventory_tag: e.target.value })} /></Field>
            <Field label="Default location for new items">{locs.length > 0 ? <Select value={f.inventory_location} onChange={v => setF({ ...f, inventory_location: v })} options={locs.map(l => ({ value: l.name, label: l.name }))} clearLabel="None" onCreate={v => v} /> : <input value={f.inventory_location} onChange={e => setF({ ...f, inventory_location: e.target.value })} placeholder="Kitchen" />}</Field>
          </div>
          <label className="row"><input type="checkbox" checked={f.inventory_auto_sync} onChange={e => setF({ ...f, inventory_auto_sync: e.target.checked })} style={{ width: 'auto' }} /> Sync the pantry from Inventory every 30 minutes</label>
          </>}
        </div>
      </Card>
      <Card title="💸 Pocket Split — who paid for the groceries" actions={connBadge(f.split_mode, f.split_via)}>
        <p className="muted small">“Done shopping” can post the receipt as an expense in one group, split evenly between its members, with the bought items in the notes.</p>
        <div className="grid" style={{ gap: 12 }}>
          <Connection app="Pocket Split" mode={f.split_mode} via={f.split_via} detail={f.split_detail} core={f.core_configured} hasToken={!!f.split_token}
            onMode={m => save({ split_mode: m })} onDisconnect={() => save({ split_url: '', split_token: '' })}
            test={<><button className="btn sm" onClick={() => run('split', f.split_url, f.split_token)}>Test connection</button>{status('split')}</>}
            direct={<div className="two-col tight">
              <Field label="URL"><input value={f.split_url} onChange={e => setF({ ...f, split_url: e.target.value })} placeholder="http://host.docker.internal:8172" /></Field>
              <Field label="API token" help="Pocket Split → Settings → API tokens"><input value={f.split_token} onChange={e => setF({ ...f, split_token: e.target.value })} autoComplete="off" /></Field>
            </div>} />
          {f.split_mode !== 'off' && <>
          <div className="three-col tight">
            <Field label="Group">{groups.length > 0 ? <Select value={String(f.split_group_id || '')} onChange={v => setF({ ...f, split_group_id: Number(v) })} options={groups.map(g => ({ value: String(g.id), label: g.name, hint: g.currency }))} placeholder="Pick a group" /> : <input type="number" value={f.split_group_id || ''} onChange={e => setF({ ...f, split_group_id: Number(e.target.value) })} placeholder="group id" />}</Field>
            <Field label="Usually paid by">{group ? <Select value={String(f.split_member_id || '')} onChange={v => setF({ ...f, split_member_id: Number(v) })} options={group.members.map(m => ({ value: String(m.id), label: m.name }))} placeholder="Ask each time" clearLabel="Ask each time" /> : <input type="number" value={f.split_member_id || ''} onChange={e => setF({ ...f, split_member_id: Number(e.target.value) })} placeholder="member id" />}</Field>
            <Field label="Category">{cats.length > 0 ? <Select value={f.split_category} onChange={v => setF({ ...f, split_category: v })} options={cats.map(c => ({ value: c, label: c }))} /> : <input value={f.split_category} onChange={e => setF({ ...f, split_category: e.target.value })} />}</Field>
          </div>
          {f.split_mode === 'direct' && <Field label="Public URL (optional)" help="What the browser opens for the “Open in Split” link when the URL above is internal."><input value={f.split_public_url} onChange={e => setF({ ...f, split_public_url: e.target.value })} placeholder="https://split.example.com" /></Field>}
          </>}
        </div>
      </Card>
      <Card title="✅ Pocket Habits — cooked at home" actions={connBadge(f.habits_mode, f.habits_via)}>
        <p className="muted small">Every “Cooked it” marks one habit as done for that day. Create a habit like “Cook at home” in Pocket Habits and pick it here.</p>
        <div className="grid" style={{ gap: 12 }}>
          <Connection app="Pocket Habits" mode={f.habits_mode} via={f.habits_via} detail={f.habits_detail} core={f.core_configured} hasToken={!!f.habits_token}
            onMode={m => save({ habits_mode: m })} onDisconnect={() => save({ habits_url: '', habits_token: '' })}
            test={<><button className="btn sm" onClick={() => run('habits', f.habits_url, f.habits_token)}>Test connection</button>{status('habits')}</>}
            direct={<div className="two-col tight">
              <Field label="URL"><input value={f.habits_url} onChange={e => setF({ ...f, habits_url: e.target.value })} placeholder="http://host.docker.internal:8169" /></Field>
              <Field label="API token" help="Pocket Habits → Settings → API tokens"><input value={f.habits_token} onChange={e => setF({ ...f, habits_token: e.target.value })} autoComplete="off" /></Field>
            </div>} />
          {f.habits_mode !== 'off' && <div className="two-col tight">
            <Field label="Habit">{habits.length > 0 ? <Select value={String(f.habit_id || '')} onChange={v => setF({ ...f, habit_id: Number(v) })} options={habits.filter(h => !h.end_date).map(h => ({ value: String(h.id), label: h.name }))} placeholder="Pick a habit" /> : <input type="number" value={f.habit_id || ''} onChange={e => setF({ ...f, habit_id: Number(e.target.value) })} placeholder="habit id" />}</Field>
          </div>}
        </div>
      </Card>
      <Card title="🔔 Notifications" actions={badge(f.notify_configured)}>
        <p className="muted small">One push a day: what is planned today and which pantry items to use soon. Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</p>
        <div className="grid" style={{ gap: 12 }}>
          <div className="two-col tight">
            <Field label="URL"><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/kitchen" /></Field>
            <Field group label="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></Field>
          </div>
          <div className="three-col tight">
            <Field label="Send at (hour, server time)"><input type="number" min={0} max={23} value={f.notify_hour} onChange={e => setF({ ...f, notify_hour: Number(e.target.value) })} /></Field>
            <Field group label="Include"><label className="row" style={{ paddingTop: 8 }}><input type="checkbox" checked={f.notify_plan} onChange={e => setF({ ...f, notify_plan: e.target.checked })} style={{ width: 'auto' }} /> today's plan</label></Field>
            <Field group label=" "><label className="row" style={{ paddingTop: 8 }}><input type="checkbox" checked={f.notify_expiry} onChange={e => setF({ ...f, notify_expiry: e.target.checked })} style={{ width: 'auto' }} /> pantry items to use soon</label></Field>
          </div>
          <div className="row"><button className="btn sm" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}>Send a test</button>{f.notify_configured && <button className="btn sm" onClick={notifyRun}>Send today's message now</button>}{status('notify')}</div>
        </div>
      </Card>
      <Card title="🌍 Open Food Facts" actions={badge(f.food_facts)}>
        <p className="muted small">When a scanned barcode is neither in the pantry nor in Pocket Inventory, the public Open Food Facts database names the product. No account needed; lookups are cached.{isAdmin ? ' Shared by everyone on this box.' : ' Set by an admin for the whole box.'}</p>
        <div className="row"><label className="row"><input type="checkbox" checked={f.food_facts} onChange={e => setF({ ...f, food_facts: e.target.checked })} style={{ width: 'auto' }} disabled={!isAdmin} /> Look up unknown barcodes on Open Food Facts</label><button className="btn sm" onClick={() => run('foodfacts', '', '')}>Test</button>{status('foodfacts')}</div>
      </Card>
      <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save integrations</button></div>
    </div>
  )
}

/**
 * One Pocket app connection: Pocket Core, Direct (URL + token) or Off, with
 * what it actually is right now. The mode saves at once; Disconnect removes
 * the saved URL and token.
 */
function Connection({ app, mode, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const ok = via !== ''
  const line = mode === 'off' ? `Off: Kitchen does not talk to ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'There is no Pocket Core on this box' },
          { value: 'direct', label: 'Direct', title: 'Your own URL and API token' },
          { value: 'off', label: 'Off' },
        ]} />
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge cat">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Card title="Users" actions={isAdmin && <button className="btn primary sm" onClick={() => setEdit({ role: 'member' })}>+ Add user</button>}>
      <p className="muted">Everyone signs in with their own account and has their own recipes, plan, shopping lists, pantry and integrations. Admins manage accounts; they do not see anyone else's kitchen.</p>
      <table className="table compact">
        <thead><tr><th /><th>Name</th><th>Email</th><th>Role</th><th /></tr></thead>
        <tbody>
          {users.map(u => (
            <tr key={u.id}>
              <td style={{ width: 32 }}><Avatar user={u} /></td>
              <td>{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge" style={{ marginLeft: 6 }}>you</span>}</td>
              <td className="muted">{u.email}</td>
              <td><span className={`badge ${u.role === 'admin' ? 'cat' : ''}`}>{u.role}</span></td>
              <td className="right">{isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their recipes, plan, lists, pantry and history." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Card>
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
    <Modal title={user.id ? 'Edit user' : 'Add user'} onClose={onClose}>
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add user'}</button></div>
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
    <Card title="Profile">
      <form onSubmit={save} className="grid" style={{ gap: 14, maxWidth: 480 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
        <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        {status?.auth_mode === 'local' && <>
          <hr />
          <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
          <Field label="New password" help="At least 8 characters. Leave empty to keep the current one."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
        </>}
        <div><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
    </Card>
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
    <div className="two-col">
      <Card title="API tokens">
        <p className="muted">Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) or drive a phone shortcut. See <code>docs/api.md</code>.</p>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage widget)" /><button className="btn primary">Create</button></form>
        {fresh && <div className="callout success mb"><b>Copy it now — it is shown once.</b><pre className="mono" style={{ margin: '8px 0 0' }}>{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Card>
      <Card title="Shortcuts & widgets">
        <p className="muted">Handy endpoints at <code>{base}/api/v1</code>:</p>
        <table className="table compact"><tbody>
          <tr><td className="mono">GET /dashboard</td><td className="muted">today's meals, open shopping lines, pantry low/expiring, cooked this month — for Homepage</td></tr>
          <tr><td className="mono">POST /lists/1/items</td><td className="muted"><code>{'{"name":"2 kg potatoes"}'}</code> — a Siri Shortcut / Raycast “add to the list”</td></tr>
          <tr><td className="mono">GET /lookup?barcode=…</td><td className="muted">pantry → Inventory → Open Food Facts</td></tr>
          <tr><td className="mono">POST /recipes/import</td><td className="muted"><code>{'{"url":"…","save":true}'}</code> — share a recipe link from the phone</td></tr>
          <tr><td className="mono">POST /recipes/{'{id}'}/cook</td><td className="muted">log a cook, deduct the pantry, mark the habit</td></tr>
        </tbody></table>
      </Card>
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
    <div className="two-col">
      {user?.role === 'admin' && <Card title="Backup">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Recipe photos live next to it under <code>files/</code>. Restore by stopping the app and replacing <code>pocket-kitchen.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}>Download database</a>
        <p className="muted small">The whole box, every account — admins only.</p>
      </Card>}
      <Card title="Export & import">
        <p className="muted">Your recipes, plan, lists, pantry, cook log and checkouts as one JSON file. Importing merges into your own kitchen: recipes by title, pantry items and lists by name.</p>
        <div className="row"><a className="btn" href={`${V1}/export.json`}>Export JSON</a><label className="btn">{busy ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label></div>
      </Card>
    </div>
  )
}

function About() {
  const { data } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const counts = (data?.counts || {}) as Record<string, number>
  return (
    <Card title="About">
      <p><b>Pocket Kitchen</b> is the recipes, the meal plan, the shopping list and the pantry in one small server — and the pantry is Pocket Inventory, the receipt goes to Pocket Split, and every cooked dish marks a Pocket Habits habit. One binary, one SQLite file, one volume.</p>
      <p className="muted small">Open source (MIT) — <a href="https://github.com/abjelosevic88/pocket-homelab-kitchen" target="_blank" rel="noreferrer">github.com/abjelosevic88/pocket-homelab-kitchen</a>. Sibling projects: Pocket Habits, Pocket Inventory, Pocket Bookshelf, Pocket Split, Pocket Clips, Pocket Travel, Pocket Garage.</p>
      {data && (
        <table className="table compact mt">
          <tbody>
            <tr><th>Version</th><td className="mono">{String(data.version)}</td></tr>
            <tr><th>Runtime</th><td className="mono">{String(data.go)} · {String(data.os)}/{String(data.arch)}</td></tr>
            <tr><th>Auth mode</th><td className="mono">{String(data.auth_mode)}</td></tr>
            <tr><th>Database</th><td className="mono">{String(data.db_path)} · {fmtBytes(Number(data.db_bytes))} · files {fmtBytes(Number(data.files_bytes))}</td></tr>
            <tr><th>Timezone</th><td className="mono">{String(data.timezone)}</td></tr>
            <tr><th>Uptime</th><td className="mono">{String(data.uptime)}</td></tr>
            <tr><th>Your rows</th><td className="mono">{counts.recipes} recipes · {counts.plan} planned meals · {counts.list_items} list lines · {counts.pantry} pantry items · {counts.cooklog} cooks · {counts.purchases} checkouts · {counts.users} users</td></tr>
          </tbody>
        </table>
      )}
    </Card>
  )
}
