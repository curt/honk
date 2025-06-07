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
	"database/sql"
	"log"
	"strings"
)

var myVersion = 54 // index honks.dt

type dbexecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func doordie(db dbexecer, s string, args ...interface{}) {
	_, err := db.Exec(s, args...)
	if err != nil {
		log.Fatalf("can't run %s: %s", s, err)
	}
}

func upgradedb() {
	db := opendatabase()
	dbversion := 0
	getconfig("dbversion", &dbversion)
	getconfig("servername", &serverName)

	if dbversion < 48 {
		log.Fatal("database is too old to upgrade")
	}
	var err error
	var tx *sql.Tx
	try := func(s string, args ...interface{}) *sql.Rows {
		var rows *sql.Rows
		if strings.HasPrefix(s, "select") {
			if tx != nil {
				rows, err = tx.Query(s, args...)
			} else {
				rows, err = db.Query(s, args...)
			}
		} else {
			if tx != nil {
				_, err = tx.Exec(s, args...)
			} else {
				_, err = db.Exec(s, args...)
			}
		}
		if err != nil {
			log.Fatalf("can't run %s: %s", s, err)
		}
		return rows
	}
	setV := func(ver int64) {
		try("update config set value = ? where key = 'dbversion'", ver)
	}

	switch dbversion {
	case 48:
		try("create index idx_honksurl on honks(url)")
		setV(49)
		fallthrough
	case 49:
		try("create index idx_honksrid on honks(rid) where rid <> ''")
		setV(50)
		fallthrough
	case 50:
		try("alter table filemeta add column meta text")
		try("update filemeta set meta = '{}'")
		setV(51)
		fallthrough
	case 51:
		hashes := make(map[string]string)
		blobdb := openblobdb()
		rows, err := blobdb.Query("select xid, hash, media from filedata")
		checkErr(err)
		for rows.Next() {
			var xid, hash, media string
			err = rows.Scan(&xid, &hash, &media)
			checkErr(err)
			hashes[xid] = hash + " " + media
		}
		rows.Close()
		tx, err = db.Begin()
		checkErr(err)
		try("create table filehashes (xid text, hash text, media text)")
		try("create index idx_filehashes on filehashes(hash)")
		for xid, data := range hashes {
			parts := strings.Split(data, " ")
			try("insert into filehashes (xid, hash, media) values (?, ?, ?)", xid, parts[0], parts[1])
		}
		setV(52)
		err = tx.Commit()
		checkErr(err)
		tx = nil
		fallthrough
	case 52:
		try("create index idx_filehashesxid on filehashes(xid)")
		setV(53)
		fallthrough
	case 53:
		try("create index idx_honksdt on honks(dt)")
		setV(54)
		fallthrough
	case 54:
		try("analyze")
		closedatabases()

	default:
		log.Fatalf("can't upgrade unknown version %d", dbversion)
	}
}
