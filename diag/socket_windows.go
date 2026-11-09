//go:build windows

package diag

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// userOnlyDACL is a DACL granting the user of this process alone full access, inherited by files
// and folders created under one that carries it.
func userOnlyDACL() (*windows.ACL, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	return dacl, err
}

// restrictBindDir gives dir a protected DACL granting the user alone, which the socket bound in it
// inherits from its creation. Windows reads a socket's access from its ACL, not from mode bits.
func restrictBindDir(dir string) error {
	dacl, err := userOnlyDACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// restrictSocket sets the user-only DACL on the socket itself, protected, so it keeps it wherever it
// is renamed to and whatever its folder later passes down. The socket is a reparse point, opened as
// one: opened through, it cannot be.
func restrictSocket(path string) error {
	dacl, err := userOnlyDACL()
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.WRITE_DAC|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// connRefused is whether a dial found a socket nothing serves: Winsock's own refusal, which is not
// syscall.ECONNREFUSED there.
func connRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, syscall.ECONNREFUSED)
}
