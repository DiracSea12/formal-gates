package validate

// HarnessResolveSnapshot exposes only the existing read-only 3.5c primitive.
// It does not expose legacy RunState or any workflow writer.
func HarnessResolveSnapshot(root string) (string, error) {
	return resolveNativeSnapshot(root, "git")
}
