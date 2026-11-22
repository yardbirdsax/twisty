package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newUpdateBackfillCmd is `twisty update backfill-stamps`, the one-off
// migration that seeds upstream stamps from PBF modification times.
func newUpdateBackfillCmd(loc *UpdateLocations) *cobra.Command {
	return &cobra.Command{
		Use:   "backfill-stamps",
		Short: "One-off: seed missing upstream stamps from PBF modification times",
		Long: "For installs that predate upstream stamps: records each loaded region's " +
			"PBF modification time as its stamp, so the first `update` skips regions " +
			"that are already current. Existing stamps are never overwritten.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			seeded, err := BackfillUpstreamStamps(dataRootFor(*loc))
			if err != nil {
				return err
			}
			out := cmd.ErrOrStderr()
			if len(seeded) == 0 {
				fmt.Fprintln(out, "No stamps to seed.")
			}
			for _, r := range seeded {
				fmt.Fprintf(out, "Seeded stamp for %s\n", r)
			}
			return nil
		},
	}
}
