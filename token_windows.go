//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credentialTypeGeneric         = 1
	credentialPersistLocalMachine = 2
)

type windowsCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

func saveSecureToken(_ string, token oauthToken) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(tokenTarget)
	if err != nil {
		return err
	}
	username, _ := windows.UTF16PtrFromString("Google Drive")
	credential := windowsCredential{
		Type: credentialTypeGeneric, TargetName: target, Persist: credentialPersistLocalMachine,
		CredentialBlobSize: uint32(len(data)), UserName: username,
	}
	if len(data) > 0 {
		credential.CredentialBlob = &data[0]
	}
	result, _, callErr := procCredWrite.Call(uintptr(unsafe.Pointer(&credential)), 0)
	if result == 0 {
		return callErr
	}
	return nil
}

func loadSecureToken(_ string) (oauthToken, error) {
	target, err := windows.UTF16PtrFromString(tokenTarget)
	if err != nil {
		return oauthToken{}, err
	}
	var credential *windowsCredential
	result, _, callErr := procCredRead.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0, uintptr(unsafe.Pointer(&credential)))
	if result == 0 {
		if errors.Is(callErr, syscall.Errno(1168)) {
			return oauthToken{}, os.ErrNotExist
		}
		return oauthToken{}, callErr
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential == nil || credential.CredentialBlobSize == 0 || credential.CredentialBlob == nil {
		return oauthToken{}, os.ErrNotExist
	}
	data := unsafe.Slice(credential.CredentialBlob, int(credential.CredentialBlobSize))
	var token oauthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return oauthToken{}, err
	}
	return token, nil
}

func deleteSecureToken(_ string) error {
	target, err := windows.UTF16PtrFromString(tokenTarget)
	if err != nil {
		return err
	}
	result, _, callErr := procCredDelete.Call(uintptr(unsafe.Pointer(target)), credentialTypeGeneric, 0)
	if result == 0 && !errors.Is(callErr, syscall.Errno(1168)) {
		return callErr
	}
	return nil
}
