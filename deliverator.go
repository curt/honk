//
// Copyright (c) 2019,2023 Ted Unangst <tedu@tedunangst.com>
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
	"bytes"
	"database/sql"
	"log/slog"
	notrand "math/rand"
	"strings"
	"sync"
	"time"

	"humungus.tedunangst.com/r/webs/gate"
)

type Doover struct {
	ID     int64
	When   time.Time
	Userid UserID
	Tries  int64
	Rcpt   string
	Msgs   [][]byte
}

func sayitagain(doover Doover) {
	doover.Tries += 1
	var drift time.Duration
	if doover.Tries <= 3 { // 5, 10, 15 minutes
		drift = time.Duration(doover.Tries*5) * time.Minute
	} else if doover.Tries <= 6 { // 1, 2, 3 hours
		drift = time.Duration(doover.Tries-3) * time.Hour
	} else if doover.Tries <= 16 { // 12 hours
		drift = time.Duration(12) * time.Hour
	} else {
		slog.Info("he's dead jim", "rcpt", doover.Rcpt)
		return
	}
	drift += time.Duration(notrand.Int63n(int64(drift / 10)))
	when := time.Now().Add(drift)
	data := bytes.Join(doover.Msgs, []byte{0})
	_, err := stmtAddDoover.Exec(when.UTC().Format(dbtimeformat), doover.Tries, doover.Userid, doover.Rcpt, data)
	if err != nil {
		slog.Error("error saving doover", "err", err)
	}
	select {
	case pokechan <- 0:
	default:
	}
}

const nearlyDead = 15

func lethaldose(err error) int64 {
	str := err.Error()
	if strings.Contains(str, "no such host") {
		return nearlyDead
	}
	return 0
}

func letitslide(err error) bool {
	str := err.Error()
	if strings.Contains(str, "http post status: 400") {
		return true
	}
	if strings.Contains(str, "http post status: 422") {
		return true
	}
	return false
}

var dqmtx sync.Mutex

func delinquent(userid UserID, rcpt string, msg []byte) bool {
	dqmtx.Lock()
	defer dqmtx.Unlock()
	row := stmtDeliquentCheck.QueryRow(userid, rcpt)
	var dooverid int64
	var data []byte
	err := row.Scan(&dooverid, &data)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		slog.Error("error scanning deliquent check", "err", err)
		return true
	}
	data = append(data, 0)
	data = append(data, msg...)
	_, err = stmtDeliquentUpdate.Exec(data, dooverid)
	if err != nil {
		slog.Error("error updating deliquent", "err", err)
		return true
	}
	return true
}

func deliverate(userid UserID, rcpt string, msg []byte) {
	if delinquent(userid, rcpt, msg) {
		return
	}
	var d Doover
	d.Userid = userid
	d.Tries = 0
	d.Rcpt = rcpt
	d.Msgs = append(d.Msgs, msg)
	deliveration(d)
}

var garage = gate.NewLimiter(40)

func deliveration(doover Doover) {
	requestWG.Add(1)
	defer requestWG.Done()
	rcpt := doover.Rcpt
	garage.StartKey(rcpt)
	defer garage.FinishKey(rcpt)

	ki := ziggy(doover.Userid)
	if ki == nil {
		slog.Error("lost key for delivery", "userid", doover.Userid)
		return
	}
	var inbox string
	// already did the box indirection
	if rcpt[0] == '%' {
		inbox = rcpt[1:]
	} else {
		box, _ := boxofboxes.Get(rcpt)
		if box == nil {
			slog.Debug("failed getting inbox", "rcpt", rcpt)
			if doover.Tries < nearlyDead {
				doover.Tries = nearlyDead
			}
			sayitagain(doover)
			return
		}
		inbox = box.In
	}
	for i, msg := range doover.Msgs {
		if i > 0 {
			time.Sleep(2 * time.Second)
		}
		err := PostMsg(ki.keyname, ki.seckey, inbox, msg)
		if err != nil {
			slog.Debug("failed to post json", "inbox", inbox, "err", err)
			if t := lethaldose(err); t > doover.Tries {
				doover.Tries = t
			}
			if letitslide(err) {
				slog.Debug("whatever myever", "inbox", inbox, "err", err)
				continue
			}
			doover.Msgs = doover.Msgs[i:]
			sayitagain(doover)
			return
		}
	}
}

var pokechan = make(chan int, 1)

func getdoovers() []Doover {
	rows, err := stmtGetDoovers.Query()
	if err != nil {
		slog.Error("wat?")
		time.Sleep(1 * time.Minute)
		return nil
	}
	defer rows.Close()
	var doovers []Doover
	for rows.Next() {
		var d Doover
		var dt string
		err := rows.Scan(&d.ID, &dt)
		if err != nil {
			slog.Error("error scanning dooverid", "err", err)
			continue
		}
		d.When, _ = time.Parse(dbtimeformat, dt)
		doovers = append(doovers, d)
	}
	return doovers
}

func extractdoover(d *Doover) error {
	dqmtx.Lock()
	defer dqmtx.Unlock()
	row := stmtLoadDoover.QueryRow(d.ID)
	var data []byte
	err := row.Scan(&d.Tries, &d.Userid, &d.Rcpt, &data)
	if err != nil {
		return err
	}
	_, err = stmtZapDoover.Exec(d.ID)
	if err != nil {
		return err
	}
	d.Msgs = bytes.Split(data, []byte{0})
	return nil
}

func redeliverator() {
	workinprogress++
	sleeper := time.NewTimer(5 * time.Second)
	for {
		select {
		case <-pokechan:
			if !sleeper.Stop() {
				<-sleeper.C
			}
			time.Sleep(5 * time.Second)
		case <-sleeper.C:
		case <-endoftheworld:
			readyalready <- true
			return
		}

		doovers := getdoovers()

		now := time.Now()
		nexttime := now.Add(24 * time.Hour)
		for _, d := range doovers {
			if d.When.Before(now) {
				err := extractdoover(&d)
				if err != nil {
					slog.Error("error extracting doover", "id", d.ID, "err", err)
					continue
				}
				slog.Info("redeliverating", "rcpt", d.Rcpt, "try", d.Tries)
				deliveration(d)
			} else if d.When.Before(nexttime) {
				nexttime = d.When
			}
		}
		now = time.Now()
		dur := 5 * time.Second
		if now.Before(nexttime) {
			dur += nexttime.Sub(now).Round(time.Second)
		}
		sleeper.Reset(dur)
	}
}
