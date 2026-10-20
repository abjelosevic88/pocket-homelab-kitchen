package store

// User is an account that can sign in. The first user is the admin (the
// owner); admins manage accounts and the few box-wide settings. Everything
// else — recipes, the plan, lists, the pantry, the cook log, checkouts and the
// integration settings — belongs to exactly one user.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Lists the UI offers; free text is always allowed where it makes sense.
var (
	Courses = []string{"breakfast", "lunch", "dinner", "dessert", "snack", "side", "drink", "sauce", "other"}
	Slots   = []string{"breakfast", "lunch", "dinner", "snack"}
	Aisles  = []string{"produce", "dairy", "meat", "fish", "bakery", "pantry", "frozen", "drinks", "spices", "household", "other"}
	Units   = []string{"g", "kg", "ml", "l", "tsp", "tbsp", "cup", "pcs", "clove", "slice", "can", "pack", "bunch", "pinch"}
)

func in(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ValidCourse reports whether c is a known course.
func ValidCourse(c string) bool { return in(Courses, c) }

// ValidSlot reports whether s is a plan slot.
func ValidSlot(s string) bool { return in(Slots, s) }

// Ingredient is one line of a recipe.
type Ingredient struct {
	Name     string  `json:"name"`
	Qty      float64 `json:"qty"`  // 0 = "to taste" / unspecified
	Unit     string  `json:"unit"` // g, ml, tbsp, pcs… free text, normalised for shopping
	Note     string  `json:"note"` // "finely chopped"
	PantryID int64   `json:"pantry_id,omitempty"`
	Optional bool    `json:"optional,omitempty"`
	Group    string  `json:"group,omitempty"` // "For the sauce"
}

// Recipe is one dish.
type Recipe struct {
	ID          int64             `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Servings    int               `json:"servings"`
	PrepMinutes int               `json:"prep_minutes"`
	CookMinutes int               `json:"cook_minutes"`
	Course      string            `json:"course"`
	Cuisine     string            `json:"cuisine"`
	Tags        string            `json:"tags"`
	SourceURL   string            `json:"source_url"`
	SourceName  string            `json:"source_name"`
	Rating      int               `json:"rating"`
	Favorite    bool              `json:"favorite"`
	Notes       string            `json:"notes"`
	PhotoPath   string            `json:"-"`
	HasPhoto    bool              `json:"has_photo"`
	ImageURL    string            `json:"image_url"`
	Ingredients []Ingredient      `json:"ingredients"`
	Steps       []string          `json:"steps"`
	Nutrition   map[string]string `json:"nutrition"`
	TimesCooked int               `json:"times_cooked"`
	LastCooked  string            `json:"last_cooked"`
	Archived    bool              `json:"archived"`
	CreatedBy   *int64            `json:"created_by"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`

	// Derived
	TotalMinutes int `json:"total_minutes"`
	// How many ingredient lines the pantry can cover right now (0..len).
	InPantry int `json:"in_pantry,omitempty"`
	Planned  int `json:"planned,omitempty"` // upcoming plan entries
}

// PlanEntry is one slot on one day.
type PlanEntry struct {
	ID        int64  `json:"id"`
	Date      string `json:"date"`
	Slot      string `json:"slot"`
	RecipeID  *int64 `json:"recipe_id"`
	Title     string `json:"title"`
	Servings  int    `json:"servings"`
	Notes     string `json:"notes"`
	CookedAt  string `json:"cooked_at"`
	CookID    *int64 `json:"cook_id"`
	SortOrder int    `json:"sort_order"`
	CreatedBy *int64 `json:"created_by"`
	CreatedAt string `json:"created_at"`

	// Derived from the recipe
	RecipeTitle    string `json:"recipe_title,omitempty"`
	RecipeCourse   string `json:"recipe_course,omitempty"`
	RecipeMinutes  int    `json:"recipe_minutes,omitempty"`
	RecipeHasPhoto bool   `json:"recipe_has_photo,omitempty"`
	RecipeImageURL string `json:"recipe_image_url,omitempty"`
	RecipeServings int    `json:"recipe_servings,omitempty"`
}

// DisplayTitle is the recipe title or the free text.
func (p *PlanEntry) DisplayTitle() string {
	if p.RecipeTitle != "" {
		return p.RecipeTitle
	}
	return p.Title
}

// List is a shopping list.
type List struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Store     string `json:"store"`
	SortOrder int    `json:"sort_order"`
	Archived  bool   `json:"archived"`
	CreatedAt string `json:"created_at"`

	// Derived
	Open    int `json:"open"`
	Checked int `json:"checked"`
}

// ListItem is one line on a shopping list.
type ListItem struct {
	ID        int64   `json:"id"`
	ListID    int64   `json:"list_id"`
	Name      string  `json:"name"`
	Qty       float64 `json:"qty"`
	Unit      string  `json:"unit"`
	Note      string  `json:"note"`
	Aisle     string  `json:"aisle"`
	Checked   bool    `json:"checked"`
	RecipeID  *int64  `json:"recipe_id"`
	PlanID    *int64  `json:"plan_id"`
	PantryID  *int64  `json:"pantry_id"`
	Price     int64   `json:"price"`
	Currency  string  `json:"currency"`
	SortOrder int     `json:"sort_order"`
	CreatedBy *int64  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`

	RecipeTitle string `json:"recipe_title,omitempty"`
}

// PantryItem is something on the shelf (mirrors a Pocket Inventory item when connected).
type PantryItem struct {
	ID          int64   `json:"id"`
	InventoryID *int64  `json:"inventory_id"`
	Name        string  `json:"name"`
	Quantity    float64 `json:"quantity"`
	Unit        string  `json:"unit"`
	MinQuantity float64 `json:"min_quantity"`
	Location    string  `json:"location"`
	Category    string  `json:"category"`
	Barcode     string  `json:"barcode"`
	Brand       string  `json:"brand"`
	ExpiryDate  string  `json:"expiry_date"`
	Notes       string  `json:"notes"`
	PhotoURL    string  `json:"photo_url"`
	Archived    bool    `json:"archived"`
	UpdatedAt   string  `json:"updated_at"`
	SyncedAt    string  `json:"synced_at"`

	// Derived
	Low      bool `json:"low"`
	Expiring bool `json:"expiring"`          // within 7 days or already past
	UsedBy   int  `json:"used_by,omitempty"` // recipes referencing it
}

// Cook is one cook-log row.
type Cook struct {
	ID             int64  `json:"id"`
	RecipeID       *int64 `json:"recipe_id"`
	PlanID         *int64 `json:"plan_id"`
	Date           string `json:"date"`
	Servings       int    `json:"servings"`
	Notes          string `json:"notes"`
	Rating         int    `json:"rating"`
	UserID         *int64 `json:"user_id"`
	HabitMarked    bool   `json:"habit_marked"`
	PantryDeducted int    `json:"pantry_deducted"`
	CreatedAt      string `json:"created_at"`

	RecipeTitle string `json:"recipe_title,omitempty"`
	UserName    string `json:"user_name,omitempty"`
}

// Purchase is one checkout of a shopping list.
type Purchase struct {
	ID              int64  `json:"id"`
	ListID          *int64 `json:"list_id"`
	ListName        string `json:"list_name"`
	Date            string `json:"date"`
	Store           string `json:"store"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	Items           int    `json:"items"`
	ItemNames       string `json:"item_names"`
	PaidBy          string `json:"paid_by"`
	SplitGroupID    int64  `json:"split_group_id"`
	SplitExpenseID  int64  `json:"split_expense_id"`
	PantryRestocked int    `json:"pantry_restocked"`
	Notes           string `json:"notes"`
	UserID          *int64 `json:"user_id"`
	CreatedAt       string `json:"created_at"`
}

// Settings are one user's preferences and integrations (user_settings) merged
// with the box-wide title / Open Food Facts / setup flag (settings).
type Settings struct {
	Title             string `json:"title"`
	HomeCurrency      string `json:"home_currency"`
	DateFormat        string `json:"date_format"`
	TimeFormat        string `json:"time_format"`
	WeekStart         string `json:"week_start"` // mon | sun
	DefaultServings   int    `json:"default_servings"`
	NotifyURL         string `json:"notify_url"`
	NotifyFormat      string `json:"notify_format"` // ntfy | apprise | json
	NotifyHour        int    `json:"notify_hour"`
	NotifyPlan        bool   `json:"notify_plan"`   // daily "today's plan" push
	NotifyExpiry      bool   `json:"notify_expiry"` // pantry items about to expire
	InventoryURL      string `json:"inventory_url"`
	InventoryToken    string `json:"inventory_token"`
	InventoryTag      string `json:"inventory_tag"`      // items with this tag are the pantry
	InventoryLocation string `json:"inventory_location"` // default location for items the kitchen creates
	InventoryAutoSync bool   `json:"inventory_auto_sync"`
	SplitURL          string `json:"split_url"`
	SplitToken        string `json:"split_token"`
	SplitPublicURL    string `json:"split_public_url"`
	SplitGroupID      int64  `json:"split_group_id"`
	SplitMemberID     int64  `json:"split_member_id"` // default payer
	SplitCategory     string `json:"split_category"`
	HabitsURL         string `json:"habits_url"`
	HabitsToken       string `json:"habits_token"`
	HabitID           int64  `json:"habit_id"`
	// How this person reaches each Pocket app: "core" (an app link through
	// Pocket Core), "direct" (the URL + token above) or "off"; "" = not chosen
	// yet (direct when a token is saved, else Pocket Core).
	InventoryMode string `json:"inventory_mode"`
	SplitMode     string `json:"split_mode"`
	HabitsMode    string `json:"habits_mode"`
	FoodFacts     bool   `json:"food_facts"`
	SetupComplete bool   `json:"setup_complete"`
}
