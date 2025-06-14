//
// Copyright (c) 2019 Ted Unangst <tedu@tedunangst.com>
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

package main

import (
	"crypto/rand"
	"crypto/sha512"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"humungus.tedunangst.com/r/webs/gencache"
	"humungus.tedunangst.com/r/webs/htfilter"
	"humungus.tedunangst.com/r/webs/httpsig"
	"humungus.tedunangst.com/r/webs/mz"
	"humungus.tedunangst.com/r/webs/synlight"
	"humungus.tedunangst.com/r/webs/templates"
)

var allowedclasses = make(map[string]bool)

func init() {
	allowedclasses["kw"] = true
	allowedclasses["bi"] = true
	allowedclasses["st"] = true
	allowedclasses["nm"] = true
	allowedclasses["tp"] = true
	allowedclasses["op"] = true
	allowedclasses["cm"] = true
	allowedclasses["al"] = true
	allowedclasses["dl"] = true
}

var relingo = make(map[string]string)

func loadLingo() {
	for _, l := range []string{"honked", "bonked", "honked back", "qonked", "evented"} {
		v := l
		k := "lingo-" + strings.ReplaceAll(l, " ", "")
		getconfig(k, &v)
		relingo[l] = v
	}
}

func reverbolate(userid UserID, honks []*Honk) {
	var handlers sync.WaitGroup
	user, _ := somenumberedusers.Get(userid)
	for i := range honks {
		h := honks[i]
		h.What += "ed"
		if h.What == "honked" && h.RID != "" {
			h.What = "honked back"
			h.Style += " subtle"
		}
		if !h.Public {
			h.Style += " limited"
		}
		if h.Whofore == WhoAtme {
			h.Style += " atme"
		}
		translate(h)
		local := false
		if h.Whofore == WhoPublic || h.Whofore == WhoPrivate {
			local = true
		}
		if local && h.What != "bonked" {
			h.Noise = re_memes.ReplaceAllString(h.Noise, "")
		}
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			h.Username, h.Handle = handles(h.Honker)
			if !local {
				short := shortname(userid, h.Honker)
				if short != "" {
					h.Username = short
				} else {
					h.Username = h.Handle
					if len(h.Username) > 20 {
						h.Username = h.Username[:20] + ".."
					}
				}
			}
			if user != nil {
				hset := []string{}
				if h.Honker != user.URL {
					hset = append(hset, "@"+h.Handle)
				}
				if user.Options.MentionAll {
					for _, a := range h.Audience {
						if a == h.Honker || a == user.URL {
							continue
						}
						_, hand := handles(a)
						if hand != "" {
							hand = "@" + hand
							hset = append(hset, hand)
						}
					}
				}
				h.Handles = strings.Join(hset, " ")
			}
			if h.URL == "" {
				h.URL = h.XID
			}
			if h.Oonker != "" {
				_, h.Oondle = handles(h.Oonker)
			}
		}()
		h.Precis = demoji(h.Precis)
		h.Noise = demoji(h.Noise)
		h.Open = "open"
		var misto string
		for _, m := range h.Mentions {
			if m.Where != h.Honker && m.Where != h.Oonker && !m.IsPresent(h.Noise) {
				misto += string(templates.Sprintf(" <a href=\"%sh?xid=%s\">%s</a>", serverPrefix, url.QueryEscape(m.Where), m.Who))
			}
		}
		var mistag string
		for _, o := range h.Onts {
			if !OntIsPresent(o, h.Noise) {
				mistag += string(templates.Sprintf(" <a href=\"%so/%s\">%s</a>", serverPrefix, o[1:], o))
			}
		}
		if len(misto) > 0 || len(mistag) > 0 {
			if len(misto) > 0 {
				misto = fmt.Sprintf("(%s)<p>", misto[1:])
			}
			if len(mistag) > 0 {
				mistag = fmt.Sprintf("<p>(%s)", mistag[1:])
			}
			h.Noise = misto + h.Noise + mistag
		}

		zap := make(map[string]bool)
		{
			var htf htfilter.Filter
			htf.Imager = replaceimgsand(zap, false, h)
			htf.SpanClasses = allowedclasses
			htf.BaseURL, _ = url.Parse(h.XID)
			emuxifier := func(e string) string {
				for _, d := range h.Donks {
					if d.Name == e {
						zap[d.XID] = true
						if d.Local {
							return fmt.Sprintf(`<img class="emu" title="%s" src="/d/%s">`, d.Name, d.XID)
						}
					}
				}
				if local && h.What != "bonked" {
					emu, _ := emucache.Get(e)
					if emu != nil {
						return fmt.Sprintf(`<img class="emu" title="%s" src="%s">`, emu.Name, emu.ID)
					}
				}
				return e
			}
			htf.FilterText = func(w io.Writer, data string) {
				data = htfilter.EscapeText(data)
				data = re_emus.ReplaceAllStringFunc(data, emuxifier)
				io.WriteString(w, data)
			}
			if user != nil {
				htf.RetargetLink = func(href string) string {
					h2 := strings.ReplaceAll(href, "/@", "/users/")
					for _, m := range h.Mentions {
						if h2 == m.Where || href == m.Where {
							return "/h?xid=" + url.QueryEscape(m.Where)
						}
					}
					return href
				}
			}
			p, _ := htf.String(h.Precis)
			n, _ := htf.String(h.Noise)
			h.Precis = string(p)
			h.Noise = string(n)
		}
		j := 0
		for i := 0; i < len(h.Donks); i++ {
			if !zap[h.Donks[i].XID] {
				h.Donks[j] = h.Donks[i]
				j++
			}
		}
		h.Donks = h.Donks[:j]
	}
	handlers.Wait()

	unsee(honks, userid)

	for _, h := range honks {
		renderflags(h)

		h.HTPrecis = template.HTML(h.Precis)
		h.HTML = template.HTML(h.Noise)
		if redo := relingo[h.What]; redo != "" {
			h.What = redo
		}
	}
}

func replaceimgsand(zap map[string]bool, absolute bool, honk *Honk) func(node *html.Node) string {
	return func(node *html.Node) string {
		src := htfilter.GetAttr(node, "src")
		alt := htfilter.GetAttr(node, "alt")
		//title := GetAttr(node, "title")
		if htfilter.HasClass(node, "Emoji") && alt != "" {
			return alt
		}
		base := path.Base(src)
		didx, _ := strconv.Atoi(base)
		var d *Donk
		if strings.HasPrefix(src, serverPrefix) && didx > 0 && didx <= len(honk.Donks) {
			d = honk.Donks[didx-1]
		} else {
			d = finddonk(src)
		}
		if d != nil {
			zap[d.XID] = true
			base := ""
			if absolute {
				base = serverURL("")
			}
			return string(templates.Sprintf(`<img alt="%s" title="%s" src="%s/d/%s">`, alt, alt, base, d.XID))
		}
		return string(templates.Sprintf(`&lt;img alt="%s" src="<a href="%s">%s</a>"&gt;`, alt, src, src))
	}
}

func translatechonk(ch *Chonk) {
	noise := ch.Noise
	if ch.Format == "markdown" {
		var marker mz.Marker
		noise = marker.Mark(noise)
	}
	var htf htfilter.Filter
	htf.SpanClasses = allowedclasses
	htf.BaseURL, _ = url.Parse(ch.XID)
	ch.HTML, _ = htf.String(noise)
}

func filterchonk(ch *Chonk) {
	translatechonk(ch)

	noise := string(ch.HTML)

	local := originate(ch.XID) == serverName

	zap := make(map[string]bool)
	emuxifier := func(e string) string {
		for _, d := range ch.Donks {
			if d.Name == e {
				zap[d.XID] = true
				if d.Local {
					return fmt.Sprintf(`<img class="emu" title="%s" src="/d/%s">`, d.Name, d.XID)
				}
			}
		}
		if local {
			emu, _ := emucache.Get(e)
			if emu != nil {
				return fmt.Sprintf(`<img class="emu" title="%s" src="%s">`, emu.Name, emu.ID)
			}
		}
		return e
	}
	noise = re_emus.ReplaceAllStringFunc(noise, emuxifier)
	j := 0
	for i := 0; i < len(ch.Donks); i++ {
		if !zap[ch.Donks[i].XID] {
			ch.Donks[j] = ch.Donks[i]
			j++
		}
	}
	ch.Donks = ch.Donks[:j]

	if strings.HasPrefix(noise, "<p>") {
		noise = noise[3:]
	}
	ch.HTML = template.HTML(noise)
	if short := shortname(ch.UserID, ch.Who); short != "" {
		ch.Handle = short
	} else {
		ch.Handle, _ = handles(ch.Who)
	}

}

func inlineimgsfor(honk *Honk) func(node *html.Node) string {
	return func(node *html.Node) string {
		src := htfilter.GetAttr(node, "src")
		alt := htfilter.GetAttr(node, "alt")
		base := path.Base(src)
		didx, _ := strconv.Atoi(base)
		if strings.HasPrefix(src, serverPrefix) && didx > 0 && didx <= len(honk.Donks) {
			slog.Debug("skipping inline image", "src", src)
			return ""
		}
		d := savedonk(src, "image", alt, "image", true)
		if d != nil {
			honk.Donks = append(honk.Donks, d)
		}
		slog.Debug("inline image", "src", src)
		return ""
	}
}

func imaginate(honk *Honk) {
	var htf htfilter.Filter
	htf.Imager = inlineimgsfor(honk)
	htf.BaseURL, _ = url.Parse(honk.XID)
	htf.String(honk.Noise)
}

var re_dangerous = regexp.MustCompile("^[a-zA-Z]{2}:")

func precipitate(honk *Honk) {
	noise := honk.Noise
	if re_dangerous.MatchString(noise) {
		idx := strings.Index(noise, "\n")
		if idx == -1 {
			honk.Precis = noise
			noise = ""
		} else {
			honk.Precis = noise[:idx]
			noise = noise[idx+1:]
		}
		var marker mz.Marker
		marker.Short = true
		honk.Precis = marker.Mark(strings.TrimSpace(honk.Precis))
		honk.Noise = noise
	}
}

func translate(honk *Honk) {
	if honk.Format == "html" {
		return
	}
	noise := honk.Noise

	var marker mz.Marker
	marker.HashLinker = ontoreplacer
	marker.AtLinker = attoreplacer
	marker.AllowImages = true
	noise = strings.TrimSpace(noise)
	noise = marker.Mark(noise)
	honk.Noise = noise
	honk.Onts = append(honk.Onts, marker.HashTags...)
	honk.Mentions = bunchofgrapes(marker.Mentions)
	for _, t := range oneofakind(strings.Split(strings.ToLower(honk.Onties), " ")) {
		if t[0] != '#' {
			t = "#" + t
		}
		honk.Onts = append(honk.Onts, t)
	}
	honk.Onts = oneofakind(honk.Onts)
	honk.Mentions = append(honk.Mentions, bunchofgrapes(oneofakind(strings.Split(honk.SeeAlso, " ")))...)
}

func redoimages(honk *Honk) {
	zap := make(map[string]bool)
	{
		var htf htfilter.Filter
		htf.Imager = replaceimgsand(zap, true, honk)
		htf.SpanClasses = allowedclasses
		htf.BaseURL, _ = url.Parse(honk.XID)
		p, _ := htf.String(honk.Precis)
		n, _ := htf.String(honk.Noise)
		honk.Precis = string(p)
		honk.Noise = string(n)
	}
	j := 0
	for i := 0; i < len(honk.Donks); i++ {
		if !zap[honk.Donks[i].XID] {
			honk.Donks[j] = honk.Donks[i]
			j++
		}
	}
	honk.Donks = honk.Donks[:j]

	honk.Noise = re_memes.ReplaceAllString(honk.Noise, "")
	honk.Noise = strings.Replace(honk.Noise, "<a href=", "<a class=\"mention u-url\" href=", -1)
}

func xcelerate(b []byte) string {
	letters := "BCDFGHJKLMNPQRSTVWXYZbcdfghjklmnpqrstvwxyz1234567891234567891234"
	for i, c := range b {
		b[i] = letters[c&63]
	}
	s := string(b)
	return s
}

func shortxid(xid string) string {
	h := sha512.New512_256()
	io.WriteString(h, xid)
	return xcelerate(h.Sum(nil)[:20])
}

func xfiltrate() string {
	var b [18]byte
	rand.Read(b[:])
	return xcelerate(b[:])
}

func xfildate() string {
	var b [21]byte
	rand.Read(b[:])
	now := time.Now().Unix() / 60 / 60 / 24
	b[2] = byte(now & 63)
	b[1] = byte((now / 64) & 63)
	b[0] = byte((now / 64 / 64) & 63)
	return xcelerate(b[:])
}

func grapevine(mentions []Mention) []string {
	var s []string
	for _, m := range mentions {
		s = append(s, m.Where)
	}
	return s
}

func bunchofgrapes(m []string) []Mention {
	var mentions []Mention
	for i := range m {
		who := m[i]
		if strings.HasPrefix(who, "@https://") {
			mentions = append(mentions, Mention{Who: who, Where: who[1:]})
			continue
		}
		where := gofish(who)
		if where != "" {
			mentions = append(mentions, Mention{Who: who, Where: where})
		}
	}
	return mentions
}

func newEnding(name, repl string) string {
	if dot := strings.LastIndexByte(name, '.'); dot != -1 {
		name = name[:dot]
	}
	return name + repl
}

type Emu struct {
	ID   string
	Name string
	Type string
}

var re_emus = regexp.MustCompile(`:[[:alnum:]_-]+:`)

var emucache = gencache.New(gencache.Options[string, *Emu]{Fill: func(ename string) (*Emu, bool) {
	fname := ename[1 : len(ename)-1]
	exts := []string{".png", ".gif"}
	for _, ext := range exts {
		_, err := os.Stat(dataDir + "/emus/" + fname + ext)
		if err != nil {
			continue
		}
		url := serverURL("/emu/%s%s", fname, ext)
		if develMode {
			url = fmt.Sprintf("/emu/%s%s", fname, ext)
		}
		return &Emu{ID: url, Name: ename, Type: "image/" + ext[1:]}, true
	}
	return nil, true
}, Duration: 10 * time.Second})

func herdofemus(noise string) []*Emu {
	m := re_emus.FindAllString(noise, -1)
	m = oneofakind(m)
	var emus []*Emu
	for _, e := range m {
		emu, _ := emucache.Get(e)
		if emu == nil {
			continue
		}
		emus = append(emus, emu)
	}
	return emus
}

var re_memes = regexp.MustCompile("meme: ?([^\n]+)")
var re_avatar = regexp.MustCompile("avatar: ?([^\n]+)")
var re_banner = regexp.MustCompile("banner: ?([^\n]+)")
var re_convoy = regexp.MustCompile("convoy: ?([^\n]+)")
var re_convalidate = regexp.MustCompile("^(https?|tag|data):")

func memetize(honk *Honk) {
	repl := func(x string) string {
		name := x[5:]
		if name[0] == ' ' {
			name = name[1:]
		}
		fd, err := os.Open(dataDir + "/memes/" + name)
		if err != nil {
			slog.Info("no meme", "name", name)
			return x
		}
		var peek [512]byte
		n, _ := fd.Read(peek[:])
		ct := http.DetectContentType(peek[:n])
		fd.Close()

		url := serverURL("/meme/%s", name)
		fileid, err := savefile(name, name, url, ct, false, nil, nil)
		if err != nil {
			slog.Error("error saving meme", "err", err)
			return x
		}
		d := &Donk{
			FileID: fileid,
			Name:   name,
			Media:  ct,
			URL:    url,
			Local:  false,
		}
		honk.Donks = append(honk.Donks, d)
		return ""
	}
	honk.Noise = re_memes.ReplaceAllStringFunc(honk.Noise, repl)
}

var re_quickmention = regexp.MustCompile("(^|[ \n])@[[:alnum:]_]+([ \n:;.,']|$)")

func quickrename(s string, userid UserID) string {
	nonstop := true
	for nonstop {
		nonstop = false
		s = re_quickmention.ReplaceAllStringFunc(s, func(m string) string {
			prefix := ""
			if m[0] == ' ' || m[0] == '\n' {
				prefix = m[:1]
				m = m[1:]
			}
			prefix += "@"
			m = m[1:]
			tail := ""
			if last := m[len(m)-1]; last == ' ' || last == '\n' ||
				last == ':' || last == ';' ||
				last == '.' || last == ',' || last == '\'' {
				tail = m[len(m)-1:]
				m = m[:len(m)-1]
			}

			xid := fullname(m, userid)

			if xid != "" {
				_, name := handles(xid)
				if name != "" {
					nonstop = true
					m = name
				}
			}
			return prefix + m + tail
		})
	}
	return s
}

var honkerdirectory = gencache.New(gencache.Options[UserID, map[string]*Honker]{Fill: func(userid UserID) (map[string]*Honker, bool) {
	honkers := gethonkers(userid)
	m := make(map[string]*Honker)
	for _, h := range honkers {
		m[h.XID] = h
	}
	return m, true
}, Invalidator: &honkerinvalidator})

func gethonker(userid UserID, xid string) *Honker {
	m, ok := honkerdirectory.Get(userid)
	if ok {
		return m[xid]
	}
	return nil
}
func shortname(userid UserID, xid string) string {
	if h := gethonker(userid, xid); h != nil {
		return h.Name
	}
	return ""
}

var fullnames = gencache.New(gencache.Options[UserID, map[string]string]{Fill: func(userid UserID) (map[string]string, bool) {
	honkers := gethonkers(userid)
	m := make(map[string]string)
	for _, h := range honkers {
		m[h.Name] = h.XID
	}
	return m, true
}, Invalidator: &honkerinvalidator})

func fullname(name string, userid UserID) string {
	m, ok := fullnames.Get(userid)
	if ok {
		return m[name]
	}
	return ""
}

func attoreplacer(m string) string {
	fill := `<span class="h-card"><a class="u-url mention" href="%s">%s</a></span>`
	if strings.HasPrefix(m, "@https://") {
		return fmt.Sprintf(fill, html.EscapeString(m[1:]), html.EscapeString(m))
	}
	where := gofish(m)
	if where == "" {
		return m
	}
	who := m[0 : 1+strings.IndexByte(m[1:], '@')]
	return fmt.Sprintf(fill, html.EscapeString(where), html.EscapeString(who))
}

func ontoreplacer(h string) string {
	return fmt.Sprintf(`<a class="mention hashtag" href="%s">%s</a>`,
		serverURL("/o/%s", strings.ToLower(h[1:])), h)
}

var re_unurl = regexp.MustCompile("https://([^/]+).*/([^/]+)")
var re_urlhost = regexp.MustCompile("https://([^/ #)?]+)")

func originate(u string) string {
	m := re_urlhost.FindStringSubmatch(u)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

var xonkInvalidator gencache.Invalidator[string]

var allhandles = gencache.New(gencache.Options[string, string]{Fill: func(xid string) (string, bool) {
	handle := getxonker(xid, "handle")
	if handle == "" {
		slog.Debug("need to get a handle", "xid", xid)
		info, _, err := investigate(xid)
		if err != nil {
			slog.Debug("failed to get handle", "xid", xid, "err", err)
			m := re_unurl.FindStringSubmatch(xid)
			if len(m) > 2 {
				handle = m[2]
			} else {
				handle = xid
			}
			savexonker(xid, handle, "handle")
		} else {
			handle = info.Name
		}
	}
	return handle, true
}, Invalidator: &xonkInvalidator})

// handle, handle@host
func handles(xid string) (string, string) {
	if xid == "" || xid == thewholeworld || strings.HasSuffix(xid, "/followers") {
		return "", ""
	}
	handle, _ := allhandles.Get(xid)
	if handle == xid {
		return xid, xid
	}
	return handle, handle + "@" + originate(xid)
}

func butnottooloud(aud []string) {
	for i, a := range aud {
		if strings.HasSuffix(a, "/followers") {
			aud[i] = ""
		}
	}
}

func loudandproud(aud []string) bool {
	for _, a := range aud {
		if a == thewholeworld {
			return true
		}
	}
	return false
}

func firstclass(honk *Honk) bool {
	return honk.Audience[0] == thewholeworld
}

func oneofakind(a []string) []string {
	seen := make(map[string]bool)
	seen[""] = true
	j := 0
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			a[j] = s
			j++
		}
	}
	if j < len(a)/2 {
		rv := make([]string, j)
		copy(rv, a[:j])
		return rv
	}
	return a[:j]
}

var ziggies = gencache.New(gencache.Options[UserID, *KeyInfo]{Fill: func(userid UserID) (*KeyInfo, bool) {
	user, ok := somenumberedusers.Get(userid)
	if !ok {
		return nil, false
	}
	ki := new(KeyInfo)
	ki.keyname = user.URL + "#key"
	ki.seckey = user.SecKey
	return ki, true
}})

func ziggy(userid UserID) *KeyInfo {
	ki, _ := ziggies.Get(userid)
	return ki
}

var zaggies = gencache.New(gencache.Options[string, httpsig.PublicKey]{Fill: func(keyname string) (httpsig.PublicKey, bool) {
	data := getxonker(keyname, "pubkey")
	if data == "" {
		slog.Debug("hitting the webs for missing pubkey", "keyname", keyname)
		j, err := GetJunk(readyLuserOne, keyname)
		if err != nil {
			slog.Info("error getting pubkey", "keyname", keyname, "err", err)
			savexonker(keyname, "failed", "pubkey")
			return httpsig.PublicKey{}, true
		}
		allinjest(originate(keyname), j)
		data = getxonker(keyname, "pubkey")
		if data == "" {
			slog.Info("key not found after ingesting", "keyname", keyname)
			savexonker(keyname, "failed", "pubkey")
			return httpsig.PublicKey{}, true
		}
	}
	if data == "failed" {
		slog.Info("lookup previously failed key", "keyname", keyname)
		return httpsig.PublicKey{}, true
	}
	_, key, err := httpsig.DecodeKey(data)
	if err != nil {
		slog.Info("error decoding pubkey", "keyname", keyname, "err", err)
		return key, true
	}
	return key, true
}, Limit: 4096, Invalidator: &xonkInvalidator})

func zaggy(keyname string) (httpsig.PublicKey, error) {
	key, _ := zaggies.Get(keyname)
	return key, nil
}

func savingthrow(keyname string) {
	when := time.Now().Add(-30 * time.Minute).UTC().Format(dbtimeformat)
	stmtDeleteXonker.Exec(keyname, "pubkey", when)
	zaggies.Clear(keyname)
}

func keymatch(keyname string, actor string) string {
	origin := originate(actor)
	if origin == originate(keyname) {
		return origin
	}
	return ""
}

var lighter = synlight.New(synlight.Options{Format: synlight.HTML})

func highlight(code, name string) string {
	return "<pre><code>" + lighter.HighlightString(code, name) + "</code></pre>"
}
