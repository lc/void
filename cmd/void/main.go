// Command `void` is the end-user CLI for the Void daemon.
//
// Void is a site blocking tool that allows users to block distracting websites.
// The CLI communicates with a background daemon that manages the actual blocking
// via macOS packet filter (pf).
//
// Usage:
//
//	void block <domain> [<duration>]  - Block a domain (permanently or temporarily)
//	void list                         - List all currently blocked domains
//
// Examples:
//
//	void block facebook.com           - Block facebook.com permanently (with confirmation)
//	void block twitter.com 2h         - Block twitter.com for 2 hours
//	void block youtube.com 30m        - Block youtube.com for 30 minutes
//	void list                         - Show all currently blocked domains
//
// Durations use Go duration syntax ("1h", "30m", "2h30m", etc.). Omitting duration
// makes the block permanent, but requires confirmation.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"

	"github.com/lc/void/internal/buildinfo"
	"github.com/lc/void/internal/config"
	"github.com/lc/void/pkg/client"
)

func main() {
	cfg, err := config.New().Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	cli := client.New(cfg.Socket.Path)

	root := &cobra.Command{
		Use:   "void",
		Short: "Void domain-block CLI",
		Long: `Void is a site blocking tool that allows users to block distracting websites.
The tool uses macOS packet filter (pf) to block domains at the network level.`,
	}
	// ---- version command ----
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Long:  `Show version information for the Void CLI and daemon.`,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("version: %s\n", buildinfo.Version)
			fmt.Printf("commit: %s\n", buildinfo.Commit)
		},
	}
	// ---- block command ----
	blockCmd := &cobra.Command{
		Use:   "block <domain> [duration]",
		Short: "Block a domain (permanent unless duration provided)",
		Long: `Block a domain either permanently or for a specified duration.
If no duration is provided, the domain will be blocked permanently
(requires confirmation).

Examples:
  void block facebook.com           Block facebook.com permanently (with confirmation)
  void block twitter.com 2h         Block twitter.com for 2 hours
  void block youtube.com 30m        Block youtube.com for 30 minutes

Durations use Go duration syntax (e.g., "30s", "5m", "2h", "1h30m").`,
		Example: "void block facebook.com 2h",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			domain := args[0]
			var dur time.Duration
			if len(args) == 2 {
				var err error
				dur, err = time.ParseDuration(args[1])
				if err != nil {
					return fmt.Errorf("invalid duration: %w", err)
				}
			}
			if dur == 0 {
				color.New(color.FgHiRed, color.Bold).Print("WARNING: ")
				color.New(color.FgYellow).Printf("You are about to permanently block ")
				color.New(color.FgHiYellow, color.Bold).Printf("%s\n", domain)
				color.New(color.FgYellow).Println("This will block the domain until explicitly unblocked.")
				color.New(color.FgHiWhite).Print("Are you sure you want to proceed? (y/yes/n/no): ")

				var response string
				_, err := fmt.Scanln(&response)
				if err != nil {
					return fmt.Errorf("failed to read input: %w", err)
				}

				// Accept various forms of "yes"
				response = strings.ToLower(response)
				if response != "y" && response != "yes" {
					return fmt.Errorf("operation aborted")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := cli.Block(ctx, domain, dur); err != nil {
				return err
			}

			if dur == 0 {
				color.New(color.FgGreen, color.Bold).Printf("✓ Successfully blocked ")
				color.New(color.FgHiGreen, color.Bold).Printf("%s ", domain)
				color.New(color.FgGreen, color.Bold).Println("permanently")
				return nil
			}
			color.New(color.FgGreen, color.Bold).Printf("✓ Successfully blocked ")
			color.New(color.FgHiGreen, color.Bold).Printf("%s ", domain)
			color.New(color.FgGreen, color.Bold).Printf("for ")
			color.New(color.FgHiYellow, color.Bold).Printf("%s\n", dur.String())

			return nil
		},
	}

	// ---- list command ----
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List currently active rules",
		Long: `List all currently active domain blocking rules.
Shows domain, rule ID, whether the rule is permanent, and when it expires (if temporary).`,
		Example: "void list",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			rules, err := cli.Rules(ctx)
			if err != nil {
				return err
			}
			if len(rules) == 0 {
				color.Yellow("No active blocking rules found.")
				return nil
			}

			// Create a new table
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Rule ID", "Domain", "Permanent", "Expires"})
			table.SetHeaderColor(
				tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiCyanColor},
				tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiCyanColor},
				tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiCyanColor},
				tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiCyanColor},
			)
			table.SetBorder(false)
			table.SetColumnColor(
				tablewriter.Colors{tablewriter.FgHiWhiteColor},
				tablewriter.Colors{tablewriter.FgGreenColor},
				tablewriter.Colors{tablewriter.FgYellowColor},
				tablewriter.Colors{tablewriter.FgHiWhiteColor},
			)

			// Add data to the table
			for _, r := range rules {
				expires := "N/A"
				if !r.Permanent {
					expires = r.Expires.Format(time.RFC3339)
				}

				permanent := "No"
				if r.Permanent {
					permanent = "Yes"
				}

				table.Append([]string{r.ID, r.Domain, permanent, expires})
			}

			color.New(color.Bold).Println("ACTIVE BLOCKING RULES:")
			table.Render()
			return nil
		},
	}

	root.AddCommand(blockCmd, listCmd, versionCmd)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
