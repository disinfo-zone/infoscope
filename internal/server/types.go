// internal/server/types.go
package server

import "time"

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Setting types
type SettingKey string

const (
	SettingSiteTitle      SettingKey = "site_title"
	SettingMaxPosts       SettingKey = "max_posts"
	SettingUpdateInterval SettingKey = "update_interval"
)

type EntryView struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	FaviconURL string `json:"faviconUrl"`
	Date       string `json:"date"`
}

type IndexData struct {
	BaseTemplateData
	Title             string
	Entries           []EntryView
	HeaderLinkURL     string
	HeaderLinkText    string
	FooterLinkURL     string
	FooterLinkText    string
	FooterImageURL    string
	FooterImageHeight string
	TrackingCode      string
}

type BaseTemplateData struct {
	CSRFToken string
}

type AdminPageData struct {
	BaseTemplateData
	Title      string
	Active     string
	FeedCount  int
	EntryCount int
	LastUpdate string
	UserID     int64
	ClickStats *DashboardStats
}

type SettingsTemplateData struct {
	BaseTemplateData
	Title     string
	Active    string
	Settings  map[string]string
	CSRFToken string
}

type Settings struct {
	SiteTitle         string `json:"siteTitle"`
	MaxPosts          int    `json:"maxPosts"`
	UpdateInterval    int    `json:"updateInterval"`
	HeaderLinkText    string `json:"headerLinkText"`
	HeaderLinkURL     string `json:"headerLinkURL"`
	FooterLinkText    string `json:"footerLinkText"`
	FooterLinkURL     string `json:"footerLinkURL"`
	FooterImageURL    string `json:"footerImageURL"`
	FooterImageHeight string `json:"footerImageHeight"`
	TrackingCode      string `json:"trackingCode"`
}

type Feed struct {
	ID          int64     `json:"id"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	LastFetched time.Time `json:"lastFetched,omitempty"`
}

type LoginTemplateData struct {
	BaseTemplateData
	Error string
}
