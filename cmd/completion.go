package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for git-agent.

To load completions:

Bash:
  $ source <(git-agent completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ git-agent completion bash > /etc/bash_completion.d/git-agent
  # macOS:
  $ git-agent completion bash > $(brew --prefix)/etc/bash_completion.d/git-agent

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ git-agent completion zsh > "${fpath[1]}/_git-agent"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ git-agent completion fish | source

  # To load completions for each session, execute once:
  $ git-agent completion fish > ~/.config/fish/completions/git-agent.fish

PowerShell:
  PS> git-agent completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, add the output to your profile:
  PS> git-agent completion powershell >> $PROFILE
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			return cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
