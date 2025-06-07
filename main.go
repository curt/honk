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
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime/pprof"
	"sort"
	"strings"
	"syscall"
	"time"

	"humungus.tedunangst.com/r/gonix"
	"humungus.tedunangst.com/r/webs/lazif"
)

var softwareVersion = "1.5.1"

var serverName string
var serverPrefix string
var masqName string
var dataDir = "."
var viewDir = "."
var iconName = "icon.png"
var serverMsg template.HTML
var aboutMsg template.HTML
var loginMsg template.HTML
var collectForwards = true
var convertAVIF = false
var acceptAVIF = false

const envListener = "HONK_LISTENER"

func serverURL(u string, args ...interface{}) string {
	return fmt.Sprintf("https://"+serverName+u, args...)
}

func ElaborateUnitTests() {
}

func unplugserver(hostname string) {
	db := opendatabase()
	xid := fmt.Sprintf("https://%s", hostname)
	db.Exec("delete from honkers where xid = ? and flavor = 'dub'", xid)
	db.Exec("delete from doovers where rcpt = ?", xid)
	xid += "/%"
	db.Exec("delete from honkers where xid like ? and flavor = 'dub'", xid)
	db.Exec("delete from doovers where rcpt like ?", xid)
}

func reexecArgs(cmd string) []string {
	var args []string
	if dataDir != "." {
		args = append(args, "-datadir", dataDir)
	}
	if viewDir != "." {
		args = append(args, "-viewdir", viewDir)
	}
	if logFile != "" {
		args = append(args, "-log", logFile)
	}
	args = append(args, cmd)
	return args
}

func runWebServer(in *os.File) *exec.Cmd {
	proc := exec.Command(os.Args[0], reexecArgs("serve")...)
	proc.Stdout = os.Stdout
	proc.Stderr = os.Stderr
	proc.Stdin = in
	env := os.Environ()
	env = append(env, envListener+"=3")
	proc.Env = env
	var ld *os.File
	if lis, ok := listenSocket.(*net.TCPListener); ok {
		ld, _ = lis.File()
	} else {
		ld, _ = listenSocket.(*net.UnixListener).File()
	}
	proc.ExtraFiles = append(proc.ExtraFiles, ld)
	err := proc.Start()
	if err != nil {
		log.Fatalf("can't exec new server: %s", err)
	}
	return proc
}

func errx(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
var memprofile = flag.String("memprofile", "", "write memory profile to this file")
var memprofilefd *os.File
var logFile string

func usage() {
	flag.PrintDefaults()
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, "\n  available honk commands:\n")
	var msgs []string
	for n, c := range commands {
		msgs = append(msgs, fmt.Sprintf("    %s: %s\n", n, c.help))
	}
	sort.Strings(msgs)
	fmt.Fprintf(out, "%s", strings.Join(msgs, ""))
}

func main() {
	commands["help"] = cmd{
		help: "you're looking at it",
		callback: func(args []string) {
			usage()
		},
	}
	var debug bool
	flag.StringVar(&dataDir, "datadir", getenv("HONK_DATADIR", dataDir), "data directory")
	flag.StringVar(&viewDir, "viewdir", getenv("HONK_VIEWDIR", viewDir), "view directory")
	flag.StringVar(&logFile, "log", "", "log file")
	flag.BoolVar(&debug, "debug", false, "debug logging")
	flag.Usage = usage

	flag.Parse()
	if logFile != "" {
		fd, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			errx("can't open logfile: %s", err)
		}
		log.SetOutput(fd)
	}
	if debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			errx("can't open cpu profile: %s", err)
		}
		pprof.StartCPUProfile(f)
	}
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			errx("can't open mem profile: %s", err)
		}
		memprofilefd = f
	}

	if os.Geteuid() == 0 {
		log.Fatalf("do not run honk as root")
	}
	err := os.Mkdir(dataDir+"/attachments", 0700)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		errx("can't create attachments directory: %s", err)
	}

	args := flag.Args()
	cmd := "run"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "init":
		commands["init"].callback(args)
	case "upgrade":
		commands["upgrade"].callback(args)
	case "version":
		commands["version"].callback(args)
	}
	db := opendatabase()
	dbversion := 0
	getconfig("dbversion", &dbversion)
	if dbversion != myVersion {
		if len(os.Getenv(envListener)) > 0 {
			upgradedb()
			db = opendatabase()
		} else {
			log.Fatal("incorrect database version. run upgrade.")
		}
	}
	getconfig("usefilestore", &storeTheFilesInTheFileSystem)
	getconfig("servermsg", &serverMsg)
	getconfig("aboutmsg", &aboutMsg)
	getconfig("loginmsg", &loginMsg)
	getconfig("servername", &serverName)
	getconfig("masqname", &masqName)
	if masqName == "" {
		masqName = serverName
	}
	serverPrefix = serverURL("/")
	getconfig("usersep", &userSep)
	getconfig("honksep", &honkSep)
	getconfig("devel", &develMode)
	if develMode {
		gogglesDoNothing()
	}
	getconfig("fasttimeout", &fastTimeout)
	getconfig("slowtimeout", &slowTimeout)
	getconfig("honkwindow", &honkwindow)
	honkwindow *= 24 * time.Hour
	getconfig("firstyear", &firstYear)
	getconfig("collectforwards", &collectForwards)
	getconfig("convertavif", &convertAVIF)
	if convertAVIF {
		stat := lazif.Load()
		if !stat.HasAVIF() {
			slog.Error("libavif could not be loaded")
			convertAVIF = false
		} else {
			getconfig("acceptavif", &acceptAVIF)
			if acceptAVIF {
				lazif.Register(stat)
			}
		}
	}

	prepareStatements(db)

	c, ok := commands[cmd]
	if !ok {
		errx("don't know about %q", cmd)
	}
	if c.nargs > 0 && len(args) != c.nargs {
		errx("incorrect arg count: %s", c.help2)
	}

	c.callback(args)
}

func takecontrol() {
	gonix.SetProcTitle("control")
	_, err := openListener()
	if err != nil {
		log.Fatal(err)
	}
	r, _, err := os.Pipe()
	if err != nil {
		log.Panicf("can't pipe: %s", err)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	for {
		server := runWebServer(r)
		backend := runBackendServer(r)
		<-sig
		server.Process.Signal(syscall.SIGTERM)
		backend.Process.Signal(syscall.SIGTERM)
		go func() {
			server.Wait()
			backend.Wait()
		}()
		slog.Info("restarting...")
	}
}
