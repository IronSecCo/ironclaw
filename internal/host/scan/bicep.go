package scan

// SpecsFromBicepARM grades an Azure Bicep template that has already been compiled to
// ARM JSON. Bicep is a domain-specific language that transpiles 1:1 to an ARM
// deployment template (`az bicep build` / the standalone `bicep build` emit exactly
// the ARM JSON that ARM itself consumes), so a Bicep file that declares a
// Microsoft.ContainerInstance/containerGroups resource compiles to the SAME ARM
// document the --azure path already grades. This is therefore a deliberately thin
// seam over SpecsFromAzure: it takes the compiled ARM bytes (the CLI wrapper runs the
// bicep compiler — the I/O) and returns one graded Spec per ACI container, with the
// identical managed-runtime model, ARM PascalCase case-insensitive decode, and
// "[...]" expression fail-closed handling.
//
// Keeping it a named entry point (rather than calling SpecsFromAzure directly from the
// CLI) gives the --bicep mode a stable seam a parity test can pin against the sibling
// --azure mode: the same compiled ARM must grade identically through both.
func SpecsFromBicepARM(compiledARM []byte) ([]Spec, bool, error) {
	return SpecsFromAzure(compiledARM)
}

// AggregateBicep folds the per-container specs from one or more compiled-Bicep ACI
// containerGroups into a single Report. It is the AggregateAzure weakest-container
// rollup (a group is only as isolated as its most-exposed container) with the Source
// re-labelled "bicep" so the report names the input mode the user actually ran. It is
// pure; the caller injects Version/GeneratedAt.
func AggregateBicep(specs []Spec, target string) (Report, Spec, error) {
	report, worst, err := AggregateAzure(specs, target)
	if err != nil {
		return Report{}, Spec{}, err
	}
	report.Source = "bicep"
	report.Notes = append([]string{
		"input compiled from Azure Bicep to ARM JSON (az bicep build / bicep build); grading is identical to the --azure ACI path once compiled",
	}, report.Notes...)
	return report, worst, nil
}
