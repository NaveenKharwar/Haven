package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // so time zones work even when the server has none installed
)

// Widgets: weather, local news, currency rates and market prices.
//
// Haven's server fetches the data (so the browser never talks to outside
// sites, and a TV doesn't need to), caches it, and keeps showing the last
// good data if a source is down. All sources are free and need no API key:
//   weather   Open-Meteo            (open-meteo.com)
//   news      Google News RSS       (news.google.com)
//   currency  Frankfurter / ECB     (frankfurter.dev)
//   markets   Yahoo Finance charts  (unofficial; cached 15 min)

// --- settings -------------------------------------------------------------

type Place struct {
	Name    string  `json:"name"`
	Region  string  `json:"region,omitempty"`
	Country string  `json:"country,omitempty"`
	CC      string  `json:"cc,omitempty"` // ISO country code, e.g. IN
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

type NewsSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type WidgetSettings struct {
	Sources    []NewsSource `json:"sources"`         // publisher feeds shown next to the Google News tabs
	Places     []Place      `json:"places"`          // up to 5; the first is the main one
	Place      *Place       `json:"place,omitempty"` // older single-city setting, migrated into Places
	Units      string       `json:"units"`           // metric or imperial
	Home       string       `json:"home"`            // home currency, e.g. INR
	Currencies []string     `json:"currencies"`
	Markets    []string     `json:"markets"`   // Yahoo symbols, e.g. GC=F, ^NSEI, RELIANCE.NS
	NewsScope  string       `json:"newsScope"` // local or top
	Hidden     []string     `json:"hidden"`    // widgets switched off: weather, news, currency, markets
	Order      []string     `json:"order"`     // left-to-right order of the widgets on the Overview

	Clocks      []string      `json:"clocks"`      // time zones shown next to the local time, e.g. Asia/Kolkata
	ClockFormat string        `json:"clockFormat"` // "24" or "12" hour time
	Notes       string        `json:"notes"`       // the shared sticky note
	Links       []QuickLink   `json:"links"`       // quick links
	Checks      []StatusCheck `json:"checks"`      // things to watch
}

type QuickLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// widgetKeys are the Overview widgets, in their default order.
var widgetKeys = []string{"weather", "markets", "currency", "news", "clock", "notes", "links", "status"}

type Background struct {
	Shader string  `json:"shader"` // "" = plain faceplate
	Preset string  `json:"preset"`
	Dim    int     `json:"dim"`   // 0-90 % darkening over the shader
	Speed  float64 `json:"speed"` // 0 = still, 1 = normal
}

type UISettings struct {
	BackgroundWeb Background     `json:"backgroundWeb"`
	BackgroundTV  Background     `json:"backgroundTv"`
	Widgets       WidgetSettings `json:"widgets"`
}

const maxPlaces = 5

// normalize moves an older single-city setting into the list.
func (w *WidgetSettings) normalize() {
	if len(w.Places) == 0 && w.Place != nil {
		w.Places = []Place{*w.Place}
	}
	w.Place = nil
	if w.Places == nil {
		w.Places = []Place{}
	}
	if w.Clocks == nil {
		w.Clocks = []string{}
	}
	if w.ClockFormat != "12" {
		w.ClockFormat = "24"
	}
	if w.Links == nil {
		w.Links = []QuickLink{}
	}
	if w.Checks == nil {
		w.Checks = []StatusCheck{}
	}
	w.Order = cleanKeys(w.Order, true)
	w.Hidden = cleanKeys(w.Hidden, false)
}

// cleanKeys keeps only known widget names, once each. With fill, any widget
// that is missing is added at the end in its default order, so a new widget
// or an old saved layout never loses one.
func cleanKeys(in []string, fill bool) []string {
	known := map[string]bool{}
	for _, k := range widgetKeys {
		known[k] = true
	}
	seen := map[string]bool{}
	out := []string{}
	for _, k := range in {
		if known[k] && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	if fill {
		for _, k := range widgetKeys {
			if !seen[k] {
				out = append(out, k)
			}
		}
	}
	return out
}

// defaultCountry guesses the country from the server's time zone, using the
// system's own zone table (e.g. Asia/Kolkata -> IN).
func defaultCountry() string {
	tz := hostTZ()
	for _, f := range []string{"/usr/share/zoneinfo/zone1970.tab", "/usr/share/zoneinfo/zone.tab"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			p := strings.Split(line, "\t")
			if len(p) >= 3 && p[2] == tz {
				return strings.Split(p[0], ",")[0]
			}
		}
	}
	return "US"
}

var countryCurrency = map[string]string{
	"IN": "INR", "US": "USD", "GB": "GBP", "CA": "CAD", "AU": "AUD", "NZ": "NZD", "JP": "JPY", "CN": "CNY",
	"SG": "SGD", "AE": "AED", "CH": "CHF", "SE": "SEK", "NO": "NOK", "DK": "DKK", "ZA": "ZAR", "BR": "BRL",
	"MX": "MXN", "KR": "KRW", "HK": "HKD", "PL": "PLN", "TR": "TRY", "ID": "IDR", "MY": "MYR", "TH": "THB",
	"PH": "PHP", "IL": "ILS", "CZ": "CZK", "HU": "HUF", "RO": "RON", "IS": "ISK",
	"DE": "EUR", "FR": "EUR", "ES": "EUR", "IT": "EUR", "NL": "EUR", "BE": "EUR", "AT": "EUR", "IE": "EUR",
	"PT": "EUR", "FI": "EUR", "GR": "EUR", "SK": "EUR", "SI": "EUR", "EE": "EUR", "LV": "EUR", "LT": "EUR", "LU": "EUR",
}

func defaultWidgetSettings() WidgetSettings {
	cc := defaultCountry()
	home := countryCurrency[cc]
	if home == "" {
		home = "USD"
	}
	var cur []string
	for _, c := range []string{"USD", "EUR", "GBP", "JPY"} { // all are ECB reference currencies
		if c != home && len(cur) < 3 {
			cur = append(cur, c)
		}
	}
	markets := []string{"GC=F", "SI=F", "^GSPC", "^IXIC"}
	if cc == "IN" {
		markets = []string{"GC=F", "SI=F", "^NSEI", "^BSESN"}
	}
	units := "metric"
	if cc == "US" {
		units = "imperial"
	}
	return WidgetSettings{Units: units, Home: home, Currencies: cur, Markets: markets, NewsScope: "local", Order: cleanKeys(nil, true)}
}

var (
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	symbolRe   = regexp.MustCompile(`^[A-Za-z0-9^=.\-]{1,20}$`)
)

// cleanUI validates settings from the browser before saving them.
func cleanUI(u *UISettings) error {
	for _, b := range []*Background{&u.BackgroundWeb, &u.BackgroundTV} {
		if len(b.Shader) > 40 || len(b.Preset) > 40 {
			return errors.New("background name is too long")
		}
		b.Dim = max(0, min(90, b.Dim))
		b.Speed = max(0, min(2, b.Speed))
	}
	w := &u.Widgets
	w.normalize()
	if len(w.Places) > maxPlaces {
		return fmt.Errorf("choose up to %d cities", maxPlaces)
	}
	if w.Units != "imperial" {
		w.Units = "metric"
	}
	if w.NewsScope != "top" && w.NewsScope != "off" {
		w.NewsScope = "local"
	}
	if len(w.Sources) > 8 {
		return errors.New("choose up to 8 news sources")
	}
	if w.Sources == nil {
		w.Sources = []NewsSource{}
	}
	for i := range w.Sources {
		src := &w.Sources[i]
		src.Name = strings.TrimSpace(src.Name)
		if src.Name == "" || len(src.Name) > 40 {
			return errors.New("each news source needs a name of up to 40 characters")
		}
		if err := validFeedURL(src.URL); err != nil {
			return fmt.Errorf("%s: %w", src.Name, err)
		}
	}
	if err := cleanExtraWidgets(w); err != nil {
		return err
	}
	w.Home = strings.ToUpper(w.Home)
	if !currencyRe.MatchString(w.Home) {
		return errors.New("home currency must be a 3-letter code like INR")
	}
	if len(w.Currencies) > 8 || len(w.Markets) > 10 {
		return errors.New("too many currencies or markets")
	}
	for i, c := range w.Currencies {
		w.Currencies[i] = strings.ToUpper(c)
		if !currencyRe.MatchString(w.Currencies[i]) {
			return fmt.Errorf("%q is not a currency code", c)
		}
	}
	for i, s := range w.Markets {
		w.Markets[i] = strings.ToUpper(strings.TrimSpace(s))
		if !symbolRe.MatchString(w.Markets[i]) {
			return fmt.Errorf("%q is not a market symbol", s)
		}
	}
	for _, p := range w.Places {
		if p.Name == "" || len(p.Name) > 100 || p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
			return errors.New("invalid place")
		}
	}
	return nil
}

// --- fetching with a cache ------------------------------------------------

type cached struct {
	data    any
	at      time.Time
	err     string
	retryAt time.Time
}

type Widgets struct {
	client *http.Client
	public *http.Client // refuses private addresses; used for user-chosen feeds

	iconMu sync.Mutex
	icons  map[string]*iconEntry
	mu     sync.Mutex
	cache  map[string]*cached
	// Fetches in flight, so many open screens don't hit the sources at once.
	inflight map[string]*sync.WaitGroup
}

func NewWidgets() *Widgets {
	return &Widgets{client: &http.Client{Timeout: 10 * time.Second}, public: publicClient(), cache: map[string]*cached{}, inflight: map[string]*sync.WaitGroup{}}
}

const userAgent = "Mozilla/5.0 (compatible; Haven homelab)"

func (w *Widgets) getJSON(ctx context.Context, u string, out any) error {
	b, err := w.get(ctx, u)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

var errRateLimited = errors.New("the source asked us to slow down; showing the last data")

func (w *Widgets) get(ctx context.Context, u string) ([]byte, error) {
	return w.getWith(ctx, w.client, u)
}

func (w *Widgets) getWith(ctx context.Context, client *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, */*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("can't reach %s: %s", req.URL.Host, reachReason(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", req.URL.Host, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// cachedFetch returns fresh-enough data from the cache, or fetches it. When
// a fetch fails, the last good data is returned along with the error.
func (w *Widgets) cachedFetch(ctx context.Context, key string, ttl time.Duration, fetch func(context.Context) (any, error)) (any, string) {
	w.mu.Lock()
	c := w.cache[key]
	if c != nil && (time.Since(c.at) < ttl || time.Now().Before(c.retryAt)) {
		w.mu.Unlock()
		return c.data, c.err
	}
	if wg := w.inflight[key]; wg != nil {
		w.mu.Unlock()
		wg.Wait()
		w.mu.Lock()
		c = w.cache[key]
		w.mu.Unlock()
		if c == nil {
			return nil, "not available"
		}
		return c.data, c.err
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	w.inflight[key] = wg
	w.mu.Unlock()

	data, err := fetch(ctx)

	w.mu.Lock()
	defer func() {
		delete(w.inflight, key)
		w.mu.Unlock()
		wg.Done()
	}()
	if err != nil {
		if c == nil {
			c = &cached{}
			w.cache[key] = c
		}
		c.err = err.Error()
		c.retryAt = time.Now().Add(2 * time.Minute)
		if errors.Is(err, errRateLimited) {
			c.retryAt = time.Now().Add(15 * time.Minute)
		}
		return c.data, c.err
	}
	w.cache[key] = &cached{data: data, at: time.Now()}
	return data, ""
}

// --- weather --------------------------------------------------------------

type DayForecast struct {
	Date string  `json:"date"`
	Code int     `json:"code"`
	Max  float64 `json:"max"`
	Min  float64 `json:"min"`
	Rain int     `json:"rain"` // chance of rain, %
}

type Weather struct {
	Place     string        `json:"place"`
	Temp      float64       `json:"temp"`
	Feels     float64       `json:"feels"`
	Humidity  int           `json:"humidity"`
	Wind      float64       `json:"wind"`
	Code      int           `json:"code"`
	IsDay     bool          `json:"isDay"`
	Units     string        `json:"units"` // C or F
	WindUnits string        `json:"windUnits"`
	Days      []DayForecast `json:"days"`
}

func (w *Widgets) weather(ctx context.Context, p Place, units string) (any, error) {
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%.4f", p.Lat))
	q.Set("longitude", fmt.Sprintf("%.4f", p.Lon))
	q.Set("current", "temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,wind_speed_10m,is_day")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max")
	q.Set("timezone", "auto")
	q.Set("forecast_days", "7")
	if units == "imperial" {
		q.Set("temperature_unit", "fahrenheit")
		q.Set("wind_speed_unit", "mph")
	}
	var r struct {
		Current struct {
			Temp     float64 `json:"temperature_2m"`
			Feels    float64 `json:"apparent_temperature"`
			Humidity int     `json:"relative_humidity_2m"`
			Code     int     `json:"weather_code"`
			Wind     float64 `json:"wind_speed_10m"`
			IsDay    int     `json:"is_day"`
		} `json:"current"`
		Daily struct {
			Time []string  `json:"time"`
			Code []int     `json:"weather_code"`
			Max  []float64 `json:"temperature_2m_max"`
			Min  []float64 `json:"temperature_2m_min"`
			Rain []int     `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := w.getJSON(ctx, "https://api.open-meteo.com/v1/forecast?"+q.Encode(), &r); err != nil {
		return nil, err
	}
	out := Weather{Place: p.Name, Temp: r.Current.Temp, Feels: r.Current.Feels, Humidity: r.Current.Humidity,
		Wind: r.Current.Wind, Code: r.Current.Code, IsDay: r.Current.IsDay == 1, Units: "C", WindUnits: "km/h"}
	if units == "imperial" {
		out.Units, out.WindUnits = "F", "mph"
	}
	for i, d := range r.Daily.Time {
		day := DayForecast{Date: d}
		if i < len(r.Daily.Code) {
			day.Code = r.Daily.Code[i]
		}
		if i < len(r.Daily.Max) {
			day.Max = r.Daily.Max[i]
		}
		if i < len(r.Daily.Min) {
			day.Min = r.Daily.Min[i]
		}
		if i < len(r.Daily.Rain) {
			day.Rain = r.Daily.Rain[i]
		}
		out.Days = append(out.Days, day)
	}
	return out, nil
}

func (w *Widgets) Places(ctx context.Context, q string) ([]Place, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 || len(q) > 80 {
		return []Place{}, nil
	}
	var r struct {
		Results []struct {
			Name    string  `json:"name"`
			Admin1  string  `json:"admin1"`
			Country string  `json:"country"`
			CC      string  `json:"country_code"`
			Lat     float64 `json:"latitude"`
			Lon     float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := w.getJSON(ctx, "https://geocoding-api.open-meteo.com/v1/search?count=6&language=en&name="+url.QueryEscape(q), &r); err != nil {
		return nil, err
	}
	out := []Place{}
	for _, x := range r.Results {
		out = append(out, Place{Name: x.Name, Region: x.Admin1, Country: x.Country, CC: x.CC, Lat: x.Lat, Lon: x.Lon})
	}
	return out, nil
}

// --- news -----------------------------------------------------------------

type NewsItem struct {
	Summary   string    `json:"summary,omitempty"`
	Title     string    `json:"title"`
	Link      string    `json:"link"`
	Source    string    `json:"source"`
	Published time.Time `json:"published"`
}

type News struct {
	Scope string     `json:"scope"` // local or top
	Label string     `json:"label"` // e.g. "Bengaluru" or "Top stories"
	Items []NewsItem `json:"items"`
}

type rss struct {
	Items []struct {
		Title   string `xml:"title"`
		Link    string `xml:"link"`
		PubDate string `xml:"pubDate"`
		Source  string `xml:"source"`
	} `xml:"channel>item"`
}

func parseNews(b []byte, limit int) ([]NewsItem, error) {
	var r rss
	if err := xml.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	items := []NewsItem{}
	for _, it := range r.Items {
		title := strings.TrimSpace(it.Title)
		// Google appends " - Source" to titles; the source is given separately.
		if it.Source != "" {
			title = strings.TrimSuffix(title, " - "+it.Source)
		}
		t, _ := time.Parse(time.RFC1123, it.PubDate)
		if !strings.HasPrefix(it.Link, "https://") {
			continue
		}
		items = append(items, NewsItem{Title: title, Link: it.Link, Source: it.Source, Published: t})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Published.After(items[j].Published) })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (w *Widgets) news(ctx context.Context, scope string, p *Place) (any, error) {
	cc := defaultCountry()
	if p != nil && p.CC != "" {
		cc = strings.ToUpper(p.CC)
	}
	params := fmt.Sprintf("hl=en-%s&gl=%s&ceid=%s:en", cc, cc, cc)
	u := "https://news.google.com/rss?" + params
	out := News{Scope: "top", Label: "Top stories"}
	if scope == "local" && p != nil {
		u = "https://news.google.com/rss/search?q=" + url.QueryEscape(p.Name) + "&" + params
		out = News{Scope: "local", Label: p.Name}
	}
	b, err := w.get(ctx, u)
	if err != nil {
		return nil, err
	}
	items, err := parseNews(b, 40)
	if err != nil {
		return nil, errors.New("the news feed couldn't be read")
	}
	out.Items = items
	return out, nil
}

// --- currency -------------------------------------------------------------

type Rate struct {
	Code    string    `json:"code"`
	Rate    float64   `json:"rate"`   // 1 Code = Rate Home
	Change  float64   `json:"change"` // % change over the period
	History []float64 `json:"history"`
}

type Currency struct {
	Home  string `json:"home"`
	Date  string `json:"date"`
	Rates []Rate `json:"rates"`
}

// currency asks for 1 Home in each currency over the last ~2 weeks, then
// flips the numbers so they read "1 USD = 94.94 INR".
func (w *Widgets) currency(ctx context.Context, home string, codes []string) (any, error) {
	if len(codes) == 0 {
		return Currency{Home: home, Rates: []Rate{}}, nil
	}
	start := time.Now().AddDate(0, 0, -14).Format("2006-01-02")
	var r struct {
		Rates map[string]map[string]float64 `json:"rates"`
	}
	u := fmt.Sprintf("https://api.frankfurter.dev/v1/%s..?base=%s&symbols=%s", start, home, strings.Join(codes, ","))
	if err := w.getJSON(ctx, u, &r); err != nil {
		return nil, err
	}
	dates := make([]string, 0, len(r.Rates))
	for d := range r.Rates {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	out := Currency{Home: home, Rates: []Rate{}}
	if len(dates) > 0 {
		out.Date = dates[len(dates)-1]
	}
	for _, c := range codes {
		rate := Rate{Code: c}
		for _, d := range dates {
			if v, ok := r.Rates[d][c]; ok && v > 0 {
				rate.History = append(rate.History, 1/v)
			}
		}
		if n := len(rate.History); n > 0 {
			rate.Rate = rate.History[n-1]
			if first := rate.History[0]; first > 0 {
				rate.Change = (rate.Rate - first) / first * 100
			}
		}
		out.Rates = append(out.Rates, rate)
	}
	return out, nil
}

// --- markets --------------------------------------------------------------

type Quote struct {
	Symbol    string    `json:"symbol"`
	Name      string    `json:"name"`
	Currency  string    `json:"currency"`
	Price     float64   `json:"price"`
	Change    float64   `json:"change"`
	ChangePct float64   `json:"changePct"`
	History   []float64 `json:"history"`
	Local     *Local    `json:"local,omitempty"` // gold/silver in your currency and units
	Error     string    `json:"error,omitempty"`
}

type Local struct {
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
	Unit     string  `json:"unit"` // "10 g" or "1 kg"
}

var friendlyNames = map[string]string{
	"GC=F": "Gold", "SI=F": "Silver", "PL=F": "Platinum", "CL=F": "Crude oil", "^NSEI": "Nifty 50", "^BSESN": "Sensex",
	"^GSPC": "S&P 500", "^IXIC": "Nasdaq", "^DJI": "Dow Jones", "^FTSE": "FTSE 100", "^N225": "Nikkei 225", "BTC-USD": "Bitcoin", "ETH-USD": "Ethereum",
}

func parseChart(b []byte) (Quote, error) {
	var r struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Symbol    string  `json:"symbol"`
					Currency  string  `json:"currency"`
					Price     float64 `json:"regularMarketPrice"`
					ShortName string  `json:"shortName"`
					LongName  string  `json:"longName"`
				} `json:"meta"`
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
			Error *struct {
				Description string `json:"description"`
			} `json:"error"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return Quote{}, err
	}
	if r.Chart.Error != nil {
		return Quote{}, errors.New(r.Chart.Error.Description)
	}
	if len(r.Chart.Result) == 0 {
		return Quote{}, errors.New("no data for this symbol")
	}
	res := r.Chart.Result[0]
	q := Quote{Symbol: res.Meta.Symbol, Currency: res.Meta.Currency, Price: res.Meta.Price}
	q.Name = friendlyNames[q.Symbol]
	if q.Name == "" {
		q.Name = res.Meta.ShortName
	}
	if q.Name == "" {
		q.Name = q.Symbol
	}
	if len(res.Indicators.Quote) > 0 {
		for _, c := range res.Indicators.Quote[0].Close {
			if c != nil && *c > 0 {
				q.History = append(q.History, *c)
			}
		}
	}
	// The last close is today's price so far; compare with the day before.
	if n := len(q.History); n >= 2 && q.Price > 0 {
		prev := q.History[n-2]
		q.Change = q.Price - prev
		q.ChangePct = q.Change / prev * 100
	}
	return q, nil
}

const gramsPerTroyOunce = 31.1034768

func (w *Widgets) markets(ctx context.Context, symbols []string, home string) (any, error) {
	quotes := make([]Quote, len(symbols))
	var wg sync.WaitGroup
	for i, s := range symbols {
		wg.Add(1)
		go func(i int, s string) {
			defer wg.Done()
			// Each symbol is cached on its own, so one bad symbol can't
			// hide the rest, and adding one doesn't refetch everything.
			data, errText := w.cachedFetch(ctx, "quote:"+s, 15*time.Minute, func(ctx context.Context) (any, error) {
				b, err := w.get(ctx, "https://query1.finance.yahoo.com/v8/finance/chart/"+url.PathEscape(s)+"?range=1mo&interval=1d")
				if err != nil {
					return nil, err
				}
				return parseChart(b)
			})
			q, ok := data.(Quote)
			if !ok {
				q = Quote{Symbol: s, Name: friendlyNames[s]}
				if q.Name == "" {
					q.Name = s
				}
			}
			if errText != "" && !ok {
				q.Error = errText
			}
			quotes[i] = q
		}(i, s)
	}
	wg.Wait()

	// Gold and silver are quoted in US dollars per troy ounce. Also show them
	// the way people buy them locally: per 10 g of gold, per kg of silver.
	if home != "USD" {
		var usdHome float64
		if data, _ := w.cachedFetch(ctx, "usd:"+home, time.Hour, func(ctx context.Context) (any, error) {
			var r struct {
				Rates map[string]float64 `json:"rates"`
			}
			if err := w.getJSON(ctx, "https://api.frankfurter.dev/v1/latest?base=USD&symbols="+home, &r); err != nil {
				return nil, err
			}
			return r.Rates[home], nil
		}); data != nil {
			usdHome, _ = data.(float64)
		}
		for i := range quotes {
			q := &quotes[i]
			if usdHome <= 0 || q.Price <= 0 || q.Currency != "USD" {
				continue
			}
			switch q.Symbol {
			case "GC=F":
				q.Local = &Local{Price: q.Price * usdHome / gramsPerTroyOunce * 10, Currency: home, Unit: "10 g"}
			case "SI=F":
				q.Local = &Local{Price: q.Price * usdHome / gramsPerTroyOunce * 1000, Currency: home, Unit: "1 kg"}
			}
		}
	}
	return quotes, nil
}

// --- all widgets at once ----------------------------------------------------

type WidgetResult[T any] struct {
	Data  *T     `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
	Off   bool   `json:"off,omitempty"`
}

type CheckSummary = []CheckResult

type WidgetData struct {
	Status   WidgetResult[CheckSummary] `json:"status"`
	Weather  WidgetResult[[]Weather]    `json:"weather"` // one per city
	News     WidgetResult[[]News]       `json:"news"`    // one per city (or one for top stories)
	Currency WidgetResult[Currency]     `json:"currency"`
	Markets  WidgetResult[[]Quote]      `json:"markets"`
	Settings WidgetSettings             `json:"settings"`
}

func hidden(s WidgetSettings, name string) bool {
	for _, h := range s.Hidden {
		if h == name {
			return true
		}
	}
	return false
}

func fill[T any](r *WidgetResult[T], data any, err string) {
	if v, ok := data.(T); ok {
		r.Data = &v
	}
	r.Error = err
}

func (w *Widgets) All(ctx context.Context, s WidgetSettings) WidgetData {
	out := WidgetData{Settings: s}
	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}
	if hidden(s, "weather") {
		out.Weather.Off = true
	} else if len(s.Places) > 0 {
		places := s.Places
		run(func() {
			list := make([]*Weather, len(places))
			errs := make([]string, len(places))
			var cw sync.WaitGroup
			for i, p := range places {
				cw.Add(1)
				go func() {
					defer cw.Done()
					key := fmt.Sprintf("weather:%.3f,%.3f:%s", p.Lat, p.Lon, s.Units)
					d, e := w.cachedFetch(ctx, key, 15*time.Minute, func(ctx context.Context) (any, error) { return w.weather(ctx, p, s.Units) })
					if v, ok := d.(Weather); ok {
						list[i] = &v
					}
					errs[i] = e
				}()
			}
			cw.Wait()
			out.Weather = collect(list, errs)
		})
	}
	if hidden(s, "news") {
		out.News.Off = true
	} else {
		run(func() {
			places := s.Places
			if s.NewsScope == "top" || len(places) == 0 {
				places = []Place{{}} // one feed, not tied to a city
			}
			if s.NewsScope == "off" {
				places = nil
			}
			total := len(places) + len(s.Sources)
			list := make([]*News, total)
			errs := make([]string, total)
			var nwg sync.WaitGroup
			for i, p := range places {
				nwg.Add(1)
				go func() {
					defer nwg.Done()
					var pp *Place
					if p.Name != "" {
						pp = &p
					}
					scope := s.NewsScope
					if pp == nil {
						scope = "top"
					}
					key := "news:" + scope
					if pp != nil {
						key += ":" + pp.Name + ":" + pp.CC
					}
					d, e := w.cachedFetch(ctx, key, 30*time.Minute, func(ctx context.Context) (any, error) { return w.news(ctx, scope, pp) })
					if v, ok := d.(News); ok {
						list[i] = &v
					}
					errs[i] = e
				}()
			}
			for j, src := range s.Sources {
				i := len(places) + j
				nwg.Add(1)
				go func() {
					defer nwg.Done()
					d, e := w.cachedFetch(ctx, "feed:"+src.URL, 30*time.Minute, func(ctx context.Context) (any, error) { return w.feed(ctx, src) })
					if v, ok := d.(News); ok {
						list[i] = &v
					}
					if e != "" {
						errs[i] = src.Name + ": " + e
					}
				}()
			}
			nwg.Wait()
			out.News = collect(list, errs)
		})
	}
	if hidden(s, "currency") {
		out.Currency.Off = true
	} else {
		run(func() {
			key := "fx:" + s.Home + ":" + strings.Join(s.Currencies, ",")
			d, e := w.cachedFetch(ctx, key, time.Hour, func(ctx context.Context) (any, error) { return w.currency(ctx, s.Home, s.Currencies) })
			fill(&out.Currency, d, e)
		})
	}
	if hidden(s, "markets") {
		out.Markets.Off = true
	} else {
		run(func() {
			d, e := w.markets(ctx, s.Markets, s.Home)
			if e != nil {
				out.Markets.Error = e.Error()
			}
			fill(&out.Markets, d, "")
		})
	}
	wg.Wait()
	return out
}

// collect gathers per-city results, skipping cities that failed. The first
// error is kept so the widget can say something is wrong.
func collect[T any](list []*T, errs []string) WidgetResult[[]T] {
	var r WidgetResult[[]T]
	data := []T{}
	for i, v := range list {
		if v != nil {
			data = append(data, *v)
		}
		if errs[i] != "" && r.Error == "" {
			r.Error = errs[i]
		}
	}
	if len(data) > 0 {
		r.Data = &data
	}
	return r
}

// cleanExtraWidgets validates the clock, notes, links and status settings.
func cleanExtraWidgets(w *WidgetSettings) error {
	if len(w.Clocks) > 3 {
		return errors.New("choose up to 3 extra clocks")
	}
	for _, z := range w.Clocks {
		if _, err := time.LoadLocation(z); err != nil || z == "" || strings.ContainsAny(z, " \\") {
			return fmt.Errorf("%q isn't a time zone Haven knows", z)
		}
	}
	if len(w.Notes) > 4000 || strings.ContainsRune(w.Notes, 0) {
		return errors.New("the note is too long (up to 4000 characters)")
	}
	if len(w.Links) > 24 {
		return errors.New("choose up to 24 links")
	}
	for i := range w.Links {
		l := &w.Links[i]
		l.Name = strings.TrimSpace(l.Name)
		l.URL = strings.TrimSpace(l.URL)
		u, err := url.Parse(l.URL)
		if l.Name == "" || len(l.Name) > 30 {
			return errors.New("each link needs a name of up to 30 characters")
		}
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(l.URL) > 300 {
			return fmt.Errorf("%s: the address must start with http:// or https://", l.Name)
		}
	}
	if len(w.Checks) > maxChecks {
		return fmt.Errorf("choose up to %d status checks", maxChecks)
	}
	seen := map[string]bool{}
	for i := range w.Checks {
		c := &w.Checks[i]
		c.Name = strings.TrimSpace(c.Name)
		c.Target = strings.TrimSpace(c.Target)
		if c.Name == "" || len(c.Name) > 30 {
			return errors.New("each status check needs a name of up to 30 characters")
		}
		if err := validCheckTarget(c.Target); err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		if seen[c.Target] {
			return fmt.Errorf("%s is listed twice", c.Target)
		}
		seen[c.Target] = true
	}
	return nil
}
