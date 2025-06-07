//
// Copyright (c) 2018 Ted Unangst <tedu@tedunangst.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

// types for making an rss feed
package rss

import (
	"encoding/xml"
	"html"
	"io"
	"strings"
)

type header struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Feed    *Feed    `xml:"channel"`
}

type Feed struct {
	XMLName        xml.Name `xml:"channel"`
	Title          string   `xml:"title"`
	Link           string   `xml:"link"`
	Description    string   `xml:"description"`
	ManagingEditor string   `xml:"managingEditor,omitempty"`
	PubDate        string   `xml:"pubDate,omitempty"`
	LastBuildDate  string   `xml:"lastBuildDate,omitempty"`
	TTL            int      `xml:"ttl,omitempty"`
	Image          *Image   `xml:"image"`
	Items          []*Item  `xml:"item"`
}

type Image struct {
	XMLName xml.Name `xml:"image"`
	URL     string   `xml:"url"`
	Title   string   `xml:"title"`
	Link    string   `xml:"link"`
}

type Item struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Description CData    `xml:"description"`
	Encoded     *CData   `xml:"encoded"`
	Author      string   `xml:"author,omitempty"`
	Category    []string `xml:"category"`
	Link        string   `xml:"link"`
	PubDate     string   `xml:"pubDate"`
	Guid        *Guid    `xml:"guid"`
	Source      *Source  `xml:"source"`
	OrigLink    *string  `xml:"origLink"`
}

type Guid struct {
	XMLName     xml.Name `xml:"guid"`
	IsPermaLink bool     `xml:"isPermaLink,attr"`
	Value       string   `xml:",chardata"`
}

type Source struct {
	XMLName xml.Name `xml:"source"`
	URL     string   `xml:"url,attr"`
	Title   string   `xml:",chardata"`
}

type CData struct {
	Data string `xml:",cdata"`
}

// Write the Feed as XML.
func (fd *Feed) Write(w io.Writer) error {
	hdr := header{Version: "2.0", Feed: fd}
	io.WriteString(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	err := enc.Encode(hdr)
	io.WriteString(w, "\n")
	return err
}
func (fd *Feed) Encode() ([]byte, error) {
	hdr := header{Version: "2.0", Feed: fd}
	data, err := xml.MarshalIndent(hdr, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}

type multiheader struct {
	Feed    *Feed    `xml:"channel"`
	Title   string   `xml:"title"`
	Entries []*Entry `xml:"entry"`
}

type Entry struct {
	XMLName   xml.Name    `xml:"entry"`
	Title     string      `xml:"title"`
	Link      []AtomLink  `xml:"link"`
	Published string      `xml:"published"`
	Updated   string      `xml:"updated"`
	ID        string      `xml:"id"`
	Summary   string      `xml:"summary"`
	Content   AtomContent `xml:"content"`
	Category  []string    `xml:"category"`
}

type AtomContent struct {
	Content string `xml:",innerxml"`
	Type    string `xml:"type,attr"`
}

type AtomLink struct {
	XMLName xml.Name `xml:"link"`
	Href    string   `xml:"href,attr"`
	Rel     string   `xml:"rel,attr"`
}

func Parse(r io.Reader) (*Feed, error) {
	var hdr multiheader
	dec := xml.NewDecoder(r)
	err := dec.Decode(&hdr)
	if err != nil {
		return nil, err
	}
	return tofeed(&hdr), nil
}
func ParseBytes(data []byte) (*Feed, error) {
	var hdr multiheader
	err := xml.Unmarshal(data, &hdr)
	if err != nil {
		return nil, err
	}
	return tofeed(&hdr), nil
}
func tofeed(hdr *multiheader) *Feed {
	if hdr.Feed != nil {
		return hdr.Feed
	}
	feed := new(Feed)
	feed.Items = make([]*Item, len(hdr.Entries))
	for i, entry := range hdr.Entries {
		item := new(Item)
		item.Title = entry.Title
		if len(entry.Content.Content) > len(entry.Summary) {
			if entry.Content.Type == "xhtml" {
				item.Description.Data = entry.Content.Content
			} else {
				s := entry.Content.Content
				if strings.HasPrefix(s, "<![CDATA[") {
					s = strings.TrimPrefix(s, "<![CDATA[")
					s = strings.TrimSuffix(s, "]]>")
				} else {
					s = html.UnescapeString(s)
				}
				item.Description.Data = s
			}
		} else {
			item.Description.Data = entry.Summary
		}
		item.Category = entry.Category
		for _, link := range entry.Link {
			if link.Rel == "alternate" {
				item.Link = link.Href
			}
		}
		if item.Link == "" && len(entry.Link) > 0 {
			item.Link = entry.Link[0].Href
		}
		item.PubDate = entry.Published
		item.Guid = new(Guid)
		item.Guid.Value = entry.ID
		feed.Items[i] = item
	}
	return feed
}
