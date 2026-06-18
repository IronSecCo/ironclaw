package main

import (
	"fmt"

	"github.com/IronSecCo/ironclaw/internal/host/catalog"
)

// cmdTools prints the built-in tool catalog so an operator can DISCOVER tool names
// (and what each does) before enabling them on an agent — no more guessing internal
// names. It reads the catalog package directly, so it works offline and always
// matches what the sandbox actually implements.
func cmdTools(_ string, args []string) error {
	if len(args) > 0 && args[0] != "list" && args[0] != "ls" {
		return fmt.Errorf("usage: tools [list]")
	}
	byCat := map[catalog.Category][]catalog.ToolInfo{}
	for _, t := range catalog.Tools() {
		byCat[t.Category] = append(byCat[t.Category], t)
	}
	fmt.Println("Built-in tools (enable with: ironctl agent create --tool <name>):")
	for _, cat := range catalog.CategoryOrder() {
		group := byCat[cat]
		if len(group) == 0 {
			continue
		}
		fmt.Printf("\n%s\n", string(cat))
		for _, t := range group {
			badge := ""
			switch {
			case t.Mandatory:
				badge = dim("  [always on]")
			case t.Egress:
				badge = dim("  [needs host approval]")
			}
			fmt.Printf("  %-26s %s%s\n", t.Name, t.Title, badge)
			fmt.Printf("  %-26s %s\n", "", dim(t.Description))
		}
	}
	fmt.Println()
	fmt.Println(dim("[always on] tools can't be disabled. [needs host approval] tools also require an approved egress host before they work."))
	return nil
}
