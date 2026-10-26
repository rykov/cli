package cli

import (
	"github.com/gemfury/cli/api"
	"github.com/gemfury/cli/pkg/terminal"
	"github.com/spf13/cobra"

	"context"
	"fmt"
)

// NewCmdPackages creates the "packages" command
func NewCmdPackages() *cobra.Command {
	return jsonCommand(&cobra.Command{
		Use:     "packages",
		Aliases: []string{"list"},
		Short:   "List packages in this account",
		Example: `  fury packages
  fury packages --account my-org`,
		Args: noArgs,
		RunE: listPackages,
	})
}

// NewCmdVersions creates the "versions" command
func NewCmdVersions() *cobra.Command {
	return jsonCommand(&cobra.Command{
		Use:     "versions PACKAGE",
		Short:   "List versions for a package",
		Example: `  fury versions package`,
		Args:    usageArgs(cobra.ExactArgs(1), "Please specify exactly one package"),
		RunE:    listVersions,
	})
}

func listPackages(cmd *cobra.Command, args []string) error {
	cc := cmd.Context()
	c, err := newAPIClient(cc)
	if err != nil {
		return err
	}

	packages, err := fetchAll[*api.Package](cc, c.Packages)
	return printListing(cmd, packages, err, "No packages found in this account", func(term *terminal.UI) {
		term.Infof("\n*** GEMFURY PACKAGES ***\n\n")
		rows := [][]string{{"name", "kind", "version", "privacy"}}
		for _, p := range packages {
			rows = append(rows, []string{p.Name, p.Kind, p.DisplayVersion(), p.Privacy()})
		}
		printTable(term.IOOut(), rows)
	})
}

func listVersions(cmd *cobra.Command, args []string) error {
	cc := cmd.Context()
	c, err := newAPIClient(cc)
	if err != nil {
		return err
	}

	versions, err := fetchAll[*api.Version](cc, func(cc context.Context, pageReq *api.PaginationRequest) (*api.VersionsResponse, error) {
		return c.PackageVersions(cc, args[0], pageReq)
	})
	err = about("Package", args[0], err)

	empty := fmt.Sprintf("No versions found for package %q", args[0])
	return printListing(cmd, versions, err, empty, func(term *terminal.UI) {
		term.Infof("\n*** %s versions ***\n\n", args[0])
		termPrintVersions(term, versions)
	})
}

func termPrintVersions(term *terminal.UI, versions []*api.Version) {
	rows := [][]string{{"version", "created_by", "created_at", "kind", "filename"}}
	loc, tty := term.TimeLocation(), term.IsOutTTY()
	for _, v := range versions {
		createdAt := timeString(v.CreatedAt, loc, tty)
		rows = append(rows, []string{v.Version, v.DisplayCreatedBy(), createdAt, v.DisplayKind(), v.Filename})
	}
	printTable(term.IOOut(), rows)
}
