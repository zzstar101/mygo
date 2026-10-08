//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"os"
	"runtime"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// Identifiers of the jump list's objects, from shobjidl.h.
var (
	iidICustomDestinationList = guid("6332debf-87b5-4670-90c0-5e57b408a49e") // ICustomDestinationList
	clsidDestinationList      = guid("77f10cf0-3db5-4966-b520-b7c54fd35ed6") // CLSID_DestinationList

	iidIObjectArray       = guid("92ca9dcd-5622-4bba-a805-5e9f541bd8c9") // IID_IObjectArray
	iidIObjectCollection  = guid("5632b1a4-e38a-400a-928a-d4cd63230295") // IID_IObjectCollection
	clsidObjectCollection = guid("2d3468c1-36a7-43b6-ac24-d3f02fd9607a") // CLSID_EnumerableObjectCollection
	iidIShellLinkW        = guid("000214f9-0000-0000-c000-000000000046") // IID_IShellLinkW
	clsidShellLink        = guid("00021401-0000-0000-c000-000000000046") // CLSID_ShellLink
	iidIPropertyStore     = guid("886d8eeb-8cf2-4446-8d02-cdba1dbdcf99") // IID_IPropertyStore
)

// Vtable indices, IUnknown (0, 1, 2) included.
const (
	cdlSetAppID            = 3
	cdlBeginList           = 4
	cdlAppendKnownCategory = 6
	cdlAddUserTasks        = 7
	cdlCommitList          = 8
	cdlAbortList           = 11

	// kdcRecent is KDC_RECENT, the shell's Recent category.
	kdcRecent = 2

	// vtLPWSTR is VT_LPWSTR, the type of the System.Title property.
	vtLPWSTR = 31

	ocAddObject = 5 // IObjectCollection

	slSetArguments    = 11 // IShellLinkW
	slSetIconLocation = 17
	slSetPath         = 20

	psSetValue = 6 // IPropertyStore
	psCommit   = 7
)

// pkeyTitle is PKEY_Title, System.Title: the label Windows shows for a jump
// list task, which the shell link's description is not.
var pkeyTitle = propertyKey{Format: guid("f29f85e0-4ff9-1068-ab91-08002b27b3d9"), ID: 2}

// SetJumpList commits the tasks of the app's jump list, replacing the list
// the shell shows.
func (a appController) SetJumpList(tasks []platform.JumpListTask) error {
	dl, err := coCreate(clsidDestinationList, iidICustomDestinationList)
	if err != nil {
		return err
	}
	defer release(dl)
	// The list belongs to the app's AppUserModelID, which Init set
	// explicitly; saying so again is what the shell reads first.
	if hr := comCall(dl, cdlSetAppID, uintptr(unsafe.Pointer(u16(a.b.appUserModelID)))); failed(hr) {
		return hresultError("ICustomDestinationList::SetAppID", hr)
	}
	var minSlots uint32
	var removed uintptr
	if hr := comCall(dl, cdlBeginList, uintptr(unsafe.Pointer(&minSlots)),
		uintptr(unsafe.Pointer(&iidIObjectArray)), uintptr(unsafe.Pointer(&removed))); failed(hr) {
		return hresultError("ICustomDestinationList::BeginList", hr)
	}
	release(removed) // the destinations the user removed; this design adds none
	// A custom list shows the Recent category, which AddRecentDocument
	// fills, only when it asks for it.
	if hr := comCall(dl, cdlAppendKnownCategory, kdcRecent); failed(hr) {
		comCall(dl, cdlAbortList)
		return hresultError("ICustomDestinationList::AppendKnownCategory", hr)
	}
	if len(tasks) > 0 {
		oc, err := coCreate(clsidObjectCollection, iidIObjectCollection)
		if err != nil {
			comCall(dl, cdlAbortList)
			return err
		}
		if err := addTasks(oc, tasks); err != nil {
			release(oc)
			comCall(dl, cdlAbortList)
			return err
		}
		if hr := comCall(dl, cdlAddUserTasks, oc); failed(hr) {
			release(oc)
			comCall(dl, cdlAbortList)
			return hresultError("ICustomDestinationList::AddUserTasks", hr)
		}
		release(oc)
	}
	if hr := comCall(dl, cdlCommitList); failed(hr) {
		comCall(dl, cdlAbortList)
		return hresultError("ICustomDestinationList::CommitList", hr)
	}
	return nil
}

// addTasks fills the object collection with one shell link per task.
func addTasks(oc uintptr, tasks []platform.JumpListTask) error {
	for _, t := range tasks {
		path := t.Path
		if path == "" {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			path = exe
		}
		sl, err := coCreate(clsidShellLink, iidIShellLinkW)
		if err != nil {
			return err
		}
		if hr := comCall(sl, slSetPath, uintptr(unsafe.Pointer(u16(path)))); failed(hr) {
			release(sl)
			return hresultError("IShellLinkW::SetPath", hr)
		}
		// AddUserTasks only shows links that declare arguments, even empty ones.
		if hr := comCall(sl, slSetArguments, uintptr(unsafe.Pointer(u16(t.Args)))); failed(hr) {
			release(sl)
			return hresultError("IShellLinkW::SetArguments", hr)
		}
		if t.IconPath != "" {
			if hr := comCall(sl, slSetIconLocation, uintptr(unsafe.Pointer(u16(t.IconPath))), uintptr(t.IconIndex)); failed(hr) {
				release(sl)
				return hresultError("IShellLinkW::SetIconLocation", hr)
			}
		}
		if err := setTitle(sl, t.Title); err != nil {
			release(sl)
			return err
		}
		if hr := comCall(oc, ocAddObject, sl); failed(hr) {
			release(sl)
			return hresultError("IObjectCollection::AddObject", hr)
		}
		release(sl) // the collection holds its own reference
	}
	return nil
}

// setTitle sets the System.Title property of a shell link, which is the
// label Windows shows for a jump list task.
func setTitle(sl uintptr, title string) error {
	ps := queryInterface(sl, &iidIPropertyStore)
	if ps == 0 {
		return errors.New("mygo: the shell link has no property store")
	}
	defer release(ps)
	// System.Title is a VT_LPWSTR string. SetValue copies the value, so the
	// string only has to outlive the call.
	s := utf16z(title)
	v := variant{VT: vtLPWSTR, Val: uint64(uintptr(unsafe.Pointer(&s[0])))}
	hr := comCall(ps, psSetValue, uintptr(unsafe.Pointer(&pkeyTitle)), uintptr(unsafe.Pointer(&v)))
	runtime.KeepAlive(s)
	if failed(hr) {
		return hresultError("IPropertyStore::SetValue", hr)
	}
	if hr := comCall(ps, psCommit); failed(hr) {
		return hresultError("IPropertyStore::Commit", hr)
	}
	return nil
}

// coCreate instantiates a class and returns its iid interface, as
// taskbar.go does for ITaskbarList3.
func coCreate(clsid, iid GUID) (uintptr, error) {
	var obj uintptr
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsid)), 0,
		clsctxInprocServer, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&obj))); failed(hr) {
		return 0, hresultError("CoCreateInstance", hr)
	}
	return obj, nil
}
