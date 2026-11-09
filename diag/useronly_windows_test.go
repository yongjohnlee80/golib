//go:build windows

package diag

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// userOnly fails unless only the user can open the socket at path: its DACL protected, every ACE in
// it an allow for the user's SID, and at least one.
func userOnly(t *testing.T, path string) {
	t.Helper()
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("opening the socket %s: %v", path, err)
	}
	defer windows.CloseHandle(h)
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("the socket's security: %v", err)
	}
	if ctl, _, err := sd.Control(); err != nil || ctl&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("the socket's DACL is not protected (%v, %v): its folder could pass others down to it", ctl, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("the socket has no DACL (%v): anyone may open it", err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if dacl.AceCount == 0 {
		t.Fatal("the socket's DACL grants nobody")
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !sid.Equals(user.User.Sid) {
			t.Errorf("the socket's ACE %d (type %d) is for %s, not only the user %s", i, ace.Header.AceType, sid, user.User.Sid)
		}
	}
}
