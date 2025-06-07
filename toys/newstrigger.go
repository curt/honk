package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

var newsaddr, groupname string

func main() {
	var listenaddr string
	flag.StringVar(&listenaddr, "listen", "", "listen on")
	flag.StringVar(&newsaddr, "news", "", "nntp address")
	flag.StringVar(&groupname, "group", "alt.honk", "newsgroup")
	flag.Parse()

	if listenaddr == "" {
		log.Fatal("listen is required")
	}
	if newsaddr == "" {
		log.Fatal("news is required")
	}

	l, err := net.Listen("tcp", listenaddr)
	if err != nil {
		log.Fatal("listen %s", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/trigger", trigger)
	http.Serve(l, mux)
}

type payload struct {
	Honk honk
}

type honk struct {
	Whofore  int
	RID      string
	XID      string
	Date     time.Time
	Honker   string
	What     string
	Noise    string
	Audience []string
}

type post struct {
	MessageID  string
	Path       string
	Newsgroups string
	From       string
	Date       time.Time
	Subject    string
	Body       string
}

func (post *post) Write(w io.Writer) {
	fmt.Fprintf(w, "Message-ID: %s\n", post.MessageID)
	fmt.Fprintf(w, "Path: %s\n", post.Path)
	fmt.Fprintf(w, "Newsgroups: %s\n", post.Newsgroups)
	fmt.Fprintf(w, "From: %s\n", post.From)
	fmt.Fprintf(w, "Date: %s\n", post.Date.Format(time.RFC1123Z))
	fmt.Fprintf(w, "Subject: %s\n", post.Subject)
	fmt.Fprintf(w, "Content-Type: %s\n", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "\n")
	if post.Body != "" {
		fmt.Fprintf(w, "%s\n", post.Body)
	}
}

func trigger(w http.ResponseWriter, r *http.Request) {
	var payload payload
	dec := json.NewDecoder(r.Body)
	dec.Decode(&payload)

	honk := payload.Honk
	if honk.Whofore != 2 ||
		honk.Audience[0] != "https://www.w3.org/ns/activitystreams#Public" ||
		honk.What != "honk" ||
		honk.RID != "" {
		return
	}
	var post post
	user := honk.Honker[strings.LastIndexByte(honk.Honker, '/')+1:]
	xid := honk.XID[strings.LastIndexByte(honk.XID, '/')+1:]
	host := honk.Honker[8:]
	host = host[:strings.IndexByte(host, '/')]

	post.MessageID = fmt.Sprintf("<%s@%s>", xid, host)
	post.Path = fmt.Sprintf("%s!%s", host, user)
	post.Newsgroups = groupname
	post.From = fmt.Sprintf("%s <%s@%s>", user, user, host)
	post.Date = honk.Date
	noise := honk.Noise
	idx := strings.IndexAny(noise, ".?!\n")
	if idx == -1 {
		idx = len(noise)
	} else if noise[idx] != '\n' {
		idx++
	}
	post.Subject = noise[:idx]
	if idx < len(noise) {
		noise = noise[idx:]
	} else {
		noise = ""
	}
	noise = strings.TrimSpace(noise)
	post.Body = noise

	c, err := net.Dial("tcp", newsaddr)
	if err != nil {
		log.Printf("cannot connect: %s", err)
		return
	}
	defer c.Close()
	bw := bufio.NewWriter(c)
	tw := textproto.NewWriter(bw)
	tw.PrintfLine("POST")
	dw := tw.DotWriter()
	post.Write(dw)
	dw.Close()
	tw.PrintfLine("QUIT")
	bw.Flush()
}
