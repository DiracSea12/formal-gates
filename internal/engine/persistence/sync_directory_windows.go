//go:build windows

package persistence

// Windows does not support syncing a directory handle.  The replacement file
// itself is still flushed before the atomic rename; the directory durability
// step is therefore a platform no-op until a Windows-specific durable replace
// primitive is introduced.
func syncDirectory(string) error { return nil }
