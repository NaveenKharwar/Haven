package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

const defaultDataDir = "/opt/haven"

type serverRunner func(addr, dataDir string)

type cliStyle struct {
	color bool
}

func (s cliStyle) bold(value string) string {
	if !s.color {
		return value
	}
	return "\x1b[1m" + value + "\x1b[0m"
}

func (s cliStyle) accent(value string) string {
	if !s.color {
		return value
	}
	return "\x1b[38;5;208m" + value + "\x1b[0m"
}

func cliColor(out io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func newRootCommand(in io.Reader, out, errOut io.Writer, serve serverRunner) *cobra.Command {
	var legacyAddr, legacyData, legacyResetPassword string
	var legacyResetToken bool

	root := &cobra.Command{
		Use:           "haven",
		Short:         "Private server dashboard and app manager",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       Version,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch {
			case legacyResetToken:
				runResetToken(legacyData)
			case legacyResetPassword != "":
				runResetPassword(legacyData, legacyResetPassword)
			case cmd.Flags().Changed("addr") || cmd.Flags().Changed("data"):
				serve(legacyAddr, legacyData)
			default:
				return cmd.Help()
			}
			return nil
		},
	}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetVersionTemplate("haven {{.Version}}\n")
	root.SuggestionsMinimumDistance = 3
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddGroup(
		&cobra.Group{ID: "service", Title: "Service"},
		&cobra.Group{ID: "manage", Title: "Manage"},
		&cobra.Group{ID: "run", Title: "Run"},
		&cobra.Group{ID: "information", Title: "Information"},
	)
	root.Flags().StringVar(&legacyAddr, "addr", ":8484", "address to listen on")
	root.Flags().StringVar(&legacyData, "data", defaultDataDir, "directory for Haven's state and apps")
	root.Flags().StringVar(&legacyResetPassword, "reset-password", "", "reset password for a user")
	root.Flags().BoolVar(&legacyResetToken, "reset-token", false, "generate a one-time password reset code")
	for _, name := range []string{"addr", "data", "reset-password", "reset-token"} {
		_ = root.Flags().MarkHidden(name)
	}

	root.AddCommand(
		newServiceActionCommand("start", "Start the Haven service", out),
		newServiceActionCommand("stop", "Stop the Haven service", out),
		newServiceActionCommand("restart", "Restart the Haven service", out),
		newStatusCommand(out),
		newLogsCommand(out, errOut),
		newExperimentalCommand(in, out),
		newHostingCommand(out),
		newResetPasswordCommand(),
		newResetTokenCommand(),
		newServeCommand(serve),
		newVersionCommand(out),
		newCompletionCommand(root),
	)
	style := cliStyle{color: cliColor(out)}
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) { printCLIHelp(cmd, style) })
	return root
}

func newServiceActionCommand(action, short string, out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:     action,
		Short:   short,
		GroupID: "service",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runHavenServiceAction(action, out, systemctlCommandRunner(), os.Geteuid() == 0)
		},
	}
}

func newStatusCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show service and Hosting status",
		GroupID: "service",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return printHavenServiceStatus(out, systemctlCommandRunner())
		},
	}
}

func newLogsCommand(out, errOut io.Writer) *cobra.Command {
	lines := 100
	follow := false
	cmd := &cobra.Command{
		Use:     "logs",
		Short:   "Show Haven service logs",
		Long:    "Show logs from the installed Haven systemd service instead of starting another server process.",
		GroupID: "service",
		Args:    cobra.NoArgs,
		Example: "  sudo haven logs\n  sudo haven logs --lines 250\n  sudo haven logs --follow",
		RunE: func(*cobra.Command, []string) error {
			return streamHavenServiceLogs(out, errOut, lines, follow)
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "number of recent lines")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow new log lines")
	return cmd
}

func newExperimentalCommand(in io.Reader, out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:     "experimental",
		Aliases: []string{"experiments"},
		Short:   "Manage experimental features",
		Long:    "List Haven's experimental features, show whether each is enabled, and manage them interactively.",
		GroupID: "manage",
		Args:    cobra.NoArgs,
		Example: "  sudo haven experimental",
		RunE: func(*cobra.Command, []string) error {
			return runExperimentalCLI(nil, in, out)
		},
	}
}

func newHostingCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "hosting",
		Short:   "Manage experimental Hosting",
		Long:    "Enable, disable or inspect the Website and App Hosting experiment without using the interactive menu.",
		GroupID: "manage",
		Args:    cobra.NoArgs,
		Example: "  sudo haven hosting enable\n  haven hosting status\n  sudo haven hosting disable",
	}
	for _, action := range []string{"enable", "disable", "status"} {
		action := action
		short := map[string]string{
			"enable":  "Enable Website and App Hosting",
			"disable": "Disable Website and App Hosting",
			"status":  "Show whether Hosting is enabled",
		}[action]
		cmd.AddCommand(&cobra.Command{
			Use:   action,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return runHostingCLI([]string{action}, out)
			},
		})
	}
	return cmd
}

func newResetPasswordCommand() *cobra.Command {
	dataDir := defaultDataDir
	cmd := &cobra.Command{
		Use:     "reset-password [username]",
		Short:   "Reset an administrator password",
		GroupID: "manage",
		Args:    cobra.MaximumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			user := ""
			if len(args) == 1 {
				user = args[0]
			}
			runResetPassword(dataDir, user)
		},
	}
	cmd.Flags().StringVar(&dataDir, "data", defaultDataDir, "directory for Haven's state and apps")
	return cmd
}

func newResetTokenCommand() *cobra.Command {
	dataDir := defaultDataDir
	cmd := &cobra.Command{
		Use:     "reset-token",
		Short:   "Generate a one-time password reset code",
		GroupID: "manage",
		Args:    cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			runResetToken(dataDir)
		},
	}
	cmd.Flags().StringVar(&dataDir, "data", defaultDataDir, "directory for Haven's state and apps")
	return cmd
}

func newServeCommand(serve serverRunner) *cobra.Command {
	addr, dataDir := ":8484", defaultDataDir
	cmd := &cobra.Command{
		Use:     "serve",
		Short:   "Run the Haven server in the foreground",
		Long:    "Run Haven in the foreground. Normal installations already run Haven through systemd; use `sudo haven logs --follow` to inspect that service instead of starting a second copy.",
		GroupID: "run",
		Args:    cobra.NoArgs,
		Example: "  haven serve --addr :8484 --data /opt/haven\n  sudo haven logs --follow",
		Run: func(*cobra.Command, []string) {
			serve(addr, dataDir)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8484", "address to listen on")
	cmd.Flags().StringVar(&dataDir, "data", defaultDataDir, "directory for Haven's state and apps")
	return cmd
}

func newVersionCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print the Haven version",
		GroupID: "information",
		Args:    cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			fmt.Fprintf(out, "haven %s\n", Version)
		},
	}
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion <bash|zsh|fish|powershell>",
		Short:     "Generate a shell completion script",
		GroupID:   "information",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			}
			return nil
		},
	}
}

func printCLIHelp(cmd *cobra.Command, style cliStyle) {
	out := cmd.OutOrStdout()
	title := strings.ToUpper(cmd.CommandPath())
	if cmd.Parent() == nil {
		title = "HAVEN " + Version
	}
	fmt.Fprintln(out, style.accent(style.bold(title)))
	if description := strings.TrimSpace(cmd.Long); description != "" {
		fmt.Fprintln(out, description)
	} else if cmd.Short != "" {
		fmt.Fprintln(out, cmd.Short)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, style.bold("USAGE"))
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(out, "  %s <command>\n", cmd.CommandPath())
	} else {
		fmt.Fprintf(out, "  %s\n", cmd.UseLine())
	}

	printed := make(map[*cobra.Command]bool)
	for _, group := range cmd.Groups() {
		commands := availableCommands(cmd, group.ID)
		if len(commands) == 0 {
			continue
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.bold(strings.ToUpper(group.Title)))
		printCommandRows(out, commands, printed)
	}
	remaining := availableCommands(cmd, "")
	if len(remaining) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.bold("COMMANDS"))
		printCommandRows(out, remaining, printed)
	}
	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.bold("OPTIONS"))
		fmt.Fprint(out, cmd.LocalNonPersistentFlags().FlagUsagesWrapped(88))
	}
	if cmd.Example != "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, style.bold("EXAMPLES"))
		fmt.Fprintln(out, cmd.Example)
	}
	if cmd.Parent() == nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Run `haven help <command>` for command details.")
		fmt.Fprintln(out, "Haven already running? Use `haven status` or `sudo haven logs --follow`.")
	}
}

func availableCommands(parent *cobra.Command, group string) []*cobra.Command {
	var commands []*cobra.Command
	for _, command := range parent.Commands() {
		if command.IsAvailableCommand() && command.GroupID == group {
			commands = append(commands, command)
		}
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name() < commands[j].Name() })
	return commands
}

func printCommandRows(out io.Writer, commands []*cobra.Command, printed map[*cobra.Command]bool) {
	width := 0
	for _, command := range commands {
		if len(command.Name()) > width {
			width = len(command.Name())
		}
	}
	for _, command := range commands {
		if printed[command] {
			continue
		}
		printed[command] = true
		fmt.Fprintf(out, "  %-*s  %s\n", width, command.Name(), command.Short)
	}
}

func normalizeLegacyCLIArgs(args []string) []string {
	aliases := map[string]string{
		"-addr": "--addr", "-data": "--data", "-reset-password": "--reset-password",
		"-reset-token": "--reset-token", "-version": "--version",
	}
	normalized := append([]string(nil), args...)
	for i, arg := range normalized {
		if replacement, ok := aliases[arg]; ok {
			normalized[i] = replacement
		}
	}
	return normalized
}
