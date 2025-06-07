//go:build openbsd || darwin || freebsd || netbsd
// +build openbsd darwin freebsd netbsd

package gonix

import (
	"os"
	"syscall"
)

type xWatcher struct {
	files map[string]*os.File
	kq    int
}

func newWatcher() (xWatcher, error) {
	kq, err := syscall.Kqueue()
	return xWatcher{files: make(map[string]*os.File), kq: kq}, err
}

func (x *xWatcher) reopen(name string) (*os.File, error) {
	if prev, ok := x.files[name]; ok {
		delete(x.files, name)
		prev.Close()
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	x.files[name] = file
	return file, nil
}

func kwatch(kq int, fd int, flags uint32) error {
	var kev [1]syscall.Kevent_t
	syscall.SetKevent(&kev[0], fd, syscall.EVFILT_VNODE, syscall.EV_ADD|syscall.EV_CLEAR)
	kev[0].Fflags = flags
	_, err := syscall.Kevent(kq, kev[:], nil, nil)
	return err
}

func (x *xWatcher) watchDirectory(name string) error {
	file, err := x.reopen(name)
	if err == nil {
		err = kwatch(x.kq, int(file.Fd()), syscall.NOTE_WRITE)
	}
	return err
}

func (x *xWatcher) watchFile(name string) error {
	file, err := x.reopen(name)
	if err == nil {
		err = kwatch(x.kq, int(file.Fd()), syscall.NOTE_WRITE|syscall.NOTE_DELETE)
	}
	return err
}

func (x *xWatcher) waitForChange() error {
	var kev [1]syscall.Kevent_t
	_, err := syscall.Kevent(x.kq, nil, kev[:], nil)
	return err
}
