//
// Copyright (c) 2024 Ted Unangst <tedu@tedunangst.com>
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
	"crypto/sha512"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"humungus.tedunangst.com/r/webs/gencache"
	"humungus.tedunangst.com/r/webs/lazif"
)

var storeTheFilesInTheFileSystem = true

func hashfiledata(data []byte) string {
	h := sha512.New512_256()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func needfilehash(data []byte, xid *string) string {
	hash := hashfiledata(data)
	row := stmtCheckFileHash.QueryRow(hash)
	err := row.Scan(xid)
	if err == nil {
		return ""
	}
	if err != sql.ErrNoRows {
		slog.Error("error checking file hash", "err", err)
	}
	return hash
}

func filepath(xid string) string {
	parts := strings.SplitN(xid, ".", 2)
	subdir := "xx"
	if len(parts[0]) == 21 {
		subdir = xid[:2]
	}
	fname := fmt.Sprintf("%s/attachments/%s/%s", dataDir, subdir, xid)
	return fname
}

func savefile(name string, desc string, url string, media string, local bool, data []byte, meta *DonkMeta) (int64, error) {
	fileid, _, err := savefileandxid(name, desc, url, media, local, data, meta)
	return fileid, err
}

func savefiledata(xid string, data []byte) error {
	if storeTheFilesInTheFileSystem {
		fname := filepath(xid)
		os.Mkdir(fname[:strings.LastIndexByte(fname, '/')], 0700)
		err := os.WriteFile(fname, data, 0700)
		return err
	} else {
		if data == nil {
			data = []byte{}
		}
		_, err := stmtSaveBlobData.Exec(xid, data)
		return err
	}
}

func savefileandxid(name string, desc string, url string, media string, local bool, data []byte, meta *DonkMeta) (int64, string, error) {
	var xid string
	if local {
		if hash := needfilehash(data, &xid); hash != "" {
			xid = xfildate()
			switch media {
			case "image/png":
				xid += ".png"
			case "image/jpeg":
				xid += ".jpg"
			case "image/svg+xml":
				xid += ".svg"
			case "application/pdf":
				xid += ".pdf"
			case "text/plain":
				xid += ".txt"
			}
			err := savefiledata(xid, data)
			if err == nil {
				_, err = stmtSaveFileHash.Exec(xid, hash, media)
			}
			if err != nil {
				return 0, "", err
			}
		}
		if url == "" {
			url = serverURL("/d/%s", xid)
		}
	}

	j := "{}"
	if meta != nil {
		j, _ = jsonify(meta)
	}
	res, err := stmtSaveFile.Exec(xid, name, desc, url, media, local, j)
	if err != nil {
		return 0, "", err
	}
	fileid, _ := res.LastInsertId()
	return fileid, xid, nil
}

func getfileinfo(xid string) *Donk {
	donk := new(Donk)
	row := stmtGetFileInfo.QueryRow(xid)
	var j string
	err := row.Scan(&donk.URL, &donk.Media, &j)
	if err == nil {
		donk.XID = xid
		unjsonify(j, &donk.Meta)
		return donk
	}
	if err != sql.ErrNoRows {
		slog.Error("error finding file", "xid", xid, "err", err)
	}
	return nil
}

func finddonkid(fileid int64, url string) *Donk {
	donk := new(Donk)
	row := stmtFindFileId.QueryRow(fileid, url)
	err := row.Scan(&donk.XID, &donk.Local, &donk.Desc)
	if err == nil {
		donk.FileID = fileid
		return donk
	}
	if err != sql.ErrNoRows {
		slog.Error("error finding file", "id", fileid, "err", err)
	}
	return nil
}

func finddonk(url string) *Donk {
	donk := new(Donk)
	row := stmtFindFile.QueryRow(url)
	err := row.Scan(&donk.FileID, &donk.XID)
	if err == nil {
		return donk
	}
	if err != sql.ErrNoRows {
		slog.Error("error finding file", "url", url, "err", err)
	}
	return nil
}

func loadfiledata(xid string) ([]byte, func(), error) {
	fname := filepath(xid)
	fd, err := os.Open(fname)
	if err != nil {
		return nil, nil, err
	}
	defer fd.Close()
	info, err := fd.Stat()
	if err != nil {
		return nil, nil, err
	}
	data, err := syscall.Mmap(int(fd.Fd()), 0, int(info.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, err
	}
	return data, func() { syscall.Munmap(data) }, nil
}

var errNoBlob = errors.New("no blobdb")

func loadblobdata(xid string) ([]byte, func(), error) {
	if g_blobdb == nil {
		return nil, nil, errNoBlob
	}

	var data sql.RawBytes
	rows, err := stmtGetBlobData.Query(xid)
	if err != nil {
		return nil, nil, err
	}
	if rows.Next() {
		err = rows.Scan(&data)
	} else {
		err = errors.New("blob not found")
	}
	if err != nil {
		rows.Close()
		return nil, nil, err
	}
	return data, func() { rows.Close() }, nil
}

func loaddata(xid string) ([]byte, func(), error) {
	if storeTheFilesInTheFileSystem {
		data, closer, err := loadfiledata(xid)
		if err == nil || g_blobdb == nil {
			return data, closer, err
		}
		return loadblobdata(xid)
	} else {
		data, closer, err := loadblobdata(xid)
		if err == nil {
			return data, closer, err
		}
		return loadfiledata(xid)
	}
}

func servefiledata(w http.ResponseWriter, r *http.Request, xid string) {
	wantAVIF := false
	if convertAVIF && strings.HasSuffix(xid, ".avif") {
		wantAVIF = true
		xid = newEnding(xid, ".jpg")
	}
	var media string
	row := stmtGetFileMedia.QueryRow(xid)
	err := row.Scan(&media)
	if err != nil {
		slog.Error("error loading file", "err", err)
		http.NotFound(w, r)
		return
	}
	data, closer, err := loaddata(xid)
	if err != nil {
		slog.Error("error loading file", "err", err)
		http.NotFound(w, r)
		return
	}
	defer closer()
	preview := r.FormValue("preview") == "1"
	if preview && strings.HasPrefix(media, "image") {
		img, err := lilshrink(data)
		if err == nil {
			data = img.Data
		}
	}
	if wantAVIF {
		d2 := avifEncode(xid, data)
		if d2 != nil {
			data = d2
			media = "image/avif"
		}
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "max-age="+somedays())
	w.Write(data)
}

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func cleanupfiles() {
	var rows *sql.Rows
	var err error
	scan := func() string {
		var xid string
		err = rows.Scan(&xid)
		checkErr(err)
		return xid
	}

	fsFiles := make(map[string]bool)
	dbFiles := make(map[string]bool)
	if storeTheFilesInTheFileSystem {
		walker := func(pathname string, ent fs.DirEntry, err error) error {
			if ent.IsDir() {
				return nil
			}
			fname := path.Base(pathname)
			fsFiles[fname] = true
			return nil
		}
		dir := os.DirFS(dataDir)
		fs.WalkDir(dir, "attachments", walker)
	}
	if g_blobdb != nil {
		rows, err = g_blobdb.Query("select xid from filedata")
		checkErr(err)
		for rows.Next() {
			xid := scan()
			dbFiles[xid] = true
		}
		rows.Close()
	}

	db := opendatabase()
	rows, err = db.Query("select xid from filemeta")
	checkErr(err)
	for rows.Next() {
		xid := scan()
		delete(fsFiles, xid)
		delete(dbFiles, xid)
	}
	rows.Close()

	tx, err := db.Begin()
	checkErr(err)
	for xid := range fsFiles {
		_, err = tx.Exec("delete from filehashes where xid = ?", xid)
		checkErr(err)
	}
	for xid := range dbFiles {
		_, err = tx.Exec("delete from filehashes where xid = ?", xid)
		checkErr(err)
	}
	err = tx.Commit()
	checkErr(err)

	if storeTheFilesInTheFileSystem {
		for xid := range fsFiles {
			fname := filepath(xid)
			os.Remove(fname)
		}

	}
	if g_blobdb != nil {
		tx, err = g_blobdb.Begin()
		checkErr(err)
		for xid := range dbFiles {
			_, err = tx.Exec("delete from filedata where xid = ?", xid)
			checkErr(err)
		}
		err = tx.Commit()
		checkErr(err)
	}

	closedatabases()
}

func extractblobs() {
	if !storeTheFilesInTheFileSystem {
		log.Fatal("can only extract blobs when using filestore")
	}
	if g_blobdb == nil {
		log.Fatal("the blob.db is already gone")
	}
	rows, err := g_blobdb.Query("select xid, content from filedata")
	checkErr(err)
	defer rows.Close()
	for rows.Next() {
		var xid string
		var data sql.RawBytes
		err = rows.Scan(&xid, &data)
		checkErr(err)
		err = savefiledata(xid, data)
		checkErr(err)
	}
	fmt.Printf("extraction complete. blob.db is redundant.\n")
}

var avifCache = gencache.New(gencache.Options[string, []byte]{
	Duration: time.Second / 4,
})

func avifEncode(xid string, data []byte) []byte {
	res, _ := avifCache.GetWith(xid, func(string) ([]byte, bool) {
		shrinkgate.Start()
		defer shrinkgate.Finish()
		return lazif.EncodeJPEG(data), true
	})
	return res
}
