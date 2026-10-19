package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/store"
	"github.com/abjelosevic88/pocket-homelab-kitchen/internal/units"
)

func (s *Server) handleListLists(w http.ResponseWriter, r *http.Request) {
	ls, err := s.store.ListLists(r.Context(), queryBool(r, "archived"))
	if err != nil {
		s.fail(w, err, "lists")
		return
	}
	writeJSON(w, http.StatusOK, ls)
}

func (s *Server) handleCreateList(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Store string `json:"store"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a name is required")
		return
	}
	l := &store.List{Name: in.Name, Store: in.Store}
	if err := s.store.CreateList(r.Context(), l); err != nil {
		s.fail(w, err, "create list")
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleGetList(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.GetList(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get list")
		return
	}
	items, err := s.store.ListItems(r.Context(), l.ID)
	if err != nil {
		s.fail(w, err, "items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": l, "items": items})
}

func (s *Server) handleUpdateList(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.GetList(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get list")
		return
	}
	var in struct {
		Name     *string `json:"name"`
		Store    *string `json:"store"`
		Archived *bool   `json:"archived"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		l.Name = *in.Name
	}
	if in.Store != nil {
		l.Store = *in.Store
	}
	if in.Archived != nil {
		l.Archived = *in.Archived
	}
	if err := s.store.UpdateList(r.Context(), l); err != nil {
		s.fail(w, err, "update list")
		return
	}
	l, _ = s.store.GetList(r.Context(), l.ID)
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteList(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteList(r.Context(), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete list")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type itemInput struct {
	Name     *string  `json:"name"`
	Qty      *float64 `json:"qty"`
	Unit     *string  `json:"unit"`
	Note     *string  `json:"note"`
	Aisle    *string  `json:"aisle"`
	Checked  *bool    `json:"checked"`
	Price    *int64   `json:"price"`
	Currency *string  `json:"currency"`
	PantryID *int64   `json:"pantry_id"`
	ListID   *int64   `json:"list_id"`
}

func applyItem(it *store.ListItem, in itemInput) string {
	if in.Name != nil {
		it.Name = strings.TrimSpace(*in.Name)
	}
	if it.Name == "" {
		return "a name is required"
	}
	if in.Qty != nil {
		it.Qty = max(0, *in.Qty)
	}
	if in.Unit != nil {
		it.Unit = units.Normalize(*in.Unit)
	}
	if in.Note != nil {
		it.Note = strings.TrimSpace(*in.Note)
	}
	if in.Aisle != nil {
		it.Aisle = strings.ToLower(strings.TrimSpace(*in.Aisle))
	}
	if it.Aisle == "" {
		it.Aisle = aisleFor(it.Name)
	}
	if in.Checked != nil {
		it.Checked = *in.Checked
	}
	if in.Price != nil {
		it.Price = max(0, *in.Price)
	}
	if in.Currency != nil {
		it.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if in.PantryID != nil {
		if *in.PantryID > 0 {
			it.PantryID = in.PantryID
		} else {
			it.PantryID = nil
		}
	}
	if in.ListID != nil && *in.ListID > 0 {
		it.ListID = *in.ListID
	}
	return ""
}

// handleCreateItem adds a line; a plain string body {"name":"milk 2 l"} is parsed like an ingredient.
func (s *Server) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.GetList(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get list")
		return
	}
	var in itemInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	it := &store.ListItem{ListID: l.ID}
	if uid := actorID(r.Context()); uid > 0 {
		it.CreatedBy = &uid
	}
	// "2 kg potatoes" typed in one go
	if in.Name != nil && in.Qty == nil && in.Unit == nil {
		p := parseQuick(*in.Name)
		in.Name, in.Qty, in.Unit = &p.name, &p.qty, &p.unit
	}
	if msg := applyItem(it, in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if it.PantryID == nil {
		if p, err := s.store.FindPantryByName(r.Context(), it.Name); err == nil {
			it.PantryID = &p.ID
		}
	}
	if err := s.store.CreateItem(r.Context(), it); err != nil {
		s.fail(w, err, "create item")
		return
	}
	writeJSON(w, http.StatusCreated, it)
}

type quick struct {
	name string
	qty  float64
	unit string
}

// parseQuick reads "2 kg potatoes", "milk 2l", "eggs x12", "3 onions".
func parseQuick(s string) quick {
	s = strings.TrimSpace(s)
	// try leading quantity first
	if p := parseLeading(s); p.qty > 0 {
		return p
	}
	// trailing "milk 2 l" / "eggs 12"
	f := strings.Fields(s)
	if len(f) >= 2 {
		last := f[len(f)-1]
		if q, u, ok := numUnit(last); ok {
			return quick{name: strings.Join(f[:len(f)-1], " "), qty: q, unit: u}
		}
		if len(f) >= 3 {
			if q, _, ok := numUnit(f[len(f)-2]); ok && units.FamilyOf(last) != units.None && units.Normalize(last) != "" {
				return quick{name: strings.Join(f[:len(f)-2], " "), qty: q, unit: units.Normalize(last)}
			}
		}
	}
	return quick{name: s}
}

func parseLeading(s string) quick {
	f := strings.Fields(s)
	if len(f) < 2 {
		return quick{name: s}
	}
	q, u, ok := numUnit(f[0])
	if !ok {
		return quick{name: s}
	}
	rest := f[1:]
	if u == "" && len(rest) >= 2 && units.FamilyOf(rest[0]) != units.None && units.Normalize(rest[0]) != "" {
		u = units.Normalize(rest[0])
		rest = rest[1:]
	}
	return quick{name: strings.Join(rest, " "), qty: q, unit: u}
}

// numUnit parses "2", "2kg", "1.5l", "x12".
func numUnit(tok string) (float64, string, bool) {
	tok = strings.TrimPrefix(strings.ToLower(tok), "x")
	i := 0
	for i < len(tok) && (tok[i] >= '0' && tok[i] <= '9' || tok[i] == '.' || tok[i] == ',') {
		i++
	}
	if i == 0 {
		return 0, "", false
	}
	var q float64
	if _, err := fmt.Sscanf(strings.ReplaceAll(tok[:i], ",", "."), "%g", &q); err != nil || q <= 0 {
		return 0, "", false
	}
	u := tok[i:]
	if u != "" && (units.FamilyOf(u) == units.None || units.Normalize(u) == "") {
		return 0, "", false
	}
	return q, units.Normalize(u), true
}

func (s *Server) getListItem(w http.ResponseWriter, r *http.Request) *store.ListItem {
	it, err := s.store.GetItem(r.Context(), idParam(r, "iid"))
	if err != nil || it.ListID != idParam(r, "id") {
		writeErr(w, http.StatusNotFound, "not found")
		return nil
	}
	return it
}

func (s *Server) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	it := s.getListItem(w, r)
	if it == nil {
		return
	}
	var in itemInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := applyItem(it, in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateItem(r.Context(), it); err != nil {
		s.fail(w, err, "update item")
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	it := s.getListItem(w, r)
	if it == nil {
		return
	}
	if err := s.store.DeleteItem(r.Context(), it.ID); err != nil {
		s.fail(w, err, "delete item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleToggleItem(w http.ResponseWriter, r *http.Request) {
	it := s.getListItem(w, r)
	if it == nil {
		return
	}
	it.Checked = !it.Checked
	if err := s.store.UpdateItem(r.Context(), it); err != nil {
		s.fail(w, err, "update item")
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) handleCheckAll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Checked bool `json:"checked"`
	}
	_ = decode(r, &in)
	if err := s.store.SetAllChecked(r.Context(), idParam(r, "id"), in.Checked); err != nil {
		s.fail(w, err, "check all")
		return
	}
	l, _ := s.store.GetList(r.Context(), idParam(r, "id"))
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleClearChecked(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.DeleteCheckedItems(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "clear")
		return
	}
	l, _ := s.store.GetList(r.Context(), idParam(r, "id"))
	writeJSON(w, http.StatusOK, map[string]any{"removed": n, "list": l})
}

// handleAddLowPantry puts every low / empty pantry item on the list.
func (s *Server) handleAddLowPantry(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.GetList(r.Context(), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get list")
		return
	}
	low, err := s.store.ListPantry(r.Context(), store.PantryFilter{Low: true})
	if err != nil {
		s.fail(w, err, "pantry")
		return
	}
	uid := actorID(r.Context())
	n := 0
	for _, p := range low {
		qty := p.MinQuantity*2 - p.Quantity
		if qty <= 0 {
			qty = p.MinQuantity
		}
		pid := p.ID
		if it := s.mergeItem(r, l.ID, p.Name, qty, p.Unit, "", aisleFor(p.Name), nil, nil, &pid, uid); it != nil {
			n++
		}
	}
	l, _ = s.store.GetList(r.Context(), l.ID)
	writeJSON(w, http.StatusOK, map[string]any{"added": n, "list": l})
}

func (s *Server) handleSuggestItems(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.ItemNameSuggestions(r.Context(), 300)
	if err != nil {
		s.fail(w, err, "suggest")
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if q != "" {
		var f []string
		for _, n := range names {
			if strings.Contains(strings.ToLower(n), q) {
				f = append(f, n)
			}
		}
		names = f
	}
	if names == nil {
		names = []string{}
	}
	if len(names) > 20 {
		names = names[:20]
	}
	writeJSON(w, http.StatusOK, names)
}

// checkoutInput finishes a shopping trip.
type checkoutInput struct {
	Amount     int64  `json:"amount"` // minor units
	Currency   string `json:"currency"`
	Date       string `json:"date"`
	Store      string `json:"store"`
	Notes      string `json:"notes"`
	Restock    *bool  `json:"restock"`     // put checked lines into the pantry (default true)
	Split      bool   `json:"split"`       // post to Pocket Split
	PaidBy     int64  `json:"paid_by"`     // Split member id (default from settings)
	Category   string `json:"category"`    // Split category
	Remove     *bool  `json:"remove"`      // drop the checked lines afterwards (default true)
	OnlyAmount bool   `json:"only_amount"` // no lines: just record the spend
}

type checkoutResult struct {
	Purchase  store.Purchase `json:"purchase"`
	Restocked []string       `json:"restocked"`
	Created   []string       `json:"created"`
	SplitURL  string         `json:"split_url,omitempty"`
	Warnings  []string       `json:"warnings"`
	List      *store.List    `json:"list"`
}

// handleCheckout: the checked lines were bought. Restock the pantry (Pocket
// Inventory when connected), post the receipt to Pocket Split, remember it.
func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.store.GetList(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get list")
		return
	}
	var in checkoutInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Date == "" {
		in.Date = store.Today()
	}
	if !validDate(in.Date) {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	cl := s.clients(ctx)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = cl.st.HomeCurrency
	}
	if in.Amount < 0 || (in.Amount > 0 && !currency.Valid(in.Currency)) {
		writeErr(w, http.StatusBadRequest, "amount must be positive with a known currency")
		return
	}
	items, err := s.store.ListItems(ctx, l.ID)
	if err != nil {
		s.fail(w, err, "items")
		return
	}
	var bought []store.ListItem
	for _, it := range items {
		if it.Checked {
			bought = append(bought, it)
		}
	}
	if len(bought) == 0 && !in.OnlyAmount {
		writeErr(w, http.StatusBadRequest, "nothing is checked off yet")
		return
	}
	res := &checkoutResult{Restocked: []string{}, Created: []string{}, Warnings: []string{}}
	restock := in.Restock == nil || *in.Restock
	if restock {
		for _, it := range bought {
			name, warn := s.restockItem(r, cl, it)
			if warn != "" {
				res.Warnings = append(res.Warnings, warn)
			}
			if name != "" {
				res.Restocked = append(res.Restocked, name)
			}
		}
	}
	// sum line prices when no total was typed
	if in.Amount == 0 {
		for _, it := range bought {
			if it.Price > 0 && (it.Currency == "" || it.Currency == in.Currency) {
				in.Amount += it.Price
			}
		}
	}
	names := make([]string, 0, len(bought))
	for _, it := range bought {
		names = append(names, it.Name)
	}
	uid := actorID(ctx)
	p := &store.Purchase{ListName: l.Name, Date: in.Date, Store: strings.TrimSpace(in.Store), Amount: in.Amount, Currency: in.Currency, Items: len(bought), ItemNames: strings.Join(names, ", "), Notes: strings.TrimSpace(in.Notes), PantryRestocked: len(res.Restocked)}
	p.ListID = &l.ID
	if uid > 0 {
		p.UserID = &uid
	}
	if in.Split && in.Amount > 0 {
		if !cl.split.Configured() || cl.st.SplitGroupID == 0 {
			res.Warnings = append(res.Warnings, "Pocket Split is not set up (Settings → Integrations)")
		} else {
			g, err := cl.split.Group(ctx, cl.st.SplitGroupID)
			if err != nil {
				res.Warnings = append(res.Warnings, "Pocket Split: "+err.Error())
			} else {
				paidBy := in.PaidBy
				if paidBy == 0 {
					paidBy = cl.st.SplitMemberID
				}
				if paidBy == 0 && len(g.Members) > 0 {
					paidBy = g.Members[0].ID
				}
				cat := in.Category
				if cat == "" {
					cat = cl.st.SplitCategory
				}
				title := "Groceries"
				if p.Store != "" {
					title += " · " + p.Store
				}
				notes := p.ItemNames
				if len(notes) > 900 {
					notes = notes[:900] + "…"
				}
				e, err := cl.split.CreateExpense(ctx, g, pocket.ExpenseInput{Title: title, Amount: in.Amount, Currency: in.Currency, PaidBy: paidBy, Date: in.Date, Category: cat, Notes: notes})
				if err != nil {
					res.Warnings = append(res.Warnings, "Pocket Split: "+err.Error())
				} else {
					p.SplitGroupID, p.SplitExpenseID = g.ID, e.ID
					for _, m := range g.Members {
						if m.ID == paidBy {
							p.PaidBy = m.Name
						}
					}
					base := cl.st.SplitPublicURL
					if base == "" {
						base = cl.st.SplitURL
					}
					res.SplitURL = fmt.Sprintf("%s/groups/%d", base, g.ID)
				}
			}
		}
	}
	if err := s.store.CreatePurchase(ctx, p); err != nil {
		s.fail(w, err, "save purchase")
		return
	}
	if in.Remove == nil || *in.Remove {
		_, _ = s.store.DeleteCheckedItems(ctx, l.ID)
	}
	res.Purchase = *p
	res.List, _ = s.store.GetList(ctx, l.ID)
	s.publish("kitchen.checkout", fmt.Sprintf("list:%d", l.ID), map[string]any{
		"list_id": l.ID, "list": l.Name, "amount": in.Amount, "currency": in.Currency, "date": in.Date, "store": in.Store,
	})
	writeJSON(w, http.StatusOK, res)
}

// restockItem adds a bought line to the pantry: tops up the matching row
// (through Inventory when mirrored) or creates a new one.
func (s *Server) restockItem(r *http.Request, cl clients, it store.ListItem) (string, string) {
	ctx := r.Context()
	var p *store.PantryItem
	var err error
	if it.PantryID != nil {
		p, err = s.store.GetPantry(ctx, *it.PantryID)
	}
	if p == nil || err != nil {
		p, err = s.store.FindPantryByName(ctx, it.Name)
	}
	qty := it.Qty
	if qty <= 0 {
		qty = 1
	}
	if err == nil && p != nil {
		if p.Archived {
			p.Archived = false
			if p.InventoryID != nil && cl.inventory.Configured() {
				_, _ = cl.inventory.UpdateItem(ctx, *p.InventoryID, map[string]any{"tags": cl.st.InventoryTag})
			}
		}
		delta := qty
		if conv, ok := units.Convert(qty, it.Unit, p.Unit); ok {
			delta = conv
		} else if it.Unit != "" && p.Unit != "" && units.Normalize(it.Unit) != units.Normalize(p.Unit) {
			return "", fmt.Sprintf("%s: bought in %s, pantry counts %s — added 1", it.Name, it.Unit, p.Unit)
		}
		if err := s.adjustPantry(r, cl, p, delta, "Bought ("+it.Name+")"); err != nil {
			return "", p.Name + ": " + err.Error()
		}
		return fmt.Sprintf("%s (%s)", p.Name, units.FormatZero(p.Quantity, p.Unit)), ""
	}
	np := &store.PantryItem{Name: it.Name, Quantity: qty, Unit: units.Normalize(it.Unit), Location: cl.st.InventoryLocation, Category: it.Aisle}
	if err := s.createPantry(r, cl, np); err != nil {
		return "", it.Name + ": " + err.Error()
	}
	return fmt.Sprintf("%s (new, %s)", np.Name, units.Format(np.Quantity, np.Unit)), ""
}
