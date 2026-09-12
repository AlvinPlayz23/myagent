package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/AlvinPlayz23/myagent/internal/plugin"
)

// runPlugin implements `myagent plugin list` / `myagent plugin validate`.
func runPlugin(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("usage: myagent plugin <list|validate [path]>")
	}
	cwd, _ := os.Getwd()
	switch argv[0] {
	case "list":
		b := plugin.Load(cwd, false)
		fmt.Println(b.Summary())
		for _, w := range b.Warnings {
			fmt.Fprintln(os.Stderr, "warning: "+w)
		}
		if len(b.Profiles) > 0 {
			fmt.Println("profiles:")
			for _, p := range b.Profiles {
				fmt.Printf("  %-21s %s\n", p.Name, p.Description)
			}
		}
		return nil
	case "validate":
		if len(argv) >= 2 {
			data, err := os.ReadFile(argv[1])
			if err != nil {
				return fmt.Errorf("read %s: %w", argv[1], err)
			}
			var f plugin.File
			if err := json.Unmarshal(data, &f); err != nil {
				return fmt.Errorf("invalid JSON: %v", err)
			}
			b := plugin.LoadBytes(data, nil)
			if len(b.Warnings) > 0 {
				for _, w := range b.Warnings {
					fmt.Println("warning: " + w)
				}
				return fmt.Errorf("plugins.json has %d warning(s)", len(b.Warnings))
			}
			fmt.Println("valid: " + b.Summary())
			return nil
		}
		b := plugin.Load(cwd, false)
		if len(b.Warnings) > 0 {
			for _, w := range b.Warnings {
				fmt.Println("warning: " + w)
			}
			return fmt.Errorf("plugins.json has %d warning(s)", len(b.Warnings))
		}
		fmt.Println("valid: " + b.Summary())
		return nil
	default:
		return fmt.Errorf("usage: myagent plugin <list|validate [path]>")
	}
}
