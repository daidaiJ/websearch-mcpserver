package privatefile

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// OpenNew exclusively creates a file with a protected DACL for the current account.
func OpenNew(path string) (*os.File, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	attrs := windows.SecurityAttributes{SecurityDescriptor: sd}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ, &attrs, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "create", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(handle), path)
	// Some filesystems ignore security descriptors. Fail before the first write.
	applied, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err == nil {
		var control windows.SECURITY_DESCRIPTOR_CONTROL
		control, _, err = applied.Control()
		if err == nil && control&windows.SE_DACL_PROTECTED == 0 {
			err = errors.New("filesystem does not protect private file permissions")
		}
	}
	if err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	return file, nil
}
