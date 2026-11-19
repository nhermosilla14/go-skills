//go:build windows

package install

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const (
	// fsctlSetReparsePoint tags a directory as a mount point, which is how Windows gets a
	// directory junction without the symlink privilege.
	fsctlSetReparsePoint    = 0x000900A4
	ioReparseTagMountPoint  = 0xA0000003
	mountPointPathBufferOff = 20 // 8-byte header plus 12 bytes of offset, length and flags
)

func platformLink(link, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	return createJunction(link, abs)
}

// createJunction creates link as a junction pointing at the absolute target. The caller removes
// link first and is responsible for the copy fallback when junction creation is not permitted.
func createJunction(link, target string) error {
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	if err := setMountPoint(link, target); err != nil {
		_ = os.Remove(link)
		return err
	}
	return nil
}

func setMountPoint(link, target string) error {
	// Substitute names use the NT namespace; print names are what Explorer shows.
	sub, err := windows.UTF16FromString(`\??\` + target)
	if err != nil {
		return err
	}
	print, err := windows.UTF16FromString(target)
	if err != nil {
		return err
	}
	subBytes := len(sub) * 2
	printBytes := len(print) * 2
	pathBytes := subBytes + printBytes
	if mountPointPathBufferOff+pathBytes > 0xFFFF {
		return fmt.Errorf("junction path is too long: %s", target)
	}
	buf := make([]byte, mountPointPathBufferOff+pathBytes)

	binary.LittleEndian.PutUint32(buf[0:], ioReparseTagMountPoint)
	binary.LittleEndian.PutUint16(buf[4:], uint16(12+pathBytes))
	binary.LittleEndian.PutUint16(buf[8:], 0)                     // SubstituteNameOffset
	binary.LittleEndian.PutUint16(buf[10:], uint16(subBytes-2))   // SubstituteNameLength, no NUL
	binary.LittleEndian.PutUint16(buf[12:], uint16(subBytes))     // PrintNameOffset
	binary.LittleEndian.PutUint16(buf[14:], uint16(printBytes-2)) // PrintNameLength, no NUL
	binary.LittleEndian.PutUint32(buf[16:], 0)                    // Flags
	putUTF16(buf[mountPointPathBufferOff:], sub)
	putUTF16(buf[mountPointPathBufferOff+subBytes:], print)

	pathPtr, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(pathPtr, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var returned uint32
	if err := windows.DeviceIoControl(handle, fsctlSetReparsePoint, &buf[0], uint32(len(buf)),
		nil, 0, &returned, nil); err != nil {
		return fmt.Errorf("setting junction reparse point: %w", err)
	}
	return nil
}

// putUTF16 writes code units in little endian order into dst.
func putUTF16(dst []byte, units []uint16) {
	for i, u := range units {
		binary.LittleEndian.PutUint16(dst[i*2:], u)
	}
}
