package provider

// GetCLIStatusSummary returns the current detection status for all built-in CLI runtimes.
func GetCLIStatusSummary() []CLIToolStatus {
	return DiscoverAvailableCLI()
}
