//go:build windows

package validate

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func replaceInstallBinary(completed, target, retired string) error {
	if err := os.Rename(target, retired); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(completed, target)
}

func removeInstallBinary(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func retireInstallBinary(path, retired string) error {
	err := os.Rename(path, retired)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func installBinaryInUse(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

func deferOuterCleanup(path string, journal outerInstallJournal) error {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		return err
	}
	request, err := json.Marshal(map[string]any{
		"journal": path, "token": journal.Token, "pid": os.Getpid(),
		"error":   filepath.Join(journal.TransactionRoot, installCleanupErrorName),
		"created": strconv.FormatUint(uint64(created.HighDateTime)<<32|uint64(created.LowDateTime), 10),
		"locks":   []string{installLockPath(journal.RegistryPath), registryLockPath(journal.RegistryPath)},
	})
	if err != nil {
		return err
	}
	script := "$request = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('" + base64.StdEncoding.EncodeToString(request) + "')) | ConvertFrom-Json\n" + installCleanupScript
	encoded := utf16.Encode([]rune(script))
	bytes := make([]byte, len(encoded)*2)
	for index, value := range encoded {
		binary.LittleEndian.PutUint16(bytes[index*2:], value)
	}
	command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(bytes))
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

const installCleanupErrorName = "cleanup.error.log"

const installCleanupScript = `
$ErrorActionPreference = 'Stop'
function Get-Cause($failure) {
  $cause = $failure.Exception
  while ($cause.InnerException) { $cause = $cause.InnerException }
  return $cause
}
function Get-Journal {
  if (-not [IO.File]::Exists($request.journal)) { return $null }
  $current = [IO.File]::ReadAllText($request.journal) | ConvertFrom-Json
  if ([string]$current.token -cne [string]$request.token -or $current.phase -cne 'committed') { return $null }
  return $current
}
while ($true) {
  $held = [Collections.Generic.List[IO.FileStream]]::new()
  $retiring = [Collections.Generic.List[IDisposable]]::new()
  $busy = $false
  try {
    try {
      $creator = [Diagnostics.Process]::GetProcessById($request.pid)
      if ($creator.StartTime.ToFileTimeUtc().ToString() -ceq $request.created) { $creator.WaitForExit() }
      $creator.Dispose()
    } catch {
      if (-not ((Get-Cause $_) -is [ArgumentException])) { throw }
    }
    if (-not (Get-Journal)) { exit 0 }
    foreach ($path in $request.locks) {
      try { $file = [IO.File]::Open($path, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, ([IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete)) }
      catch {
        if (-not (Get-Journal)) { exit 0 }
        throw
      }
      $held.Add($file)
      while ($true) {
        try { $file.Lock(0, 1); break }
        catch {
          if (((Get-Cause $_).HResult -band 65535) -ne 33) { throw }
          Start-Sleep -Milliseconds 25
        }
      }
    }
    $journal = Get-Journal
    if (-not $journal) { exit 0 }
    foreach ($path in $journal.staged) {
      if ([IO.Directory]::Exists($path)) { [IO.Directory]::Delete($path, $true); continue }
      try { [IO.File]::Delete($path) }
      catch {
        $code = (Get-Cause $_).HResult -band 65535
        if ($code -ne 5 -and $code -ne 32) { throw }
        $busy = $true
      }
    }
    if (-not $busy) {
      if ([IO.Directory]::Exists($journal.transactionRoot)) { [IO.Directory]::Delete($journal.transactionRoot, $true) }
      $unused = -not [IO.File]::Exists($journal.registryPath)
      if (-not $unused) {
        $registry = [IO.File]::ReadAllText($journal.registryPath) | ConvertFrom-Json
        $unused = @($registry.records).Count -eq 0
      }
      if ($unused) {
        Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using Microsoft.Win32.SafeHandles;
public static class InstallLock {
  [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
  static extern SafeFileHandle CreateFileW(string path, uint access, uint share, IntPtr security, uint mode, uint attributes, IntPtr template);
  [DllImport("kernel32.dll", SetLastError = true)]
  static extern bool SetFileInformationByHandle(SafeFileHandle handle, uint kind, ref uint flags, uint size);
  public static SafeFileHandle PrepareRetire(string path) {
    var file = CreateFileW(path, 0x10000, 7, IntPtr.Zero, 3, 0x80, IntPtr.Zero);
    if (file.IsInvalid) throw new Win32Exception(Marshal.GetLastWin32Error());
    uint flags = 3;
    if (SetFileInformationByHandle(file, 21, ref flags, 4)) return file;
    int error = Marshal.GetLastWin32Error();
    file.Dispose();
    throw new Win32Exception(error);
  }
}
'@
        foreach ($path in $request.locks) { $retiring.Add([InstallLock]::PrepareRetire($path)) }
      }
      [IO.File]::Delete($request.journal)
    }
  } catch {
    $failure = $_
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($request.error)) | Out-Null
    [IO.File]::WriteAllText($request.error, ($failure | Out-String))
    exit 1
  } finally {
    foreach ($file in $retiring) { $file.Dispose() }
    foreach ($file in $held) { $file.Dispose() }
  }
  if (-not $busy) { exit 0 }
  Start-Sleep -Milliseconds 100
}
`
