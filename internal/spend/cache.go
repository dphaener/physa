package spend

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dphaener/physa/internal/adapter"
	"github.com/dphaener/physa/internal/configdir"
)

// File is the name of the machine's spend cache in the settings directory.
const File = "spend.json"

// Path is where the machine keeps its spend cache: File in the settings
// directory configdir gives.
func Path() (string, error) {
	dir, err := configdir.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, File), nil
}

// Price is a model's input price, in millionths of a dollar per million
// input tokens, and when it was worked out.
type Price struct {
	PerMillion int64     `json:"micro_usd_per_million_input"`
	At         time.Time `json:"at"`
}

// Default is an adapter's default model: the one its sessions ran on, and
// when one last did.
type Default struct {
	Model string    `json:"model"`
	At    time.Time `json:"at"`
}

// Cache is what the machine knows of prices: each model's input price and
// each adapter's default model, each the newest learned. It is safe for
// concurrent use.
type Cache struct {
	mu       sync.Mutex
	path     string
	Models   map[string]Price   `json:"models"`
	Defaults map[string]Default `json:"defaults"`
}

// Open reads the cache kept at path; an empty path keeps one in memory only.
// A file that is missing or cannot be read is taken as empty, since it is a
// cache, and Save replaces it.
func Open(path string) *Cache {
	c := read(path)
	c.path = path
	return c
}

// read is the cache in the file at path, or an empty one.
func read(path string) *Cache {
	c := &Cache{}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, c) != nil {
			c = &Cache{}
		}
	}
	if c.Models == nil {
		c.Models = map[string]Price{}
	}
	if c.Defaults == nil {
		c.Defaults = map[string]Default{}
	}
	return c
}

// Learn keeps what r, a session of the adapter named agent that ended at
// at, says of prices: the input price of each model whose cost it carries,
// backed out of its dollars, and its model as the adapter's default. An entry
// newer than at is kept.
func (c *Cache) Learn(agent string, r adapter.Result, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range r.Cost {
		if p, ok := InputPrice(m); ok && !c.Models[name(m.Model)].At.After(at) {
			c.Models[name(m.Model)] = Price{PerMillion: p, At: at}
		}
	}
	if r.Model != "" && !c.Defaults[agent].At.After(at) {
		c.Defaults[agent] = Default{Model: name(r.Model), At: at}
	}
}

// Weighted is what r, a session of the adapter named agent, spent in
// weighted tokens: each model's dollars ÷ the input price of the adapter's
// default model, or, for a model that reported no dollars, its tokens by
// kind at its cached price. A model with no price, and every model while the
// default model has none, counts at raw tokens and leaves the Spend
// Unpriced.
func (c *Cache) Weighted(agent string, r adapter.Result) Spend {
	c.mu.Lock()
	defer c.mu.Unlock()
	ref, refOK := c.Models[name(c.Defaults[agent].Model)]
	var s Spend
	for _, m := range costOf(r) {
		usd := float64(m.MicroUSD)
		if p, ok := c.Models[name(m.Model)]; usd <= 0 && ok {
			usd = float64(units(m)) * float64(p.PerMillion) / 1e9
		}
		if !refOK || ref.PerMillion <= 0 || usd <= 0 {
			s.Tokens += m.Raw()
			s.Unpriced = s.Unpriced || m.Raw() > 0
			continue
		}
		s.Tokens += int64(math.Round(usd * 1e6 / float64(ref.PerMillion)))
		s.MicroUSD += int64(math.Round(usd))
	}
	return s
}

// Price is model's cached input price, if any.
func (c *Cache) Price(model string) (Price, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.Models[name(model)]
	return p, ok
}

// Reference is the input price one weighted token of the adapter named agent
// is worth: its default model's, if any is cached.
func (c *Cache) Reference(agent string) (Price, bool) {
	c.mu.Lock()
	d := c.Defaults[agent]
	c.mu.Unlock()
	return c.Price(d.Model)
}

// Save writes the cache to its file, merged with what the file holds now so
// that another run's newer entries survive, newest wins. A cache with no path
// is not written.
func (c *Cache) Save() error {
	if c.path == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	disk := read(c.path)
	for k, v := range disk.Models {
		if v.At.After(c.Models[k].At) {
			c.Models[k] = v
		}
	}
	for k, v := range disk.Defaults {
		if v.At.After(c.Defaults[k].At) {
			c.Defaults[k] = v
		}
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(c.path, append(b, '\n'))
}

// writeFile replaces the file at path with b in one step, creating its
// directory, private to the person, when it is missing.
func writeFile(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".spend-*.json")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
