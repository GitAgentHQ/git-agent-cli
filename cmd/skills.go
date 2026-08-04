package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gitagenthq/git-agent/infrastructure/skills"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage embedded usage documents",
	Long: `Print git-agent's own usage documentation, embedded in the binary so it
always matches the installed version. The repository's skill stub
(skills/using-git-agent/SKILL.md) delegates to git-agent skills get core.`,
}

var skillsGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Print an embedded usage document",
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires exactly one document name; see `git-agent skills list`")
		}
		return nil
	},
	RunE: runSkillsGet,
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available usage documents",
	RunE:  runSkillsList,
}

func runSkillsGet(cmd *cobra.Command, args []string) error {
	doc, err := skills.Get(args[0])
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), doc)
	return nil
}

func runSkillsList(cmd *cobra.Command, _ []string) error {
	for _, name := range skills.Names() {
		fmt.Fprintln(cmd.OutOrStdout(), name)
	}
	return nil
}

func init() {
	skillsCmd.AddCommand(skillsGetCmd, skillsListCmd)
	rootCmd.AddCommand(skillsCmd)
}
