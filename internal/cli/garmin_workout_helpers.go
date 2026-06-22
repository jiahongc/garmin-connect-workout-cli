package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func isISODate(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func printJSONOrHuman(cmd *cobra.Command, flags *rootFlags, value map[string]any, human string) error {
	if flags.asJSON || flags.agent || !isTerminal(cmd.OutOrStdout()) {
		return printJSONFiltered(cmd.OutOrStdout(), value, flags)
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), human)
	return err
}
