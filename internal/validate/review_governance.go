package validate

func findingStableID(finding Finding) string {
	copy := finding
	copy.ID = ""
	return "finding-" + shortDigest(digestValue(copy))
}
