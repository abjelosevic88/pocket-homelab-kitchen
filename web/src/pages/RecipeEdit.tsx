import { useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { cap, COURSE_ICON, recipeImage, type Ingredient, type Recipe } from '../lib/types'
import { Card, Field, Loading, PageHeader, useToast } from '../components/ui'
import { DataList } from '../components/forms'

type Form = { title: string; description: string; servings: number; prep_minutes: number; cook_minutes: number; course: string; cuisine: string; tags: string; source_url: string; source_name: string; rating: number; notes: string; image_url: string; ingredients: Ingredient[]; steps: string[]; nutrition: Record<string, string> }

const blank = (servings: number): Form => ({ title: '', description: '', servings, prep_minutes: 0, cook_minutes: 0, course: 'dinner', cuisine: '', tags: '', source_url: '', source_name: '', rating: 0, notes: '', image_url: '', ingredients: [{ name: '', qty: 0, unit: '', note: '' }], steps: [''], nutrition: {} })

export default function RecipeEdit() {
  const { id } = useParams()
  const [sp] = useSearchParams()
  const navigate = useNavigate()
  const { meta, settings } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<Form | null>(null)
  const [existing, setExisting] = useState<Recipe | null>(null)
  const [busy, setBusy] = useState(false)
  const [photo, setPhoto] = useState<File | null>(null)
  const [bulk, setBulk] = useState(false)
  const [bulkText, setBulkText] = useState('')
  useEffect(() => {
    if (id) { api.get<{ recipe: Recipe }>(`${V1}/recipes/${id}`).then(d => { setExisting(d.recipe); setF({ ...d.recipe, ingredients: d.recipe.ingredients.length ? d.recipe.ingredients : [{ name: '', qty: 0, unit: '', note: '' }], steps: d.recipe.steps.length ? d.recipe.steps : [''] }) }).catch(e => toast((e as Error).message, 'err')); return }
    if (sp.get('import')) {
      const raw = sessionStorage.getItem('pk-import')
      if (raw) { const r = JSON.parse(raw) as Recipe; setF({ ...blank(settings?.default_servings || 2), ...r, ingredients: r.ingredients.length ? r.ingredients : [{ name: '', qty: 0, unit: '', note: '' }], steps: r.steps.length ? r.steps : [''] }); return }
    }
    setF(blank(settings?.default_servings || 2))
  }, [id]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!f) return <Loading />
  const set = (k: keyof Form, v: unknown) => setF(x => x ? { ...x, [k]: v } : x)
  const setIng = (i: number, patch: Partial<Ingredient>) => set('ingredients', f.ingredients.map((x, j) => j === i ? { ...x, ...patch } : x))
  const setStep = (i: number, v: string) => set('steps', f.steps.map((x, j) => j === i ? v : x))
  const move = <T,>(arr: T[], i: number, d: number): T[] => { const j = i + d; if (j < 0 || j >= arr.length) return arr; const c = [...arr]; [c[i], c[j]] = [c[j], c[i]]; return c }
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try {
      const body = { ...f, ingredients: f.ingredients.filter(i => i.name.trim()), steps: f.steps.filter(s => s.trim()) }
      const r = existing ? await api.put<Recipe>(`${V1}/recipes/${existing.id}`, body) : await api.post<Recipe>(`${V1}/recipes`, body)
      if (photo) await upload(`${V1}/recipes/${r.id}/photo`, 'file', photo)
      sessionStorage.removeItem('pk-import')
      toast(existing ? 'Saved' : 'Recipe added')
      navigate(`/recipes/${r.id}`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const applyBulk = () => {
    const lines = bulkText.split('\n').map(l => l.trim()).filter(Boolean)
    if (lines.length === 0) { setBulk(false); return }
    // the server parses "200 g flour, sifted" — reuse the import parser via a tiny client-side split
    const parsed = lines.map(parseLine)
    set('ingredients', [...f.ingredients.filter(i => i.name.trim()), ...parsed])
    setBulk(false); setBulkText('')
  }
  const img = photo ? URL.createObjectURL(photo) : existing ? recipeImage(existing) : f.image_url
  return (
    <form onSubmit={save}>
      <PageHeader title={existing ? `Edit ${existing.title}` : sp.get('import') ? 'Check the imported recipe' : 'New recipe'} actions={<><button type="button" className="btn" onClick={() => navigate(existing ? `/recipes/${existing.id}` : '/recipes')}>Cancel</button><button className="btn primary" disabled={busy}>{busy ? 'Saving…' : 'Save recipe'}</button></>} />
      <div className="two-col" style={{ gridTemplateColumns: 'minmax(0, 3fr) minmax(0, 2fr)' }}>
        <div className="grid" style={{ gap: 16 }}>
          <Card title="Recipe">
            <div className="grid" style={{ gap: 12 }}>
              <Field label="Title"><input value={f.title} onChange={e => set('title', e.target.value)} required autoFocus={!existing} /></Field>
              <Field label="Description"><textarea rows={2} value={f.description} onChange={e => set('description', e.target.value)} /></Field>
              <div className="three-col tight">
                <Field label="Servings"><input type="number" min={1} value={f.servings} onChange={e => set('servings', Number(e.target.value))} required /></Field>
                <Field label="Prep (min)"><input type="number" min={0} value={f.prep_minutes || ''} onChange={e => set('prep_minutes', Number(e.target.value))} /></Field>
                <Field label="Cook (min)"><input type="number" min={0} value={f.cook_minutes || ''} onChange={e => set('cook_minutes', Number(e.target.value))} /></Field>
              </div>
              <Field group label="Course"><div className="chips">{meta.courses.map(c => <button type="button" key={c} className={`chip ${f.course === c ? 'active' : ''}`} onClick={() => set('course', c)}>{COURSE_ICON[c]} {cap(c)}</button>)}</div></Field>
              <div className="two-col tight">
                <Field label="Cuisine"><input value={f.cuisine} onChange={e => set('cuisine', e.target.value)} placeholder="Italian" /></Field>
                <Field label="Tags" help="Comma separated"><input value={f.tags} onChange={e => set('tags', e.target.value)} placeholder="quick, vegetarian, batch" /></Field>
              </div>
              <Field group label="Rating"><div className="rating-pick">{[1, 2, 3, 4, 5].map(n => <button type="button" key={n} className={n <= f.rating ? 'on' : ''} onClick={() => set('rating', f.rating === n ? 0 : n)}>★</button>)}</div></Field>
            </div>
          </Card>
          <Card title="Ingredients" actions={<><button type="button" className="btn sm" onClick={() => setBulk(b => !b)}>Paste a list</button><button type="button" className="btn sm" onClick={() => set('ingredients', [...f.ingredients, { name: '', qty: 0, unit: '', note: '' }])}>+ Line</button></>}>
            {bulk && <div className="grid mb" style={{ gap: 8 }}><textarea rows={5} value={bulkText} onChange={e => setBulkText(e.target.value)} placeholder={'200 g flour\n2 eggs\n1 tsp salt'} /><div className="row"><button type="button" className="btn sm primary" onClick={applyBulk}>Add these lines</button><button type="button" className="btn sm" onClick={() => setBulk(false)}>Cancel</button></div></div>}
            <div className="grid" style={{ gap: 6 }}>
              {f.ingredients.map((ing, i) => (
                <div key={i} className="ing-edit">
                  <input type="number" step="0.01" min={0} value={ing.qty || ''} onChange={e => setIng(i, { qty: Number(e.target.value) })} placeholder="qty" aria-label="Quantity" />
                  <input value={ing.unit} onChange={e => setIng(i, { unit: e.target.value })} placeholder="unit" list="units" aria-label="Unit" />
                  <input value={ing.name} onChange={e => setIng(i, { name: e.target.value })} placeholder="Ingredient" aria-label="Ingredient" list="pantry-names" />
                  <input className="note-in" value={ing.note} onChange={e => setIng(i, { note: e.target.value })} placeholder="note (chopped, optional…)" aria-label="Note" />
                  <label className="row small muted opt-in" title="Optional ingredient"><input type="checkbox" checked={!!ing.optional} onChange={e => setIng(i, { optional: e.target.checked })} style={{ width: 'auto' }} />opt.</label>
                  <span className="row" style={{ gap: 2 }}><button type="button" className="icon-btn" onClick={() => set('ingredients', move(f.ingredients, i, -1))} aria-label="Up">↑</button><button type="button" className="icon-btn" onClick={() => set('ingredients', move(f.ingredients, i, 1))} aria-label="Down">↓</button><button type="button" className="icon-btn" onClick={() => set('ingredients', f.ingredients.filter((_, j) => j !== i))} aria-label="Remove">✕</button></span>
                </div>
              ))}
            </div>
            <DataList id="units" items={meta.units} />
            <p className="muted small mt">Names that match a pantry item are linked automatically, so cooking takes them off the shelf.</p>
          </Card>
          <Card title="Steps" actions={<button type="button" className="btn sm" onClick={() => set('steps', [...f.steps, ''])}>+ Step</button>}>
            <div className="grid" style={{ gap: 8 }}>
              {f.steps.map((s, i) => (
                <div key={i} className="step-edit">
                  <span className="n">{i + 1}.</span>
                  <textarea rows={2} value={s} onChange={e => setStep(i, e.target.value)} placeholder="What to do" />
                  <span className="row" style={{ gap: 2, paddingTop: 6 }}><button type="button" className="icon-btn" onClick={() => set('steps', move(f.steps, i, -1))} aria-label="Up">↑</button><button type="button" className="icon-btn" onClick={() => set('steps', move(f.steps, i, 1))} aria-label="Down">↓</button><button type="button" className="icon-btn" onClick={() => set('steps', f.steps.filter((_, j) => j !== i))} aria-label="Remove">✕</button></span>
                </div>
              ))}
            </div>
          </Card>
        </div>
        <div className="grid" style={{ gap: 16 }}>
          <Card title="Photo">
            <div className="photo-drop">
              {img ? <img src={img} alt="" /> : <span className="muted small">No photo</span>}
              <div className="grid" style={{ gap: 6 }}>
                <label className="btn sm">Choose a photo…<input type="file" accept="image/*" hidden onChange={e => setPhoto(e.target.files?.[0] || null)} /></label>
                {existing?.has_photo && <button type="button" className="btn sm ghost danger-text" onClick={async () => { await api.del(`${V1}/recipes/${existing.id}/photo`); setExisting({ ...existing, has_photo: false }); toast('Photo removed') }}>Remove photo</button>}
              </div>
            </div>
            <Field label="…or an image URL" className="mt"><input value={f.image_url} onChange={e => set('image_url', e.target.value)} placeholder="https://" /></Field>
          </Card>
          <Card title="Source">
            <div className="grid" style={{ gap: 10 }}>
              <Field label="URL"><input value={f.source_url} onChange={e => set('source_url', e.target.value)} placeholder="https://" /></Field>
              <Field label="Name"><input value={f.source_name} onChange={e => set('source_name', e.target.value)} placeholder="Grandma, BBC Good Food…" /></Field>
            </div>
          </Card>
          <Card title="Notes"><textarea rows={4} value={f.notes} onChange={e => set('notes', e.target.value)} placeholder="Tweaks, what worked, what to try" /></Card>
          <Card title="Nutrition per serving" actions={<button type="button" className="btn sm" onClick={() => set('nutrition', { ...f.nutrition, [`field${Object.keys(f.nutrition).length + 1}`]: '' })}>+ Field</button>}>
            <div className="grid" style={{ gap: 6 }}>
              {Object.entries(f.nutrition).map(([k, v]) => <div key={k} className="row"><input value={k} onChange={e => { const n: Record<string, string> = {}; for (const [kk, vv] of Object.entries(f.nutrition)) n[kk === k ? e.target.value : kk] = vv; set('nutrition', n) }} placeholder="calories" /><input value={v} onChange={e => set('nutrition', { ...f.nutrition, [k]: e.target.value })} placeholder="420 kcal" /><button type="button" className="icon-btn" onClick={() => { const n = { ...f.nutrition }; delete n[k]; set('nutrition', n) }}>✕</button></div>)}
              {Object.keys(f.nutrition).length === 0 && <p className="muted small">Optional.</p>}
            </div>
          </Card>
        </div>
      </div>
    </form>
  )
}

const UNITS = ['kg', 'g', 'mg', 'oz', 'lb', 'lbs', 'ml', 'cl', 'dl', 'l', 'tsp', 'tbsp', 'cup', 'cups', 'pcs', 'clove', 'cloves', 'slice', 'slices', 'can', 'cans', 'pack', 'bunch', 'pinch', 'handful', 'sprig', 'stick', 'teaspoon', 'teaspoons', 'tablespoon', 'tablespoons', 'grams', 'gram', 'litre', 'liter']
function parseLine(line: string): Ingredient {
  let s = line.replace(/^[-•*]\s*/, '').replace(/[½⅓⅔¼¾]/g, m => ({ '½': ' 0.5', '⅓': ' 0.333', '⅔': ' 0.667', '¼': ' 0.25', '¾': ' 0.75' }[m] || m)).trim()
  let qty = 0
  const m = s.match(/^(\d+(?:[.,]\d+)?)(?:\s+(\d+)\/(\d+))?\s*/) || s.match(/^(\d+)\/(\d+)\s*/)
  if (m) {
    if (m[0].includes('/') && !m[2]) { qty = Number(m[1]) / Number((m as RegExpMatchArray)[2] || 1) } else { qty = Number(m[1].replace(',', '.')); if (m[2] && m[3]) qty += Number(m[2]) / Number(m[3]) }
    s = s.slice(m[0].length)
  }
  let unit = ''
  const w = s.split(/\s+/)
  if (w.length > 1 && UNITS.includes(w[0].toLowerCase().replace(/\.$/, ''))) { unit = w[0].toLowerCase().replace(/\.$/, ''); s = w.slice(1).join(' ').replace(/^of\s+/, '') }
  let note = ''
  const c = s.indexOf(',')
  if (c > 0) { note = s.slice(c + 1).trim(); s = s.slice(0, c).trim() }
  return { name: s.trim() || line, qty, unit, note }
}
