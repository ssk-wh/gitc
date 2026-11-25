//go:build windows

package platform

import (
	"encoding/base64"
	"errors"
	"os/exec"
	"strings"
	"unicode/utf16"
)

// 通过 PowerShell 调用 IFileOpenDialog(FOS_PICKFOLDERS)，得到 Windows 11 自带的
// “打开文件夹”对话框（带“选择文件夹(S)”按钮），而不是保存文件框或旧版文件夹树。
const pickerScript = `$ErrorActionPreference='Stop'
[Console]::OutputEncoding=[System.Text.Encoding]::UTF8
$code=@'
using System;
using System.Runtime.InteropServices;

[ComImport, Guid("42f85136-db7e-439c-85f1-e4075d135fc8"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IFileDialog {
    [PreserveSig] int Show(IntPtr parent);
    [PreserveSig] int SetFileTypes();
    [PreserveSig] int SetFileTypeIndex();
    [PreserveSig] int GetFileTypeIndex();
    [PreserveSig] int Advise();
    [PreserveSig] int Unadvise();
    [PreserveSig] int SetOptions(uint fos);
    [PreserveSig] int GetOptions();
    [PreserveSig] int SetDefaultFolder();
    [PreserveSig] int SetFolder(IShellItem psi);
    [PreserveSig] int GetFolder();
    [PreserveSig] int GetCurrentSelection();
    [PreserveSig] int SetFileName();
    [PreserveSig] int GetFileName();
    [PreserveSig] int SetTitle([MarshalAs(UnmanagedType.LPWStr)] string title);
    [PreserveSig] int SetOkButtonLabel();
    [PreserveSig] int SetFileNameLabel();
    [PreserveSig] int GetResult(out IShellItem item);
    [PreserveSig] int AddPlace();
    [PreserveSig] int SetDefaultExtension();
    [PreserveSig] int Close();
    [PreserveSig] int SetClientGuid();
    [PreserveSig] int ClearClientData();
    [PreserveSig] int SetFilter();
}

[ComImport, Guid("d57c7288-d4ad-4768-be02-9d969532d960"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IFileOpenDialog : IFileDialog {
    [PreserveSig] int GetResults();
    [PreserveSig] int GetSelectedItems();
}

[ComImport, Guid("43826d1e-e718-42ee-bc55-a1e261c37bfe"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IShellItem {
    [PreserveSig] int BindToHandler();
    [PreserveSig] int GetParent();
    [PreserveSig] int GetDisplayName(uint sigdn, out IntPtr name);
    [PreserveSig] int GetAttributes();
    [PreserveSig] int Compare();
}

[ComImport, Guid("DC1C5A9C-E88A-4dde-A5A1-60F82A20AEF7")]
class FileOpenDialogRCW { }

public static class GitcFolderPicker {
    [DllImport("shell32.dll", CharSet=CharSet.Unicode)]
    static extern int SHCreateItemFromParsingName(string path, IntPtr pbc, ref Guid riid, out IShellItem item);

    public static string Pick(string title, string initial) {
        var dlg = (IFileOpenDialog)new FileOpenDialogRCW();
        const uint FOS_PICKFOLDERS = 0x20;
        const uint FOS_FORCEFILESYSTEM = 0x40;
        const uint FOS_PATHMUSTEXIST = 0x800;
        dlg.SetOptions(FOS_PICKFOLDERS | FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST);
        dlg.SetTitle(title);
        if (!string.IsNullOrEmpty(initial)) {
            var iid = new Guid("43826d1e-e718-42ee-bc55-a1e261c37bfe");
            IShellItem folder;
            if (SHCreateItemFromParsingName(initial, IntPtr.Zero, ref iid, out folder) == 0) {
                dlg.SetFolder(folder);
            }
        }
        if (dlg.Show(IntPtr.Zero) != 0) return null;
        IShellItem result;
        if (dlg.GetResult(out result) != 0) return null;
        IntPtr ptr;
        if (result.GetDisplayName(0x80058000, out ptr) != 0) return null;
        try { return Marshal.PtrToStringUni(ptr); } finally { Marshal.FreeCoTaskMem(ptr); }
    }
}
'@
Add-Type -TypeDefinition $code -Language CSharp
$p=[GitcFolderPicker]::Pick('选择项目目录',[Environment]::GetFolderPath('UserProfile'))
if($p){[Console]::Out.Write($p)}
`

func pickDirectory() (string, error) {
	encoded := base64.StdEncoding.EncodeToString(utf16LE(pickerScript))
	out, err := exec.Command("powershell", "-NoProfile", "-STA", "-WindowStyle", "Hidden", "-EncodedCommand", encoded).Output()
	if err != nil {
		var ee *exec.Error
		if errors.As(err, &ee) {
			return "", ErrUnavailable
		}
		return "", ErrCanceled
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", ErrCanceled
	}
	return p, nil
}

func utf16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[i*2] = byte(v)
		b[i*2+1] = byte(v >> 8)
	}
	return b
}
