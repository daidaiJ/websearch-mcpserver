package privatefile

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestOpenNewProtectsCredentialAtCreation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	f, err := OpenNew(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := info.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("inherits parent access")
	}
	acl, _, err := info.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatal("expected one explicit permission")
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !sid.Equals(user.User.Sid) || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		t.Fatal("unexpected access principal")
	}
	if second, err := OpenNew(path); err == nil {
		second.Close()
		t.Fatal("existing file overwritten")
	}
	if _, err := f.Write([]byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "synthetic" {
		t.Fatal("private file unavailable to creator")
	}
}
