export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }
export interface Currency { code: string; name: string; decimals: number }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'

export interface Settings {
  core_configured: boolean
  inventory_mode: ConnMode; inventory_via: ConnVia; inventory_detail: string
  split_mode: ConnMode; split_via: ConnVia; split_detail: string
  habits_mode: ConnMode; habits_via: ConnVia; habits_detail: string
  title: string; home_currency: string; date_format: string; time_format: string; week_start: 'mon' | 'sun'; default_servings: number
  notify_url: string; notify_format: string; notify_hour: number; notify_plan: boolean; notify_expiry: boolean; notify_configured: boolean
  inventory_url: string; inventory_token: string; inventory_tag: string; inventory_location: string; inventory_auto_sync: boolean; inventory_configured: boolean
  split_url: string; split_token: string; split_public_url: string; split_group_id: number; split_member_id: number; split_category: string; split_configured: boolean
  habits_url: string; habits_token: string; habit_id: number; habits_configured: boolean
  food_facts: boolean; max_upload_bytes: number; setup_complete: boolean; base_url: string
}

export interface Meta { courses: string[]; slots: string[]; aisles: string[]; units: string[] }

export interface Ingredient { name: string; qty: number; unit: string; note: string; pantry_id?: number; optional?: boolean; group?: string }

export interface Recipe {
  id: number; title: string; description: string; servings: number; prep_minutes: number; cook_minutes: number; course: string; cuisine: string; tags: string
  source_url: string; source_name: string; rating: number; favorite: boolean; notes: string; has_photo: boolean; image_url: string
  ingredients: Ingredient[]; steps: string[]; nutrition: Record<string, string>; times_cooked: number; last_cooked: string; archived: boolean
  created_by: number | null; created_at: string; updated_at: string; total_minutes: number; in_pantry?: number; planned?: number
}

export interface PantryMatch { pantry_id: number; name: string; quantity: number; unit: string; needed: number; enough: boolean; converted: boolean }

export interface PlanEntry {
  id: number; date: string; slot: string; recipe_id: number | null; title: string; servings: number; notes: string; cooked_at: string; cook_id: number | null; sort_order: number
  recipe_title?: string; recipe_course?: string; recipe_minutes?: number; recipe_has_photo?: boolean; recipe_image_url?: string; recipe_servings?: number
}

export interface List { id: number; name: string; store: string; sort_order: number; archived: boolean; created_at: string; open: number; checked: number }
export interface ListItem {
  id: number; list_id: number; name: string; qty: number; unit: string; note: string; aisle: string; checked: boolean; recipe_id: number | null; plan_id: number | null; pantry_id: number | null
  price: number; currency: string; sort_order: number; created_at: string; updated_at: string; recipe_title?: string
}

export interface PantryItem {
  id: number; inventory_id: number | null; name: string; quantity: number; unit: string; min_quantity: number; location: string; category: string; barcode: string; brand: string
  expiry_date: string; notes: string; photo_url: string; archived: boolean; updated_at: string; synced_at: string; low: boolean; expiring: boolean
}

export interface Cook { id: number; recipe_id: number | null; plan_id: number | null; date: string; servings: number; notes: string; rating: number; user_id: number | null; habit_marked: boolean; pantry_deducted: number; created_at: string; recipe_title?: string; user_name?: string }
export interface Purchase { id: number; list_id: number | null; list_name: string; date: string; store: string; amount: number; currency: string; items: number; item_names: string; paid_by: string; split_group_id: number; split_expense_id: number; pantry_restocked: number; notes: string; created_at: string }

export interface CookResult { cook: Cook; recipe: Recipe; deducted: string[]; not_in_pantry: string[]; habit_marked: boolean; warnings: string[] }
export interface CheckoutResult { purchase: Purchase; restocked: string[]; created: string[]; split_url?: string; warnings: string[]; list: List }

export interface HomeData {
  today: string; week: PlanEntry[]; lists: List[]; cooks: Cook[]; expiring: PantryItem[]; low: PantryItem[]; suggestions: (Recipe & { reason: string })[]
  stats: { recipes: number; pantry: number; pantry_low: number; pantry_expiring: number; cooked_month: number; cooked_year: number; cooked_days_30: number; shopping_open: number; spend_month: number; spend_year: number; currency: string }
}

export interface FoodProduct { barcode: string; name: string; brand: string; quantity: string; categories: string[]; image_url: string; source: string }
export interface InventoryItem { id: number; name: string; quantity: number; unit: string; min_quantity: number; barcode: string; brand: string; expiry_date: string; tags: string; location_name: string; category_name: string }
export interface LookupResult { barcode: string; source: 'pantry' | 'inventory' | 'openfoodfacts' | 'none'; pantry?: PantryItem; inventory?: InventoryItem; product?: FoodProduct; error?: string }
export interface SplitGroup { id: number; name: string; currency: string; archived: boolean; members: { id: number; name: string; user_id: number | null }[] }
export interface Habit { id: number; name: string; color: string; end_date: string | null }

export const cap = (s: string) => s ? s[0].toUpperCase() + s.slice(1).replace(/_/g, ' ') : ''
/** "1.5 kg", "2", "to taste" */
export function fmtQty(qty: number, unit: string): string {
  if (!qty) return unit || ''
  const q = Number.isInteger(qty) ? String(qty) : String(Math.round(qty * 100) / 100)
  if (!unit || unit === 'pcs') return q
  return `${q} ${unit}`
}
export function fmtMinutes(m: number): string {
  if (!m) return ''
  const h = Math.floor(m / 60), mm = m % 60
  return h ? (mm ? `${h} h ${mm} min` : `${h} h`) : `${mm} min`
}
export function recipeImage(r: { id: number; has_photo: boolean; image_url: string; updated_at?: string }): string {
  if (r.has_photo) return `/api/v1/recipes/${r.id}/photo?v=${r.updated_at || ''}`
  return r.image_url || ''
}
