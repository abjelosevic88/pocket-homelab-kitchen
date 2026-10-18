// Package foodfacts looks up a barcode on Open Food Facts.
package foodfacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Product is what a barcode resolves to.
type Product struct {
	Barcode    string   `json:"barcode"`
	Name       string   `json:"name"`
	Brand      string   `json:"brand"`
	Quantity   string   `json:"quantity"` // "500 g", "1 L"
	Categories []string `json:"categories"`
	ImageURL   string   `json:"image_url"`
	Source     string   `json:"source"`
}

// ErrNotFound means Open Food Facts has no product with that code.
var ErrNotFound = errors.New("no product with that barcode on Open Food Facts")

// Client queries the public API.
type Client struct {
	UserAgent string
	HTTP      *http.Client
}

// New creates a client.
func New(ua string) *Client {
	return &Client{UserAgent: ua, HTTP: &http.Client{Timeout: 12 * time.Second}}
}

// Lookup fetches a product.
func (c *Client) Lookup(ctx context.Context, code string) (*Product, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("barcode is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://world.openfoodfacts.org/api/v2/product/"+code+".json?fields=product_name,product_name_en,brands,quantity,categories_tags,image_front_url,image_url", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Open Food Facts HTTP %d", res.StatusCode)
	}
	var raw struct {
		Status  int `json:"status"`
		Product struct {
			Name     string   `json:"product_name"`
			NameEN   string   `json:"product_name_en"`
			Brands   string   `json:"brands"`
			Quantity string   `json:"quantity"`
			Cats     []string `json:"categories_tags"`
			ImgFront string   `json:"image_front_url"`
			Img      string   `json:"image_url"`
		} `json:"product"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if raw.Status != 1 || (raw.Product.Name == "" && raw.Product.NameEN == "") {
		return nil, ErrNotFound
	}
	p := &Product{Barcode: code, Name: raw.Product.Name, Brand: raw.Product.Brands, Quantity: raw.Product.Quantity, ImageURL: raw.Product.ImgFront, Source: "openfoodfacts"}
	if p.Name == "" {
		p.Name = raw.Product.NameEN
	}
	if p.ImageURL == "" {
		p.ImageURL = raw.Product.Img
	}
	if i := strings.Index(p.Brand, ","); i > 0 {
		p.Brand = strings.TrimSpace(p.Brand[:i])
	}
	for _, c := range raw.Product.Cats {
		c = strings.TrimPrefix(c, "en:")
		if !strings.Contains(c, ":") {
			p.Categories = append(p.Categories, strings.ReplaceAll(c, "-", " "))
		}
	}
	if len(p.Categories) > 6 {
		p.Categories = p.Categories[len(p.Categories)-6:]
	}
	return p, nil
}
