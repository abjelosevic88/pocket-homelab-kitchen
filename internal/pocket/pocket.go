// Package pocket talks to sibling Pocket apps: Pocket Inventory (the pantry),
// Pocket Split (who paid for the groceries) and Pocket Habits ("cooked at
// home"). All three share the same bearer-token JSON API style.
package pocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and token are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, v any, operation ...string) error {
	if !c.Configured() {
		return errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if len(operation) > 0 {
		req.Header.Set("Idempotency-Key", operation[0])
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errors.New("token rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		_ = json.Unmarshal(b, &e)
		if e.Error != "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// ErrNotFound is a 404 from the sibling app.
var ErrNotFound = errors.New("not found")

func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	return c.do(ctx, http.MethodGet, path, q, nil, v)
}

// Health is the connection test for any Pocket app: /healthz plus a token check.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if !c.Configured() {
		return "", errors.New("not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var me map[string]any
	if err := c.get(ctx, "/api/v1/auth/me", nil, &me); err != nil {
		return out.Version, err
	}
	return out.Version, nil
}

// ---------------- Pocket Inventory ----------------

// Item is a Pocket Inventory item.
type Item struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Notes       string  `json:"notes"`
	Quantity    float64 `json:"quantity"`
	Unit        string  `json:"unit"`
	MinQuantity float64 `json:"min_quantity"`
	Barcode     string  `json:"barcode"`
	Brand       string  `json:"brand"`
	ExpiryDate  string  `json:"expiry_date"`
	Tags        string  `json:"tags"`
	Photo       string  `json:"photo"`
	Archived    bool    `json:"archived"`
	Location    string  `json:"location_name"`
	Category    string  `json:"category_name"`
	Low         bool    `json:"low"`
	UpdatedAt   string  `json:"updated_at"`
}

func decodeItems(raw json.RawMessage) ([]Item, error) {
	var list []Item
	if err := json.Unmarshal(raw, &list); err != nil {
		var wrap struct {
			Items []Item `json:"items"`
		}
		if err := json.Unmarshal(raw, &wrap); err != nil {
			return nil, err
		}
		list = wrap.Items
	}
	return list, nil
}

// ItemsByTag lists active items carrying a tag.
func (c *Client) ItemsByTag(ctx context.Context, tag string) ([]Item, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/api/v1/items", url.Values{"tag": {tag}, "sort": {"name"}}, &raw); err != nil {
		return nil, err
	}
	return decodeItems(raw)
}

// SearchItems looks up items by name.
func (c *Client) SearchItems(ctx context.Context, q string, limit int) ([]Item, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/api/v1/items", url.Values{"q": {q}, "limit": {fmt.Sprint(limit)}}, &raw); err != nil {
		return nil, err
	}
	return decodeItems(raw)
}

// LookupBarcode finds an inventory item by barcode (nil when none).
func (c *Client) LookupBarcode(ctx context.Context, code string) (*Item, error) {
	var it Item
	err := c.get(ctx, "/api/v1/lookup", url.Values{"barcode": {code}}, &it)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

// GetItem fetches one item.
func (c *Client) GetItem(ctx context.Context, id int64) (*Item, error) {
	var out struct {
		Item Item `json:"item"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/v1/items/%d", id), nil, &out); err != nil {
		return nil, err
	}
	return &out.Item, nil
}

// ItemInput creates or updates an inventory item.
type ItemInput struct {
	Name        string   `json:"name"`
	Notes       string   `json:"notes,omitempty"`
	Location    string   `json:"location,omitempty"`
	Category    string   `json:"category,omitempty"`
	Quantity    *float64 `json:"quantity,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	MinQuantity *float64 `json:"min_quantity,omitempty"`
	Barcode     string   `json:"barcode,omitempty"`
	Brand       string   `json:"brand,omitempty"`
	ExpiryDate  string   `json:"expiry_date,omitempty"`
	Tags        string   `json:"tags,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// CreateItem creates an inventory item.
func (c *Client) CreateItem(ctx context.Context, in ItemInput, operation ...string) (*Item, error) {
	var it Item
	if err := c.do(ctx, http.MethodPost, "/api/v1/items", nil, in, &it, operation...); err != nil {
		return nil, err
	}
	return &it, nil
}

// UpdateItem updates an inventory item (only the sent fields change).
func (c *Client) UpdateItem(ctx context.Context, id int64, in map[string]any) (*Item, error) {
	var it Item
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/items/%d", id), nil, in, &it); err != nil {
		return nil, err
	}
	return &it, nil
}

// AdjustItem changes the quantity by delta with a movement note.
func (c *Client) AdjustItem(ctx context.Context, id int64, delta float64, note string, operation ...string) (*Item, error) {
	var it Item
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/items/%d/adjust", id), nil, map[string]any{"delta": delta, "note": note}, &it, operation...); err != nil {
		return nil, err
	}
	return &it, nil
}

// SetItemQuantity sets an absolute quantity.
func (c *Client) SetItemQuantity(ctx context.Context, id int64, qty float64, note string, operation ...string) (*Item, error) {
	var it Item
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/items/%d/adjust", id), nil, map[string]any{"quantity": qty, "note": note}, &it, operation...); err != nil {
		return nil, err
	}
	return &it, nil
}

// ArchiveItem archives an inventory item.
func (c *Client) ArchiveItem(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/items/%d/archive", id), nil, nil, nil)
}

// Named is a location or category.
type Named struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Locations lists inventory locations.
func (c *Client) Locations(ctx context.Context) ([]Named, error) {
	var out []Named
	err := c.get(ctx, "/api/v1/locations", nil, &out)
	return out, err
}

// ---------------- Pocket Split ----------------

// Member is a member of a Split group.
type Member struct {
	ID     int64  `json:"id"`
	UserID *int64 `json:"user_id"`
	Name   string `json:"name"`
}

// Group is a Split group.
type Group struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Currency string   `json:"currency"`
	Archived bool     `json:"archived"`
	Members  []Member `json:"members"`
}

// Groups lists the caller's Split groups.
func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	var out []Group
	err := c.get(ctx, "/api/v1/groups", nil, &out)
	return out, err
}

// Group fetches one group with members.
func (c *Client) Group(ctx context.Context, id int64) (*Group, error) {
	var g Group
	if err := c.get(ctx, fmt.Sprintf("/api/v1/groups/%d", id), nil, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// Categories lists Split expense categories.
func (c *Client) Categories(ctx context.Context) ([]string, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/api/v1/categories", nil, &raw); err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil {
		return names, nil
	}
	var objs []struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil, err
	}
	for _, o := range objs {
		if o.Name != "" {
			names = append(names, o.Name)
		} else {
			names = append(names, o.ID)
		}
	}
	return names, nil
}

// ExpenseInput creates a Split expense. Amount is in minor units of Currency.
type ExpenseInput struct {
	Title     string `json:"title"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	PaidBy    int64  `json:"paid_by"`
	Date      string `json:"date"`
	Category  string `json:"category"`
	Notes     string `json:"notes"`
	SplitMode string `json:"split_mode"`
	Shares    []struct {
		MemberID int64 `json:"member_id"`
		Share    int64 `json:"share"`
	} `json:"shares"`
}

// Expense is the created expense (only what we keep).
type Expense struct {
	ID          int64  `json:"id"`
	GroupID     int64  `json:"group_id"`
	Title       string `json:"title"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	AmountGroup int64  `json:"amount_group"`
}

// CreateExpense posts an expense split evenly between every member.
func (c *Client) CreateExpense(ctx context.Context, g *Group, in ExpenseInput, operation ...string) (*Expense, error) {
	if in.SplitMode == "" {
		in.SplitMode = "even"
	}
	if len(in.Shares) == 0 {
		for _, m := range g.Members {
			in.Shares = append(in.Shares, struct {
				MemberID int64 `json:"member_id"`
				Share    int64 `json:"share"`
			}{m.ID, 1})
		}
	}
	var e Expense
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/groups/%d/expenses", g.ID), nil, in, &e, operation...); err != nil {
		return nil, err
	}
	return &e, nil
}

// ---------------- Pocket Habits ----------------

// Habit is a Pocket Habits habit.
type Habit struct {
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	Color   string  `json:"color"`
	EndDate *string `json:"end_date"`
}

// Habits lists habits.
func (c *Client) Habits(ctx context.Context) ([]Habit, error) {
	var out []Habit
	err := c.get(ctx, "/api/v1/habits", nil, &out)
	return out, err
}

// MarkHabit sets a habit day to done (1), missed (-1) or clear (0).
func (c *Client) MarkHabit(ctx context.Context, habitID int64, date string, status int) error {
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/habits/%d/entries/%s", habitID, date), nil, map[string]int{"status": status}, nil)
}
