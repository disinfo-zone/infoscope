package rss

import "encoding/xml"

// RSS is the root element of an RSS feed.
type RSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	AtomNS  string   `xml:"xmlns:atom,attr,omitempty"` // Atom namespace
	Channel Channel  `xml:"channel"`
}

// AtomLink defines the structure for an atom:link element.
type AtomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr,omitempty"`
}

// Channel represents the channel element in an RSS feed.
type Channel struct {
	XMLName       xml.Name `xml:"channel"`
	Title         string   `xml:"title"`
	Link          string   `xml:"link"`
	Description   string   `xml:"description"`
	Language      string   `xml:"language,omitempty"`
	LastBuildDate string   `xml:"lastBuildDate,omitempty"` // Should be in RFC1123Z format
	SelfLink      AtomLink `xml:"atom:link,omitempty"`     // Atom link for self-reference
	Items         []Item   `xml:"item"`
}

// Item represents an item element in an RSS feed.
type Item struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description,omitempty"` // Optional, can be a summary or full content
	PubDate     string   `xml:"pubDate,omitempty"`     // Should be in RFC1123Z format
	GUID        string   `xml:"guid,omitempty"`        // A unique identifier for the item, can be the link
}
